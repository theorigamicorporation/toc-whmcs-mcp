package confirm_test

import (
	"testing"
	"time"

	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/confirm"
	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/errs"
)

func newStore(t *testing.T, opts ...confirm.Option) *confirm.Store {
	t.Helper()
	s, err := confirm.NewStore(opts...)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return s
}

func code(err error) errs.Code {
	if err == nil {
		return ""
	}
	return errs.Coded(err).Code
}

func TestIssueAndVerify(t *testing.T) {
	s := newStore(t)
	args := map[string]any{"serviceid": 42}

	tok, err := s.Issue("ModuleTerminate", args)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if tok.Value == "" {
		t.Fatal("issued an empty token")
	}
	if err := s.Verify(tok.Value, "ModuleTerminate", args); err != nil {
		t.Fatalf("Verify of a fresh token failed: %v", err)
	}
}

func TestTokenCannotMoveToAnotherTarget(t *testing.T) {
	// The reason tokens are bound to arguments rather than to a tool: a
	// confirmation for terminating service 42 must not terminate service 43.
	s := newStore(t)
	tok, err := s.Issue("ModuleTerminate", map[string]any{"serviceid": 42})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	err = s.Verify(tok.Value, "ModuleTerminate", map[string]any{"serviceid": 43})
	if code(err) != errs.CodeConfirmationMismatch {
		t.Fatalf("code = %s, want %s", code(err), errs.CodeConfirmationMismatch)
	}
}

func TestTokenCannotMoveToAnotherAction(t *testing.T) {
	s := newStore(t)
	args := map[string]any{"clientid": 7}
	tok, err := s.Issue("UpdateClient", args)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	if got := code(s.Verify(tok.Value, "DeleteClient", args)); got != errs.CodeConfirmationMismatch {
		t.Fatalf("code = %s, want %s", got, errs.CodeConfirmationMismatch)
	}
}

func TestTokenCannotBeInvented(t *testing.T) {
	// A model does not hold the signing key, so it cannot mint a token, and a
	// claim in the arguments that "the user approved" is not a token.
	s := newStore(t)
	for _, forged := range []string{
		"v1.abc.9999999999.deadbeef",
		"approved-by-user",
		"",
		"v1..0.",
	} {
		if err := s.Verify(forged, "ModuleTerminate", map[string]any{"serviceid": 1}); err == nil {
			t.Errorf("forged token %q was accepted", forged)
		}
	}
}

func TestTokenFromAnotherStoreIsRejected(t *testing.T) {
	// Each process generates its own key, so a restart invalidates outstanding
	// tokens and tokens are not portable between deployments.
	a, b := newStore(t), newStore(t)
	args := map[string]any{"serviceid": 9}
	tok, err := a.Issue("ModuleTerminate", args)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if got := code(b.Verify(tok.Value, "ModuleTerminate", args)); got != errs.CodeConfirmationMismatch {
		t.Fatalf("code = %s, want %s", got, errs.CodeConfirmationMismatch)
	}
}

func TestTokenExpires(t *testing.T) {
	now := time.Now()
	clock := func() time.Time { return now }
	s := newStore(t, confirm.WithTTL(time.Minute), confirm.WithClock(clock))

	args := map[string]any{"serviceid": 5}
	tok, err := s.Issue("ModuleTerminate", args)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	now = now.Add(2 * time.Minute)
	if got := code(s.Verify(tok.Value, "ModuleTerminate", args)); got != errs.CodeConfirmationExpired {
		t.Fatalf("code = %s, want %s", got, errs.CodeConfirmationExpired)
	}
}

func TestTokenIsSingleUse(t *testing.T) {
	// A retried tool call must not post the payment twice.
	s := newStore(t)
	args := map[string]any{"invoiceid": 100, "amount": 25.0}

	tok, err := s.Issue("AddInvoicePayment", args)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if err := s.Verify(tok.Value, "AddInvoicePayment", args); err != nil {
		t.Fatalf("first use failed: %v", err)
	}
	if got := code(s.Verify(tok.Value, "AddInvoicePayment", args)); got != errs.CodeConfirmationConsumed {
		t.Fatalf("second use code = %s, want %s", got, errs.CodeConfirmationConsumed)
	}
}

func TestPresentingTheTokenDoesNotChangeTheFingerprint(t *testing.T) {
	// The token argument is stripped before fingerprinting, so the arguments
	// that produced a token are the arguments that verify it.
	s := newStore(t)
	args := map[string]any{"serviceid": 3}

	tok, err := s.Issue("ModuleTerminate", args)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	withToken := map[string]any{"serviceid": 3, confirm.ArgKey: tok.Value}
	if err := s.Verify(tok.Value, "ModuleTerminate", withToken); err != nil {
		t.Fatalf("Verify with the token present in the arguments failed: %v", err)
	}
}

func TestArgumentOrderDoesNotMatter(t *testing.T) {
	s := newStore(t)
	issued := map[string]any{"invoiceid": 1, "gateway": "banktransfer", "amount": 10.0}
	presented := map[string]any{"amount": 10.0, "gateway": "banktransfer", "invoiceid": 1}

	tok, err := s.Issue("AddInvoicePayment", issued)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if err := s.Verify(tok.Value, "AddInvoicePayment", presented); err != nil {
		t.Fatalf("map ordering changed the fingerprint: %v", err)
	}
}

func TestTypeChangesAreDetected(t *testing.T) {
	// WHMCS treats 42 and "42" alike. A confirmation should not silently cover
	// both: the operator approved a specific rendering of the arguments.
	s := newStore(t)
	tok, err := s.Issue("ModuleTerminate", map[string]any{"serviceid": 42})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if err := s.Verify(tok.Value, "ModuleTerminate", map[string]any{"serviceid": "42"}); err == nil {
		t.Error("a type change was accepted as the same target")
	}
}

func TestActionNameIsCaseInsensitive(t *testing.T) {
	s := newStore(t)
	args := map[string]any{"serviceid": 1}
	tok, err := s.Issue("ModuleTerminate", args)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if err := s.Verify(tok.Value, "moduleterminate", args); err != nil {
		t.Errorf("case difference in the action name broke verification: %v", err)
	}
}

func TestConcurrentVerifyExecutesOnce(t *testing.T) {
	// Two concurrent presentations of one token must not both succeed.
	s := newStore(t)
	args := map[string]any{"serviceid": 77}
	tok, err := s.Issue("ModuleTerminate", args)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	const racers = 16
	results := make(chan error, racers)
	start := make(chan struct{})
	for range racers {
		go func() {
			<-start
			results <- s.Verify(tok.Value, "ModuleTerminate", args)
		}()
	}
	close(start)

	succeeded := 0
	for range racers {
		if err := <-results; err == nil {
			succeeded++
		}
	}
	if succeeded != 1 {
		t.Fatalf("%d of %d concurrent verifications succeeded, want exactly 1", succeeded, racers)
	}
}
