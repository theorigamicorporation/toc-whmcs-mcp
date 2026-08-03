package registry

import (
	"strings"
	"testing"

	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/errs"
)

// FuzzValidate exercises parameter validation with arbitrary names and values.
//
// Validation is the last thing standing between a model's arguments and a POST
// to a billing system. It has two jobs and must never fail either one: it must
// not panic, and it must not let a value through that it did not check. A
// panic is a denial of service on a tool call; a silent pass-through is how a
// filtered query becomes an unfiltered one.
//
// Run with: go test -fuzz=FuzzValidate ./internal/registry/
func FuzzValidate(f *testing.F) {
	seeds := []struct {
		action string
		key    string
		value  string
	}{
		{"GetInvoice", "invoiceid", "42"},
		{"GetInvoice", "invoiceid", "-1"},
		{"GetInvoice", "invoiceid", "0"},
		{"GetInvoice", "invoiceid", "1.5"},
		{"GetInvoice", "invoiceid", "9999999999999999999999"},
		{"AddClient", "firstname", "Ada"},
		{"AddClient", "first_name", "Ada"},
		{"AddClient", "cardnum", "4111111111111111"},
		{"AddClient", "noemail", "true"},
		{"AddClient", "notes", strings.Repeat("x", MaxStringLen+1)},
		{"GetClients", "limitnum", "\x00"},
		{"NotAnAction", "x", "y"},
		{"", "", ""},
		{"GetInvoice", "INVOICEID", "42"},
		{"GetInvoice", " invoiceid ", "42"},
	}
	for _, s := range seeds {
		f.Add(s.action, s.key, s.value)
	}

	f.Fuzz(func(t *testing.T, actionName, key, value string) {
		a, ok := Lookup(actionName)
		if !ok {
			// An unknown action must be refused before anything is validated,
			// so nothing can be sent to an endpoint we have no schema for.
			if _, err := Resolve(actionName); err == nil {
				t.Fatalf("Resolve accepted the unknown action %q", actionName)
			}
			return
		}

		values, err := Validate(a, map[string]any{key: value})
		if err != nil {
			// Every rejection must be a coded error a caller can act on, never
			// a bare error or a panic.
			coded := errs.Coded(err)
			switch coded.Code {
			case errs.CodeInvalidParams, errs.CodeUnknownAction, errs.CodeForbidden:
			default:
				t.Fatalf("validation failed with unexpected code %s for %q=%q on %s",
					coded.Code, key, value, a.Name)
			}
			if coded.Retryable {
				t.Fatalf("a validation error is marked retryable for %q=%q", key, value)
			}
			return
		}

		// If it passed, the parameter must be one the action actually declares,
		// and it must not be a deprecated one.
		for name := range values {
			p, declared := a.Param(name)
			if !declared {
				t.Fatalf("Validate emitted the undeclared parameter %q for %s", name, a.Name)
			}
			if p.Deprecated {
				t.Fatalf("Validate emitted the deprecated parameter %q for %s", name, a.Name)
			}
		}

		// The action name is supplied by the client, never by an argument. If
		// validation let one through, a caller could redirect the call after
		// the policy layer had already authorised a different action.
		if values.Get("action") != "" {
			t.Fatalf("Validate emitted an action parameter from input %q=%q", key, value)
		}

		// Bounds that the rest of the server relies on.
		for name, vs := range values {
			for _, v := range vs {
				if len(v) > MaxStringLen {
					t.Fatalf("parameter %q is %d bytes, over the %d limit", name, len(v), MaxStringLen)
				}
			}
		}
	})
}

// FuzzResolveNeverPanics checks action resolution against arbitrary names.
//
// The name reaching Resolve comes straight from the model through
// whmcs_call_action, so it is attacker-influenced in the same way ticket text
// is. Blocked actions must stay blocked no matter how the name is spelled.
func FuzzResolveNeverPanics(f *testing.F) {
	for _, s := range []string{
		"GetClients", "getclients", "GETCLIENTS", " GetClients ",
		"GetClientPassword", "getclientpassword", "GetClientPassword\x00",
		"DecryptPassword", "CreateSsoToken", "ValidateLogin",
		"", "\x00", strings.Repeat("A", 4096), "../../etc/passwd", "%s%s%s",
	} {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, name string) {
		a, err := Resolve(name)
		if err != nil {
			return
		}

		// Anything that resolves must be a real action, and must never be one
		// of the permanently blocked ones.
		if _, ok := Lookup(a.Name); !ok {
			t.Fatalf("Resolve returned %q, which is not in the registry", a.Name)
		}
		if Classify(a.Name) == ClassBlocked {
			t.Fatalf("Resolve returned the blocked action %q for input %q", a.Name, name)
		}
	})
}
