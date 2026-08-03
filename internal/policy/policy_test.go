package policy_test

import (
	"testing"

	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/errs"
	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/policy"
	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/registry"
)

func mustPolicy(t *testing.T, cfg policy.Config) *policy.Policy {
	t.Helper()
	p, err := policy.New(cfg)
	if err != nil {
		t.Fatalf("policy.New: %v", err)
	}
	return p
}

func TestDefaultProfileIsReadOnly(t *testing.T) {
	// Starting with no profile configured must not be a way to get write
	// access. This is the single most important default in the server.
	p, err := policy.ParseProfile("")
	if err != nil {
		t.Fatalf("ParseProfile(\"\"): %v", err)
	}
	if p != policy.ProfileReadOnly {
		t.Fatalf("default profile = %s, want %s", p, policy.ProfileReadOnly)
	}
}

func TestUnknownProfileIsRejected(t *testing.T) {
	if _, err := policy.ParseProfile("superuser"); err == nil {
		t.Fatal("unknown profile accepted")
	}
}

func TestReadOnlyForbidsEveryMutation(t *testing.T) {
	p := mustPolicy(t, policy.Config{Profile: policy.ProfileReadOnly, AllowDestructive: true})

	for _, a := range registry.All() {
		err := p.AllowsAction(a.Name)
		class := registry.Classify(a.Name)
		switch class {
		case registry.ClassRead:
			if err != nil {
				t.Errorf("readonly forbade the read action %s: %v", a.Name, err)
			}
		default:
			if err == nil {
				t.Errorf("readonly permitted %s, which is classified %s", a.Name, class)
			}
		}
	}
}

func TestProfileSeparation(t *testing.T) {
	// The point of profiles: a support agent cannot move money, and a billing
	// agent cannot take a customer's server away.
	tests := []struct {
		profile policy.Profile
		action  string
		allowed bool
	}{
		{policy.ProfileSupport, "UpdateTicket", true},
		{policy.ProfileSupport, "AddTicketNote", true},
		{policy.ProfileSupport, "UpdateClient", true},
		{policy.ProfileSupport, "AddInvoicePayment", false},
		{policy.ProfileSupport, "CreateInvoice", false},
		{policy.ProfileSupport, "ModuleTerminate", false},

		{policy.ProfileBilling, "CreateInvoice", true},
		{policy.ProfileBilling, "AddInvoicePayment", true},
		{policy.ProfileBilling, "UpdateInvoice", true},
		{policy.ProfileBilling, "ModuleTerminate", false},
		{policy.ProfileBilling, "ModuleSuspend", false},
		{policy.ProfileBilling, "UpdateTicket", false},

		{policy.ProfileAdmin, "ModuleTerminate", true},
		{policy.ProfileAdmin, "AddInvoicePayment", true},
		{policy.ProfileAdmin, "UpdateTicket", true},

		// Reads are permitted everywhere: a support agent who cannot see an
		// invoice cannot answer a billing question.
		{policy.ProfileSupport, "GetInvoices", true},
		{policy.ProfileBilling, "GetTickets", true},
	}

	for _, tt := range tests {
		p := mustPolicy(t, policy.Config{Profile: tt.profile, AllowDestructive: true})
		err := p.AllowsAction(tt.action)
		if (err == nil) != tt.allowed {
			t.Errorf("%s profile, action %s: allowed = %v, want %v (err: %v)",
				tt.profile, tt.action, err == nil, tt.allowed, err)
		}
		if err != nil && errs.Coded(err).Code != errs.CodeForbidden {
			t.Errorf("%s/%s: code = %s, want %s", tt.profile, tt.action, errs.Coded(err).Code, errs.CodeForbidden)
		}
	}
}

func TestDestructiveIsOffByDefaultEvenForAdmin(t *testing.T) {
	// Being an administrator is a statement about authority, not about intent
	// to delete something right now.
	p := mustPolicy(t, policy.Config{Profile: policy.ProfileAdmin})

	for _, name := range []string{"DeleteClient", "ModuleTerminate", "AddInvoicePayment", "DomainRenew"} {
		if err := p.AllowsAction(name); err == nil {
			t.Errorf("admin without explicit enablement permitted the destructive action %s", name)
		}
	}
	// Non-destructive writes are still available.
	if err := p.AllowsAction("UpdateClient"); err != nil {
		t.Errorf("admin was refused a plain write: %v", err)
	}
}

func TestDestructiveEnablementWorks(t *testing.T) {
	p := mustPolicy(t, policy.Config{Profile: policy.ProfileAdmin, AllowDestructive: true})
	if err := p.AllowsAction("DeleteClient"); err != nil {
		t.Errorf("explicit destructive enablement did not take effect: %v", err)
	}
}

func TestBlockedActionsAreNeverPermitted(t *testing.T) {
	// No profile and no configuration may reach a credential oracle.
	for _, profile := range policy.Profiles() {
		p := mustPolicy(t, policy.Config{Profile: profile, AllowDestructive: true})
		for _, name := range []string{"GetClientPassword", "DecryptPassword", "CreateSsoToken", "ValidateLogin"} {
			if err := p.AllowsAction(name); err == nil {
				t.Errorf("%s profile permitted the blocked action %s", profile, name)
			}
		}
	}
}

func TestAllowlistOnlySubtracts(t *testing.T) {
	p := mustPolicy(t, policy.Config{
		Profile:   policy.ProfileAdmin,
		Allowlist: []string{"whmcs_client_get", "whmcs_invoice_list"},
	})

	if !p.AllowsTool("whmcs_client_get") {
		t.Error("allowlisted tool was refused")
	}
	if p.AllowsTool("whmcs_ticket_reply") {
		t.Error("a tool outside the allowlist was permitted")
	}

	// Naming a write tool under readonly must not enable it. The allowlist
	// narrows a profile; it cannot widen one.
	ro := mustPolicy(t, policy.Config{
		Profile:   policy.ProfileReadOnly,
		Allowlist: []string{"whmcs_client_update"},
	})
	if err := ro.AllowsAction("UpdateClient"); err == nil {
		t.Error("the allowlist escalated a readonly profile")
	}
}

func TestPermittedActionsShrinkWithProfile(t *testing.T) {
	ro := mustPolicy(t, policy.Config{Profile: policy.ProfileReadOnly})
	admin := mustPolicy(t, policy.Config{Profile: policy.ProfileAdmin, AllowDestructive: true})

	if len(ro.PermittedActions()) == 0 {
		t.Fatal("readonly permits no actions at all; reads should be available")
	}
	if len(ro.PermittedActions()) >= len(admin.PermittedActions()) {
		t.Errorf("readonly permits %d actions, admin permits %d; readonly must be a strict subset",
			len(ro.PermittedActions()), len(admin.PermittedActions()))
	}
}

func TestDenialExplainsItself(t *testing.T) {
	// A model told only "forbidden" retries with variations. One told why
	// reports back to the operator, which is the outcome we want.
	p := mustPolicy(t, policy.Config{Profile: policy.ProfileReadOnly})
	err := p.AllowsAction("DeleteClient")
	if err == nil {
		t.Fatal("expected a denial")
	}
	coded := errs.Coded(err)
	if coded.Details["profile"] != string(policy.ProfileReadOnly) {
		t.Errorf("denial does not name the active profile: %+v", coded.Details)
	}
	if coded.Retryable {
		t.Error("a policy denial is marked retryable; retrying cannot help")
	}
}
