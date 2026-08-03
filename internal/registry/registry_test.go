package registry

import (
	"errors"
	"strings"
	"testing"

	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/errs"
)

func TestEveryActionIsClassified(t *testing.T) {
	// The runtime fallback treats an unclassified action as a write. That is
	// the safe direction, but it should never be reached: docgen fails
	// generation on an unclassified action. This test makes the committed
	// registry prove it, without needing the network.
	for _, a := range All() {
		if !Classified(a.Name) {
			t.Errorf("action %s has no entry in the classification table", a.Name)
		}
	}
}

func TestKnownDangerousActionsAreClassifiedDestructive(t *testing.T) {
	// A pinned list of the actions whose misclassification would be worst. If
	// someone downgrades one of these, this test is the tripwire.
	dangerous := []string{
		"DeleteClient", "CloseClient", "DeleteOrder", "CancelOrder",
		"ModuleTerminate", "ModuleSuspend", "ModuleCreate", "ModuleChangePw",
		"AddInvoicePayment", "CapturePayment", "AddCredit", "ApplyCredit",
		"AddTransaction", "GenInvoices",
		"DomainRegister", "DomainRenew", "DomainTransfer", "DomainRelease",
		"DeleteTicket", "AddTicketReply", "SendEmail", "SendQuote",
		"ResetPassword", "DeleteUserClient", "UpdateUserPermissions",
		"SetConfigurationValue", "UpdateModuleConfiguration",
	}
	for _, name := range dangerous {
		if got := Classify(name); got != ClassDestructive {
			t.Errorf("Classify(%s) = %s, want %s", name, got, ClassDestructive)
		}
	}
}

func TestCredentialActionsAreBlocked(t *testing.T) {
	// These return or mint credentials. No profile and no configuration may
	// enable them; Resolve must refuse before validation or policy runs.
	for _, name := range []string{
		"GetClientPassword", "DecryptPassword", "EncryptPassword",
		"CreateSsoToken", "CreateOAuthCredential", "UpdateOAuthCredential",
		"ValidateLogin",
	} {
		if got := Classify(name); got != ClassBlocked {
			t.Errorf("Classify(%s) = %s, want %s", name, got, ClassBlocked)
		}
		if _, err := Resolve(name); err == nil {
			t.Errorf("Resolve(%s) succeeded, want a forbidden error", name)
		} else if code := errs.Coded(err).Code; code != errs.CodeForbidden {
			t.Errorf("Resolve(%s) code = %s, want %s", name, code, errs.CodeForbidden)
		}
	}
}

func TestUnclassifiedDefaultsToWrite(t *testing.T) {
	if got := Classify("SomeActionTheVendorAddedYesterday"); got != ClassWrite {
		t.Errorf("Classify(unknown) = %s, want %s (never read)", got, ClassWrite)
	}
}

func TestLookupIsCaseInsensitive(t *testing.T) {
	for _, name := range []string{"GetClients", "getclients", "GETCLIENTS", " GetClients "} {
		a, ok := Lookup(name)
		if !ok {
			t.Fatalf("Lookup(%q) failed", name)
		}
		if a.Name != "GetClients" {
			t.Errorf("Lookup(%q).Name = %s, want GetClients", name, a.Name)
		}
	}
}

func TestResolveUnknownAction(t *testing.T) {
	_, err := Resolve("DefinitelyNotAnAction")
	if err == nil {
		t.Fatal("Resolve of an unknown action succeeded")
	}
	if code := errs.Coded(err).Code; code != errs.CodeUnknownAction {
		t.Errorf("code = %s, want %s", code, errs.CodeUnknownAction)
	}
}

func TestRegistryCoverage(t *testing.T) {
	if Count() < 150 {
		t.Errorf("registry has %d actions, expected at least 150", Count())
	}
	if got := len(Categories()); got != 16 {
		t.Errorf("registry has %d categories, want 16", got)
	}
	if n := len(ByCategory("client")); n == 0 {
		t.Error("ByCategory is not case-insensitive")
	}
}

func mustAction(t *testing.T, name string) Action {
	t.Helper()
	a, ok := Lookup(name)
	if !ok {
		t.Fatalf("action %s not in registry", name)
	}
	return a
}

func TestValidateRejectsUnknownParameter(t *testing.T) {
	a := mustAction(t, "AddClient")
	// A misspelling must be an error, not a silently dropped field. Silently
	// dropping is how a filtered query becomes an unfiltered one.
	_, err := Validate(a, map[string]any{"first_name": "Ada"})
	if err == nil {
		t.Fatal("unknown parameter accepted")
	}
	e := errs.Coded(err)
	if e.Code != errs.CodeInvalidParams {
		t.Fatalf("code = %s, want %s", e.Code, errs.CodeInvalidParams)
	}
	accepted, _ := e.Details["accepted_parameters"].([]string)
	if len(accepted) == 0 {
		t.Error("error details do not list the accepted parameter names")
	}
	found := false
	for _, n := range accepted {
		if n == "firstname" {
			found = true
		}
		if n == "cardnum" {
			t.Error("deprecated parameter offered as an accepted alternative")
		}
	}
	if !found {
		t.Error("the correct spelling was not offered in the error details")
	}
}

func TestValidateRejectsDeprecatedParameter(t *testing.T) {
	a := mustAction(t, "AddClient")
	for _, name := range []string{"cardnum", "cvv", "expdate"} {
		_, err := Validate(a, map[string]any{name: "4111111111111111"})
		if err == nil {
			t.Errorf("deprecated parameter %s accepted", name)
			continue
		}
		if !strings.Contains(err.Error(), "deprecated") {
			t.Errorf("error for %s does not explain the deprecation: %v", name, err)
		}
	}
}

func TestValidateRequiresRequiredParameters(t *testing.T) {
	a := mustAction(t, "AddClient")
	_, err := Validate(a, map[string]any{"firstname": "Ada", "lastname": "Lovelace"})
	if err == nil {
		t.Fatal("missing required parameters accepted")
	}
	if code := errs.Coded(err).Code; code != errs.CodeInvalidParams {
		t.Errorf("code = %s, want %s", code, errs.CodeInvalidParams)
	}
}

func TestValidateIdentifierBounds(t *testing.T) {
	a := mustAction(t, "GetInvoice")
	for _, bad := range []any{0, -1, 1.5, "abc", "0"} {
		if _, err := Validate(a, map[string]any{"invoiceid": bad}); err == nil {
			t.Errorf("invoiceid = %v accepted, want rejection", bad)
		}
	}
	v, err := Validate(a, map[string]any{"invoiceid": 42})
	if err != nil {
		t.Fatalf("valid invoiceid rejected: %v", err)
	}
	if got := v.Get("invoiceid"); got != "42" {
		t.Errorf("encoded invoiceid = %q, want 42", got)
	}
}

func TestValidateCoercesJSONNumbers(t *testing.T) {
	a := mustAction(t, "GetInvoice")
	// JSON decodes every number to float64. An integral float64 is an integer.
	v, err := Validate(a, map[string]any{"invoiceid": float64(7)})
	if err != nil {
		t.Fatalf("integral float64 rejected: %v", err)
	}
	if got := v.Get("invoiceid"); got != "7" {
		t.Errorf("encoded = %q, want 7", got)
	}
	if _, err := Validate(a, map[string]any{"invoiceid": 7.5}); err == nil {
		t.Error("fractional identifier accepted")
	}
}

func TestValidateBooleanEncoding(t *testing.T) {
	a := mustAction(t, "AddClient")
	args := requiredArgs(t, a)
	args["noemail"] = true
	v, err := Validate(a, args)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if got := v.Get("noemail"); got != "1" {
		t.Errorf("noemail = %q, want 1", got)
	}
}

func TestValidateBoundsStringLength(t *testing.T) {
	a := mustAction(t, "AddClient")
	args := requiredArgs(t, a)
	args["notes"] = strings.Repeat("x", MaxStringLen+1)
	if _, err := Validate(a, args); err == nil {
		t.Fatal("oversized string accepted")
	}
}

func TestValidateRejectsStructuredValues(t *testing.T) {
	a := mustAction(t, "AddClient")
	args := requiredArgs(t, a)
	args["notes"] = map[string]any{"nested": true}
	if _, err := Validate(a, args); err == nil {
		t.Fatal("structured value accepted for a string parameter")
	}
}

func TestValidateHappyPath(t *testing.T) {
	a := mustAction(t, "AddClient")
	v, err := Validate(a, requiredArgs(t, a))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if v.Get("action") != "" {
		t.Error("Validate emitted an action parameter; the client owns that")
	}
	if v.Get("firstname") == "" {
		t.Error("required value not encoded")
	}
}

func TestDescribeOmitsDeprecatedParameters(t *testing.T) {
	a := mustAction(t, "AddClient")
	d := Describe(a)
	params, _ := d["parameters"].([]map[string]any)
	if len(params) == 0 {
		t.Fatal("Describe returned no parameters")
	}
	for _, p := range params {
		if p["name"] == "cardnum" {
			t.Error("Describe advertised a deprecated parameter the server will refuse")
		}
	}
	if d["classification"] != string(ClassWrite) {
		t.Errorf("classification = %v, want %s", d["classification"], ClassWrite)
	}
}

func TestErrorsAreCoded(t *testing.T) {
	a := mustAction(t, "AddClient")
	_, err := Validate(a, map[string]any{"nope": 1})
	var coded *errs.Error
	if !errors.As(err, &coded) {
		t.Fatalf("validation error is not a coded error: %T", err)
	}
	if coded.Retryable {
		t.Error("a validation error is marked retryable; retrying cannot help")
	}
}

// requiredArgs builds a minimal valid argument set for an action.
func requiredArgs(t *testing.T, a Action) map[string]any {
	t.Helper()
	args := map[string]any{}
	for _, p := range a.Params {
		if !p.Required || p.Deprecated {
			continue
		}
		switch p.Type {
		case TypeInt:
			args[p.Name] = 1
		case TypeFloat:
			args[p.Name] = 1.0
		case TypeBool:
			args[p.Name] = false
		default:
			args[p.Name] = "x"
		}
	}
	return args
}
