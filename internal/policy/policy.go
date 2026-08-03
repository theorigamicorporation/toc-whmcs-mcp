// Package policy decides which WHMCS actions this process may perform.
//
// The decision is made once, at startup, from the configured profile. Tools
// whose action is not permitted are never registered with the MCP server, so
// they are neither listed nor callable. Handlers re-check anyway, because
// whmcs_call_action picks its action at call time.
//
// Nothing here consults the model, the tool description, or the MCP host. A
// natural-language warning is not an access control.
package policy

import (
	"fmt"
	"sort"
	"strings"

	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/errs"
	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/registry"
)

// Profile is a coarse capability band. Four bands is deliberately fewer than
// every team will want: a general policy language would be a much larger
// surface to get wrong, and the allowlist provides narrowing.
type Profile string

const (
	// ProfileReadOnly permits no state change at all. This is the default.
	ProfileReadOnly Profile = "readonly"
	// ProfileSupport permits ticket and client-record work. It cannot touch
	// billing or provisioning.
	ProfileSupport Profile = "support"
	// ProfileBilling permits invoice, quote, transaction and order work. It
	// cannot terminate or modify services.
	ProfileBilling Profile = "billing"
	// ProfileAdmin permits every non-blocked action. Destructive actions still
	// require explicit enablement and confirmation.
	ProfileAdmin Profile = "admin"
)

// Profiles lists the valid profiles, for error messages and documentation.
func Profiles() []Profile {
	return []Profile{ProfileReadOnly, ProfileSupport, ProfileBilling, ProfileAdmin}
}

// ParseProfile resolves a configured profile name. An empty value is the
// read-only default: starting with no profile configured must not be a way to
// get write access.
func ParseProfile(s string) (Profile, error) {
	if strings.TrimSpace(s) == "" {
		return ProfileReadOnly, nil
	}
	p := Profile(strings.ToLower(strings.TrimSpace(s)))
	for _, valid := range Profiles() {
		if p == valid {
			return p, nil
		}
	}
	names := make([]string, 0, 4)
	for _, valid := range Profiles() {
		names = append(names, string(valid))
	}
	return "", fmt.Errorf("unknown profile %q; valid profiles are: %s", s, strings.Join(names, ", "))
}

// mutableCategories lists the documentation categories in which a profile may
// change state. Reads are permitted in every category for every profile: the
// separation that matters operationally is who can change what, and a support
// agent who cannot see an invoice cannot answer a billing question.
var mutableCategories = map[Profile]map[string]bool{
	ProfileReadOnly: {},
	ProfileSupport: {
		"Support": true,
		"Tickets": true,
		"Client":  true,
		"Users":   true,
	},
	ProfileBilling: {
		"Billing":  true,
		"Orders":   true,
		"Addons":   true,
		"Products": true,
		"Client":   true,
	},
	// Admin is handled as "everything" rather than by enumeration, so a new
	// vendor category does not silently become unreachable for the one profile
	// that is supposed to reach everything.
	ProfileAdmin: nil,
}

// Config is the resolved access-control configuration.
type Config struct {
	Profile Profile
	// AllowDestructive enables actions classified destructive. It is off by
	// default in every profile, including admin: being an administrator is a
	// statement about authority, not about intent to delete something right now.
	AllowDestructive bool
	// Allowlist, when non-empty, restricts the advertised tools to those named.
	// It can only subtract from the profile.
	Allowlist []string
}

// Policy answers "may this process do that".
type Policy struct {
	profile          Profile
	allowDestructive bool
	allowlist        map[string]bool
	// ignoredAllowlist records allowlist entries that name a tool the profile
	// does not grant, so startup can report them rather than leaving an
	// operator to wonder why a tool they listed never appeared.
	ignoredAllowlist []string
}

// New resolves a policy.
func New(cfg Config) (*Policy, error) {
	if _, err := ParseProfile(string(cfg.Profile)); err != nil {
		return nil, err
	}
	p := &Policy{
		profile:          cfg.Profile,
		allowDestructive: cfg.AllowDestructive,
	}
	if len(cfg.Allowlist) > 0 {
		p.allowlist = make(map[string]bool, len(cfg.Allowlist))
		for _, name := range cfg.Allowlist {
			name = strings.TrimSpace(name)
			if name != "" {
				p.allowlist[name] = true
			}
		}
	}
	return p, nil
}

// Profile returns the active profile.
func (p *Policy) Profile() Profile { return p.profile }

// DestructiveEnabled reports whether destructive actions were explicitly
// enabled.
func (p *Policy) DestructiveEnabled() bool { return p.allowDestructive }

// AllowsAction reports whether an action may be performed, returning a coded
// forbidden error explaining why not.
//
// The explanation matters: a model that is told "forbidden" without a reason
// will retry with variations. One that is told the profile forbids it will
// report back to the operator, which is the outcome we want.
func (p *Policy) AllowsAction(name string) error {
	a, ok := registry.Lookup(name)
	if !ok {
		return errs.New(errs.CodeUnknownAction, "unknown WHMCS action %q", name)
	}

	class := registry.Classify(a.Name)

	if class == registry.ClassBlocked {
		return errs.New(errs.CodeForbidden,
			"action %s is permanently blocked by this server and cannot be enabled by configuration", a.Name).
			WithDetails(map[string]any{"reason": "the action returns or issues credentials"})
	}

	if !class.Mutating() {
		return nil
	}

	if p.profile == ProfileReadOnly {
		return errs.New(errs.CodeForbidden,
			"action %s modifies data and the server is running in the readonly profile", a.Name).
			WithDetails(map[string]any{
				"profile":        string(p.profile),
				"classification": string(class),
			})
	}

	if allowed := mutableCategories[p.profile]; allowed != nil && !allowed[a.Category] {
		return errs.New(errs.CodeForbidden,
			"action %s is in the %s category, which the %s profile may not modify", a.Name, a.Category, p.profile).
			WithDetails(map[string]any{
				"profile":            string(p.profile),
				"category":           a.Category,
				"mutable_categories": sortedKeys(allowed),
			})
	}

	if class == registry.ClassDestructive && !p.allowDestructive {
		return errs.New(errs.CodeForbidden,
			"action %s is classified destructive and destructive actions are not enabled on this server", a.Name).
			WithDetails(map[string]any{
				"classification": string(class),
				"remedy":         "set WHMCS_MCP_ALLOW_DESTRUCTIVE=true to enable, then confirm each call",
			})
	}

	return nil
}

// AllowsTool reports whether a named tool may be registered and called. It
// applies the allowlist; the action check is separate because one tool can map
// to more than one action and whmcs_call_action maps to any of them.
func (p *Policy) AllowsTool(tool string) bool {
	if p.allowlist == nil {
		return true
	}
	return p.allowlist[tool]
}

// HasAllowlist reports whether an explicit tool allowlist is configured.
func (p *Policy) HasAllowlist() bool { return p.allowlist != nil }

// NoteIgnoredAllowlistEntry records an allowlist entry that the profile does
// not grant. The allowlist can only subtract, so naming a write tool under the
// readonly profile does not enable it, and an operator should be told.
func (p *Policy) NoteIgnoredAllowlistEntry(tool string) {
	p.ignoredAllowlist = append(p.ignoredAllowlist, tool)
}

// IgnoredAllowlistEntries returns the recorded entries, sorted.
func (p *Policy) IgnoredAllowlistEntries() []string {
	out := make([]string, len(p.ignoredAllowlist))
	copy(out, p.ignoredAllowlist)
	sort.Strings(out)
	return out
}

// PermittedActions returns every action this policy allows, for the startup
// summary and the status tool.
func (p *Policy) PermittedActions() []string {
	var out []string
	for _, a := range registry.All() {
		if p.AllowsAction(a.Name) == nil {
			out = append(out, a.Name)
		}
	}
	return out
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
