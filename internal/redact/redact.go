// Package redact removes values that must never reach the model.
//
// Curated tools project responses onto an allowlist of declared fields, so
// redaction there is a second line of defence. The generic whmcs_call_action
// escape hatch has no per-action schema and cannot use an allowlist, so for it
// this denylist is the only line. That asymmetry is why the denylist is
// aggressive and matches on substrings.
package redact

import (
	"strings"
)

// Placeholder replaces a withheld value. It is deliberately informative: the
// agent should know a value exists so it can tell an operator where to look,
// without holding the value itself.
const Placeholder = "[withheld by policy]"

// MaxDepth bounds recursion into a response. WHMCS responses are shallow; a
// deeper structure is either a defect or an attempt to exhaust the redactor.
const MaxDepth = 24

// secretSubstrings match a key at any depth. Substring matching over exact
// names is intentional: WHMCS spells the same concept as "password",
// "password2", "serverpassword" and "encryptedPassword" in different responses,
// and a name we have not seen yet should still be caught.
var secretSubstrings = []string{
	"password", "passwd", "secret", "apikey", "api_key", "accesskey", "access_key",
	"privatekey", "private_key", "token", "credential", "authcode", "auth_code",
	"epp", "cvv", "cvc", "cardnum", "cardnumber", "card_number", "ccnumber",
	"securityqans", "securityanswer", "security_answer", "hash", "salt",
	"twofa", "2fa", "otp", "sso",
}

// allowedSubstrings are keys that contain a secret substring but carry no
// secret. Without these, useful and harmless fields disappear: "tokens_used" or
// "password_updated_at" are not credentials.
var allowedSubstrings = []string{
	"passwordupdated", "password_updated", "passwordchanged", "password_changed",
	"tokenexpiry", "token_expiry", "tokensused", "tokens_used",
	"hasnewpassword", "requirepasswordchange", "password_reset_at",
	"epp_required", "ssoenabled", "sso_enabled",
}

// piiSubstrings match keys carrying personal data that is withheld unless a
// tool explicitly opts in. These are not secrets; they are data minimisation.
var piiSubstrings = []string{
	"address1", "address2", "postcode", "zipcode", "zip_code",
	"phonenumber", "phone_number", "telephone", "tax_id", "taxid", "vatnumber",
	"billingcontact", "cardlastfour", "card_last_four", "lastfour",
	"datebirth", "date_of_birth", "dob", "ssn", "nationalid", "national_id",
}

// notesSubstrings match internal, admin-authored commentary. It is excluded by
// default because it is written on the assumption that customers and automated
// systems will never read it.
var notesSubstrings = []string{
	"adminnotes", "admin_notes", "internalnotes", "internal_notes",
}

// Options controls what a particular call is allowed to keep.
type Options struct {
	// IncludePII keeps personal data fields. A tool sets this only when the
	// caller explicitly asked for full detail, and the access is audited.
	IncludePII bool
	// IncludeNotes keeps admin-only notes, on the same terms.
	IncludeNotes bool
}

// Value redacts a decoded JSON value in place of its secrets, returning a new
// value. Maps, slices and scalars are all handled; anything deeper than
// MaxDepth is replaced with a marker rather than walked.
func Value(v any, opts Options) any {
	return walk(v, opts, 0)
}

// Map redacts a decoded JSON object.
func Map(m map[string]any, opts Options) map[string]any {
	out, _ := walk(m, opts, 0).(map[string]any)
	return out
}

func walk(v any, opts Options, depth int) any {
	if depth > MaxDepth {
		return "[truncated: structure too deep]"
	}
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			switch Classify(k, opts) {
			case KeepValue:
				out[k] = walk(val, opts, depth+1)
			case WithholdValue:
				// Report the field's existence, not its content. An operator
				// needs to know a service has a password set; the agent does
				// not need the password.
				out[k] = Placeholder
			case DropValue:
				continue
			}
		}
		return out
	case []any:
		out := make([]any, 0, len(t))
		for _, val := range t {
			out = append(out, walk(val, opts, depth+1))
		}
		return out
	default:
		return v
	}
}

// Decision is what to do with a field.
type Decision int

const (
	// KeepValue passes the value through.
	KeepValue Decision = iota
	// WithholdValue replaces the value with Placeholder, keeping the key so the
	// caller learns the field exists.
	WithholdValue
	// DropValue removes the key entirely, used for data that is merely
	// unnecessary rather than sensitive.
	DropValue
)

// Classify decides how to treat a field name.
func Classify(key string, opts Options) Decision {
	k := normalise(key)

	for _, allowed := range allowedSubstrings {
		if strings.Contains(k, allowed) {
			return KeepValue
		}
	}
	for _, s := range secretSubstrings {
		if strings.Contains(k, s) {
			return WithholdValue
		}
	}
	if !opts.IncludeNotes {
		for _, s := range notesSubstrings {
			if strings.Contains(k, s) {
				return DropValue
			}
		}
	}
	if !opts.IncludePII {
		for _, s := range piiSubstrings {
			if strings.Contains(k, s) {
				return DropValue
			}
		}
	}
	return KeepValue
}

// IsSecret reports whether a field name names a credential. Used by the
// projection layer to refuse a spec that would declare a secret field, which
// makes that mistake a startup failure rather than a leak.
func IsSecret(key string) bool {
	k := normalise(key)
	for _, allowed := range allowedSubstrings {
		if strings.Contains(k, allowed) {
			return false
		}
	}
	for _, s := range secretSubstrings {
		if strings.Contains(k, s) {
			return true
		}
	}
	return false
}

// normalise lowercases and strips separators so that "api_key", "apiKey" and
// "API-KEY" all match the same substring.
func normalise(key string) string {
	var b strings.Builder
	b.Grow(len(key))
	for _, r := range strings.ToLower(key) {
		if r == '_' || r == '-' || r == ' ' {
			continue
		}
		b.WriteRune(r)
	}
	// Match both the separated and unseparated spellings.
	stripped := b.String()
	return stripped + "|" + strings.ToLower(key)
}
