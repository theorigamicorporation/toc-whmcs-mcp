package main

import (
	"os"
	"testing"

	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/registry"
)

// The fixtures are captured copies of the vendor pages. They exist so a parser
// regression is caught by `go test` with no network access, which is the only
// way this check can run in CI.

func open(t *testing.T, name string) *os.File {
	t.Helper()
	f, err := os.Open("testdata/" + name)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func TestParseIndex(t *testing.T) {
	entries, err := parseIndex(open(t, "api-index.html"))
	if err != nil {
		t.Fatalf("parseIndex: %v", err)
	}
	entries = dedupe(entries)

	if got := len(entries); got < 150 {
		t.Errorf("parsed %d actions, expected at least 150; the index parser is probably broken", got)
	}
	if got := countCategories(entries); got != 16 {
		t.Errorf("parsed %d categories, want 16", got)
	}

	byName := make(map[string]indexEntry, len(entries))
	for _, e := range entries {
		byName[e.Name] = e
	}

	// Spot-check one action per structural concern: first in the page, in a
	// multi-word category, and one whose slug differs in case from its name.
	for _, want := range []indexEntry{
		{Name: "AcceptOrder", Slug: "acceptorder", Category: "Orders"},
		{Name: "CreateProject", Slug: "createproject", Category: "Project Management"},
		{Name: "ModuleTerminate", Slug: "moduleterminate", Category: "Service"},
		{Name: "GetTLDPricing", Slug: "gettldpricing", Category: "Domains"},
	} {
		got, ok := byName[want.Name]
		if !ok {
			t.Errorf("action %s missing from index", want.Name)
			continue
		}
		if got != want {
			t.Errorf("index entry for %s = %+v, want %+v", want.Name, got, want)
		}
	}

	// Navigation headings ("Learn", "Contribute") must not become categories.
	for _, e := range entries {
		if e.Category == "Learn" || e.Category == "Contribute" || e.Category == "Stay up to date" {
			t.Errorf("site navigation heading %q leaked in as a category (action %s)", e.Category, e.Name)
		}
	}
}

func TestParseAction(t *testing.T) {
	page, err := parseAction(open(t, "action-addclient.html"))
	if err != nil {
		t.Fatalf("parseAction: %v", err)
	}

	if page.Summary != "Adds a client." {
		t.Errorf("summary = %q, want %q", page.Summary, "Adds a client.")
	}

	byName := make(map[string]registry.Param, len(page.Params))
	for _, p := range page.Params {
		byName[p.Name] = p
	}

	// The synthetic "action" row must be dropped: it is supplied by the client,
	// and accepting it would let a caller redirect the call after the policy
	// layer has already resolved which action it is authorising.
	if _, ok := byName["action"]; ok {
		t.Error("the synthetic \"action\" parameter was not dropped")
	}

	tests := []struct {
		name       string
		typ        registry.ParamType
		required   bool
		deprecated bool
	}{
		{name: "firstname", typ: registry.TypeString, required: true},
		{name: "email", typ: registry.TypeString, required: true},
		{name: "owner_user_id", typ: registry.TypeInt},
		{name: "securityqid", typ: registry.TypeInt},
		{name: "noemail", typ: registry.TypeBool},
		{name: "notes", typ: registry.TypeString},
		// Card fields are documented as deprecated. The server refuses to send
		// them, so the parser must carry the flag through.
		{name: "cardnum", typ: registry.TypeString, deprecated: true},
		{name: "cvv", typ: registry.TypeString, deprecated: true},
	}
	for _, tt := range tests {
		p, ok := byName[tt.name]
		if !ok {
			t.Errorf("parameter %s missing", tt.name)
			continue
		}
		if p.Type != tt.typ {
			t.Errorf("%s type = %s, want %s", tt.name, p.Type, tt.typ)
		}
		if p.Required != tt.required {
			t.Errorf("%s required = %v, want %v", tt.name, p.Required, tt.required)
		}
		if p.Deprecated != tt.deprecated {
			t.Errorf("%s deprecated = %v, want %v", tt.name, p.Deprecated, tt.deprecated)
		}
	}

	// A deprecated parameter must never also read as required, or validation
	// would demand a value it then refuses to accept.
	for _, p := range page.Params {
		if p.Deprecated && p.Required {
			t.Errorf("parameter %s is both deprecated and required", p.Name)
		}
	}

	if len(page.Response) == 0 {
		t.Error("no response parameters parsed")
	}
}

func TestParseActionDescriptionsAreNormalised(t *testing.T) {
	page, err := parseAction(open(t, "action-addclient.html"))
	if err != nil {
		t.Fatalf("parseAction: %v", err)
	}

	for _, p := range page.Params {
		// Inline <code> markup must be flattened, entities decoded, and
		// typographic quotes normalised, or the generated file churns between
		// regenerations and the diff stops being reviewable.
		for _, bad := range []string{"<", ">", "&amp;", "&ldquo;", "“", "”", "\n", "\t"} {
			if contains(p.Description, bad) {
				t.Errorf("description of %s contains unnormalised %q: %q", p.Name, bad, p.Description)
			}
		}
	}
}

func TestNormaliseType(t *testing.T) {
	tests := map[string]registry.ParamType{
		"string":  registry.TypeString,
		"int":     registry.TypeInt,
		"integer": registry.TypeInt,
		"bool":    registry.TypeBool,
		"boolean": registry.TypeBool,
		"float":   registry.TypeFloat,
		"decimal": registry.TypeFloat,
		"array":   registry.TypeArray,
		"object":  registry.TypeObject,
		"":        registry.TypeString,
		"mixed":   registry.TypeString,
	}
	for in, want := range tests {
		if got := normaliseType(in); got != want {
			t.Errorf("normaliseType(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestActionSlug(t *testing.T) {
	tests := []struct {
		href string
		want string
		ok   bool
	}{
		{"//developers.whmcs.com/api-reference/addclient/", "addclient", true},
		{"https://developers.whmcs.com/api-reference/getinvoices/", "getinvoices", true},
		{"/api-reference/moduleterminate", "moduleterminate", true},
		{"/api/api-index/", "", false},
		{"https://example.com/", "", false},
		{"/api-reference/", "", false},
	}
	for _, tt := range tests {
		got, ok := actionSlug(tt.href)
		if ok != tt.ok || got != tt.want {
			t.Errorf("actionSlug(%q) = (%q, %v), want (%q, %v)", tt.href, got, ok, tt.want, tt.ok)
		}
	}
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(h, n string) int {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}
	return -1
}
