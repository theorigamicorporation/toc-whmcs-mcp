package redact_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/redact"
)

func TestSecretsAreWithheldNotDropped(t *testing.T) {
	// An operator needs to know a service has a password set. The agent does
	// not need the password. Keeping the key and replacing the value serves
	// both.
	in := map[string]any{
		"username": "alice",
		"password": "hunter2",
	}
	out := redact.Map(in, redact.Options{})

	if out["username"] != "alice" {
		t.Errorf("a harmless field was altered: %+v", out)
	}
	v, ok := out["password"]
	if !ok {
		t.Fatal("the password key was dropped; it should be present and withheld")
	}
	if v != redact.Placeholder {
		t.Errorf("password = %v, want the placeholder", v)
	}
}

func TestSecretSpellingsAreAllCaught(t *testing.T) {
	// WHMCS spells the same concept several ways across actions, so matching
	// is on substrings rather than an exact list of known field names.
	for _, key := range []string{
		"password", "password2", "Password", "serverpassword", "server_password",
		"encryptedPassword", "api_secret", "apiSecret", "accesskey", "access_key",
		"privatekey", "authcode", "eppcode", "cvv", "cardnum", "card_number",
		"securityqans", "twofa_secret", "sso_token", "oauth_token", "passwordHash",
	} {
		out := redact.Map(map[string]any{key: "sensitive"}, redact.Options{})
		if out[key] != redact.Placeholder {
			t.Errorf("%s was not withheld (got %v)", key, out[key])
		}
	}
}

func TestHarmlessLookalikesSurvive(t *testing.T) {
	// Over-redaction has a cost too: a field that merely mentions a credential
	// is not a credential, and losing it makes the tool less useful for no gain.
	in := map[string]any{
		"password_updated_at":   "2026-01-01",
		"requirepasswordchange": true,
		"tokens_used":           12.0,
		"sso_enabled":           true,
		"email":                 "a@example.com",
	}
	out := redact.Map(in, redact.Options{})
	for k, want := range in {
		if out[k] != want {
			t.Errorf("%s was redacted but carries no secret (got %v)", k, out[k])
		}
	}
}

func TestNestedSecretsAreCaught(t *testing.T) {
	// The escape hatch returns arbitrary nesting, so depth cannot be assumed.
	in := map[string]any{
		"server": map[string]any{
			"hostname": "web01.example.com",
			"access": map[string]any{
				"username": "root",
				"password": "s3cret",
			},
		},
		"services": []any{
			map[string]any{"id": 1.0, "servicepassword": "abc"},
		},
	}
	out := redact.Map(in, redact.Options{})

	encoded, _ := json.Marshal(out)
	for _, leaked := range []string{"s3cret", "abc"} {
		if strings.Contains(string(encoded), leaked) {
			t.Errorf("a nested secret leaked: %s", encoded)
		}
	}
	if !strings.Contains(string(encoded), "web01.example.com") {
		t.Error("nested non-secret data was lost")
	}
}

func TestPIIRequiresOptIn(t *testing.T) {
	in := map[string]any{
		"firstname":   "Ada",
		"email":       "ada@example.com",
		"address1":    "1 Main Street",
		"postcode":    "12345",
		"phonenumber": "+1 555 0100",
		"tax_id":      "GB123456789",
	}

	minimal := redact.Map(in, redact.Options{})
	for _, k := range []string{"address1", "postcode", "phonenumber", "tax_id"} {
		if _, present := minimal[k]; present {
			t.Errorf("%s was returned without the personal-detail opt-in", k)
		}
	}
	if minimal["firstname"] != "Ada" || minimal["email"] != "ada@example.com" {
		t.Error("identifying fields needed to do the job were dropped")
	}

	full := redact.Map(in, redact.Options{IncludePII: true})
	for k, want := range in {
		if full[k] != want {
			t.Errorf("%s missing with the opt-in set", k)
		}
	}
}

func TestAdminNotesRequireOptIn(t *testing.T) {
	// Admin notes are written on the assumption that customers and automated
	// systems will never read them.
	in := map[string]any{"id": 1.0, "adminnotes": "chargeback risk", "admin_notes": "do not refund"}

	out := redact.Map(in, redact.Options{})
	if _, present := out["adminnotes"]; present {
		t.Error("admin notes returned by default")
	}
	if _, present := out["admin_notes"]; present {
		t.Error("admin notes returned by default")
	}

	full := redact.Map(in, redact.Options{IncludeNotes: true})
	if full["adminnotes"] != "chargeback risk" {
		t.Error("admin notes missing with the opt-in set")
	}
}

func TestDeeplyNestedStructureIsTruncatedNotWalkedForever(t *testing.T) {
	// A pathological structure must not become an unbounded walk.
	deep := map[string]any{"v": "leaf"}
	for range redact.MaxDepth + 10 {
		deep = map[string]any{"next": deep}
	}
	out := redact.Map(deep, redact.Options{})
	encoded, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(encoded), "too deep") {
		t.Error("no truncation marker in a structure deeper than the limit")
	}
}

func TestIsSecret(t *testing.T) {
	if !redact.IsSecret("servicepassword") {
		t.Error("IsSecret missed a credential field")
	}
	if redact.IsSecret("email") {
		t.Error("IsSecret flagged an ordinary field")
	}
	if redact.IsSecret("password_updated_at") {
		t.Error("IsSecret flagged a timestamp about a credential as the credential")
	}
}
