package tools

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/server"

	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/policy"
	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/shape"
)

func TestArgsAccessors(t *testing.T) {
	// Arguments arrive decoded from JSON, so every number is a float64 and a
	// model may send "42" where 42 is documented. The accessors have to cope
	// without silently accepting something that is not a whole number.
	args := Args{
		"name":     "  Ada  ",
		"count":    float64(42),
		"fraction": 1.5,
		"numeric":  "7",
		"flag":     true,
		"flagstr":  "true",
		"blank":    "   ",
		"nothing":  nil,
	}

	if got := args.String("name"); got != "Ada" {
		t.Errorf("String did not trim: %q", got)
	}
	if n, ok := args.Int("count"); !ok || n != 42 {
		t.Errorf("Int(float64 42) = %d, %v", n, ok)
	}
	if _, ok := args.Int("fraction"); ok {
		t.Error("Int accepted a fractional value")
	}
	if n, ok := args.Int("numeric"); !ok || n != 7 {
		t.Errorf(`Int("7") = %d, %v`, n, ok)
	}
	if !args.Bool("flag") || !args.Bool("flagstr") {
		t.Error("Bool did not accept a boolean and its string spelling")
	}
	if args.Bool("name") {
		t.Error("Bool treated a non-boolean string as true")
	}

	if !args.Has("name") || !args.Has("flag") {
		t.Error("Has reported a supplied argument as missing")
	}
	for _, key := range []string{"blank", "nothing", "absent"} {
		if args.Has(key) {
			t.Errorf("Has(%q) is true for an empty or absent argument", key)
		}
	}
}

func TestArgsIntRejectsPartiallyNumericStrings(t *testing.T) {
	// Partial parsing silently changes the target. "88x" reading as 88 would
	// show a human "service_id: 88x" in a confirmation preview while
	// terminating service 88.
	for _, bad := range []any{"42abc", "42.9", "1e3", "0x10", " 42 x", "", "abc", "4 2"} {
		if n, ok := (Args{"v": bad}).Int("v"); ok {
			t.Errorf("Int(%q) = %d, accepted; want rejection", bad, n)
		}
	}
	// Whitespace around an otherwise whole number is fine.
	for _, good := range []any{"42", " 42 ", 42, float64(42), int64(42)} {
		if n, ok := (Args{"v": good}).Int("v"); !ok || n != 42 {
			t.Errorf("Int(%v) = %d, %v; want 42, true", good, n, ok)
		}
	}
}

func TestArgsKeysAreSortedAndValueFree(t *testing.T) {
	// Keys feeds the audit record. It must be deterministic, and it must expose
	// names only: values may be customer content or the text of a write.
	keys := Args{"zebra": "x", "alpha": "y", "middle": "z"}.Keys()
	want := []string{"alpha", "middle", "zebra"}
	if len(keys) != len(want) {
		t.Fatalf("Keys() = %v, want %v", keys, want)
	}
	for i := range want {
		if keys[i] != want[i] {
			t.Fatalf("Keys() = %v, want %v", keys, want)
		}
	}
}

func TestPageClamping(t *testing.T) {
	lim := Limits{DefaultPageSize: 25, MaxPageSize: 50}

	tests := []struct {
		name        string
		args        Args
		wantLimit   int
		wantOffset  int
		wantClamped bool
	}{
		{"omitted", Args{}, 25, 0, false},
		{"within bounds", Args{"limit": float64(10)}, 10, 0, false},
		{"at the maximum", Args{"limit": float64(50)}, 50, 0, false},
		{"over the maximum", Args{"limit": float64(100000)}, 50, 0, true},
		{"zero falls back to the default", Args{"limit": float64(0)}, 25, 0, false},
		{"negative falls back to the default", Args{"limit": float64(-5)}, 25, 0, false},
		{"offset applied", Args{"offset": float64(75)}, 25, 75, false},
		{"negative offset ignored", Args{"offset": float64(-1)}, 25, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			limit, offset, clamped := tt.args.Page(lim)
			if limit != tt.wantLimit || offset != tt.wantOffset || clamped != tt.wantClamped {
				t.Errorf("Page() = (%d, %d, %v), want (%d, %d, %v)",
					limit, offset, clamped, tt.wantLimit, tt.wantOffset, tt.wantClamped)
			}
		})
	}
}

func TestLimitsResolveIsAlwaysBounded(t *testing.T) {
	// A zero-valued Limits must not mean "no limit". Unbounded is never a
	// resolved state.
	for _, in := range []Limits{
		{},
		{DefaultPageSize: -1, MaxPageSize: -1},
		{DefaultPageSize: 500, MaxPageSize: 100},
	} {
		got := in.resolve()
		if got.DefaultPageSize < 1 || got.MaxPageSize < 1 {
			t.Errorf("resolve(%+v) produced a non-positive page size: %+v", in, got)
		}
		if got.DefaultPageSize > got.MaxPageSize {
			t.Errorf("resolve(%+v) left the default above the maximum: %+v", in, got)
		}
	}
}

func TestToolValidationRejectsUnsafeDefinitions(t *testing.T) {
	okSpec := shape.Spec{Title: "X", Fields: []shape.Field{{Name: "id"}}}
	params := func(Args, Limits) (map[string]any, error) { return nil, nil }
	extract := func(map[string]any, shape.Spec, shape.Options, Limits, Args) any { return nil }

	tests := map[string]Tool{
		"no name":        {Desc: "d", Action: "GetClients", Out: okSpec, Params: params, Extract: extract},
		"no description": {Name: "t", Action: "GetClients", Out: okSpec, Params: params, Extract: extract},
		"no action":      {Name: "t", Desc: "d", Out: okSpec, Params: params, Extract: extract},
		"action not in the registry": {
			Name: "t", Desc: "d", Action: "NotAnAction", Out: okSpec, Params: params, Extract: extract,
		},
		"no params function":  {Name: "t", Desc: "d", Action: "GetClients", Out: okSpec, Extract: extract},
		"no extract function": {Name: "t", Desc: "d", Action: "GetClients", Out: okSpec, Params: params},
		// The one that matters: a destructive tool with no impact preview would
		// ask a human to approve something the server cannot describe.
		"destructive without a preview": {
			Name: "t", Desc: "d", Action: "ModuleTerminate", Out: okSpec, Params: params, Extract: extract,
		},
		// A spec declaring a credential field must fail at registration, not
		// leak in production.
		"output spec declares a credential": {
			Name: "t", Desc: "d", Action: "GetClients", Params: params, Extract: extract,
			Out: shape.Spec{Title: "X", Fields: []shape.Field{{Name: "password"}}},
		},
	}

	for name, def := range tests {
		t.Run(name, func(t *testing.T) {
			if err := def.validate(); err == nil {
				t.Error("an invalid tool definition was accepted")
			}
		})
	}
}

func TestClassIsTakenFromTheRegistryNotDeclared(t *testing.T) {
	// A tool cannot claim to be read-only while calling something that is not:
	// its class comes from the registry entry for the action it declares.
	tests := map[string]struct {
		tool     Tool
		wantRead bool
		wantDest bool
	}{
		"read action":        {Tool{Action: "GetClients"}, true, false},
		"write action":       {Tool{Action: "UpdateClient"}, false, false},
		"destructive action": {Tool{Action: "ModuleTerminate"}, false, true},
		// The escape hatch resolves its action at call time, so it is
		// annotated for the worst case it can reach.
		"escape hatch": {Tool{Action: anyAction}, false, true},
		// A local tool has no action to derive a class from, so it declares
		// one, and the default for an undeclared local tool is write.
		"local read-only": {Tool{Local: noopLocal, LocalReadOnly: true}, true, false},
		"local default":   {Tool{Local: noopLocal}, false, false},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			class := tt.tool.class()
			if (!class.Mutating()) != tt.wantRead {
				t.Errorf("read-only = %v, want %v", !class.Mutating(), tt.wantRead)
			}
			if class.NeedsConfirmation() != tt.wantDest {
				t.Errorf("needs confirmation = %v, want %v", class.NeedsConfirmation(), tt.wantDest)
			}
		})
	}
}

func TestEveryShippedToolDefinitionIsValid(t *testing.T) {
	// Every definition in All() must pass validation, so a malformed one is a
	// failing test rather than a server that refuses to start.
	for _, def := range All() {
		if err := def.validate(); err != nil {
			t.Errorf("%s: %v", def.Name, err)
		}
	}
}

func TestRegistrationRefusesAnOversizedToolSurface(t *testing.T) {
	// The cap is the whole reason for the curated-plus-escape-hatch design.
	// Exceeding it should stop the server, not quietly ship a listing that
	// degrades tool selection.
	pol, err := policy.New(policy.Config{Profile: policy.ProfileAdmin, AllowDestructive: true})
	if err != nil {
		t.Fatalf("policy.New: %v", err)
	}

	var defs []Tool
	base := All()
	for i := range MaxAdvertisedTools + 5 {
		def := base[0]
		def.Name = def.Name + string(rune('a'+i%26)) + string(rune('a'+i/26))
		defs = append(defs, def)
	}

	deps := Deps{Policy: pol, Limits: DefaultLimits()}
	if _, err := Register(server.NewMCPServer("test", "0"), deps, defs); err == nil {
		t.Fatal("registration accepted more tools than the advertised limit")
	}
}

// noopLocal is a stand-in for a local tool handler.
func noopLocal(context.Context, Deps, Args) (any, error) { return nil, nil }
