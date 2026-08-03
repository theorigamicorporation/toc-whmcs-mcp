package whmcs_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/errs"
	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/registry"
	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/whmcs"
	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/whmcs/whmcstest"
)

const (
	testIdentifier = "test-identifier-value"
	testSecret     = "test-secret-value-0123456789"
)

func newClient(t *testing.T, f *whmcstest.Fake, tweak func(*whmcs.Config)) *whmcs.Client {
	t.Helper()
	cfg := whmcs.Config{
		BaseURL:    f.URL(),
		Identifier: testIdentifier,
		Secret:     testSecret,
		Timeout:    2 * time.Second,
		MaxRetries: 2,
	}
	if tweak != nil {
		tweak(&cfg)
	}
	c, err := whmcs.New(cfg)
	if err != nil {
		t.Fatalf("whmcs.New: %v", err)
	}
	return c
}

func action(t *testing.T, name string) registry.Action {
	t.Helper()
	a, ok := registry.Lookup(name)
	if !ok {
		t.Fatalf("action %s not in registry", name)
	}
	return a
}

func code(err error) errs.Code {
	if err == nil {
		return ""
	}
	return errs.Coded(err).Code
}

func TestCallSendsCredentialsAndAction(t *testing.T) {
	f := whmcstest.New(t)
	f.On("GetClients", whmcstest.Success(`"clients":{"client":[]}`))
	c := newClient(t, f, nil)

	_, err := c.Call(context.Background(), action(t, "GetClients"), url.Values{"limitnum": {"5"}})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}

	reqs := f.Requests()
	if len(reqs) != 1 {
		t.Fatalf("got %d requests, want 1", len(reqs))
	}
	got := reqs[0]
	for field, want := range map[string]string{
		"action":       "GetClients",
		"identifier":   testIdentifier,
		"secret":       testSecret,
		"responsetype": "json",
		"limitnum":     "5",
	} {
		if got.Get(field) != want {
			t.Errorf("form field %s = %q, want %q", field, got.Get(field), want)
		}
	}
}

func TestNewRejectsIncompleteConfig(t *testing.T) {
	f := whmcstest.New(t)
	tests := map[string]whmcs.Config{
		"no base url":   {Identifier: "i", Secret: "s"},
		"no identifier": {BaseURL: f.URL(), Secret: "s"},
		"no secret":     {BaseURL: f.URL(), Identifier: "i"},
		// Credentials travel in the body of every request. Plaintext to a
		// remote host would put them on the wire.
		"plaintext remote": {BaseURL: "http://billing.example.com", Identifier: "i", Secret: "s"},
	}
	for name, cfg := range tests {
		if _, err := whmcs.New(cfg); err == nil {
			t.Errorf("%s: New succeeded, want an error", name)
		}
	}
}

func TestWHMCSApplicationErrorIsTyped(t *testing.T) {
	f := whmcstest.New(t)
	// WHMCS reports application errors with HTTP 200 and result:error.
	f.On("GetClients", whmcstest.APIError("Invalid IP 203.0.113.7"))
	c := newClient(t, f, nil)

	_, err := c.Call(context.Background(), action(t, "GetClients"), nil)
	if code(err) != errs.CodeWHMCSError {
		t.Fatalf("code = %s, want %s (err: %v)", code(err), errs.CodeWHMCSError, err)
	}
	if !strings.Contains(err.Error(), "Invalid IP") {
		t.Errorf("the WHMCS message was lost: %v", err)
	}
}

func TestHTMLResponseIsNotTreatedAsData(t *testing.T) {
	f := whmcstest.New(t)
	// A WHMCS in maintenance mode returns an HTML page with HTTP 200. Decoding
	// that as an empty result would report "this client has no invoices" to an
	// operator about to make a decision on it.
	f.On("GetInvoices", whmcstest.HTML())
	c := newClient(t, f, nil)

	resp, err := c.Call(context.Background(), action(t, "GetInvoices"), nil)
	if err == nil {
		t.Fatalf("HTML accepted as a response: %+v", resp)
	}
	if code(err) != errs.CodeInvalidResponse {
		t.Errorf("code = %s, want %s", code(err), errs.CodeInvalidResponse)
	}
	if !strings.Contains(err.Error(), "HTML") {
		t.Errorf("error does not identify the HTML page: %v", err)
	}
}

func TestMalformedAndIncompleteJSONRejected(t *testing.T) {
	tests := map[string]whmcstest.Reply{
		"malformed":      whmcstest.JSON(`{"result":`),
		"no result key":  whmcstest.JSON(`{"clients":[]}`),
		"empty body":     whmcstest.JSON(``),
		"json but plain": {Status: http.StatusOK, ContentType: "text/plain", Body: "OK"},
	}
	for name, reply := range tests {
		t.Run(name, func(t *testing.T) {
			f := whmcstest.New(t)
			f.On("GetClients", reply)
			c := newClient(t, f, nil)

			if _, err := c.Call(context.Background(), action(t, "GetClients"), nil); code(err) != errs.CodeInvalidResponse {
				t.Errorf("code = %s, want %s (err: %v)", code(err), errs.CodeInvalidResponse, err)
			}
		})
	}
}

func TestResponseSizeCap(t *testing.T) {
	f := whmcstest.New(t)
	f.On("GetClients", whmcstest.JSON(`{"result":"success","padding":"`+strings.Repeat("x", 4096)+`"}`))
	c := newClient(t, f, func(cfg *whmcs.Config) { cfg.MaxResponseBytes = 1024 })

	_, err := c.Call(context.Background(), action(t, "GetClients"), nil)
	if code(err) != errs.CodeResponseTooLarge {
		t.Fatalf("code = %s, want %s (err: %v)", code(err), errs.CodeResponseTooLarge, err)
	}
	if !strings.Contains(err.Error(), "narrow the query") {
		t.Errorf("error does not tell the caller how to recover: %v", err)
	}
}

func TestTimeoutIsBounded(t *testing.T) {
	f := whmcstest.New(t)
	f.On("GetClients", whmcstest.Reply{
		Status:      http.StatusOK,
		ContentType: "application/json",
		Body:        `{"result":"success"}`,
		Delay:       2 * time.Second,
	})
	c := newClient(t, f, func(cfg *whmcs.Config) {
		cfg.Timeout = 100 * time.Millisecond
		cfg.MaxRetries = 0
	})

	start := time.Now()
	_, err := c.Call(context.Background(), action(t, "GetClients"), nil)
	elapsed := time.Since(start)

	if code(err) != errs.CodeTimeout {
		t.Fatalf("code = %s, want %s (err: %v)", code(err), errs.CodeTimeout, err)
	}
	if elapsed > time.Second {
		t.Errorf("call took %s; the timeout was not enforced", elapsed)
	}
}

func TestCancellationPropagates(t *testing.T) {
	f := whmcstest.New(t)
	f.On("GetClients", whmcstest.Reply{
		Status:      http.StatusOK,
		ContentType: "application/json",
		Body:        `{"result":"success"}`,
		Delay:       2 * time.Second,
	})
	c := newClient(t, f, func(cfg *whmcs.Config) { cfg.MaxRetries = 0 })

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	if _, err := c.Call(ctx, action(t, "GetClients"), nil); code(err) != errs.CodeCancelled {
		t.Fatalf("code = %s, want %s (err: %v)", code(err), errs.CodeCancelled, err)
	}
}

func TestReadsRetryTransientFailures(t *testing.T) {
	f := whmcstest.New(t)
	f.On("GetClients",
		whmcstest.Status(http.StatusServiceUnavailable),
		whmcstest.Status(http.StatusBadGateway),
		whmcstest.Success(`"clients":{"client":[]}`),
	)
	c := newClient(t, f, nil)

	if _, err := c.Call(context.Background(), action(t, "GetClients"), nil); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if got := f.CallCount("GetClients"); got != 3 {
		t.Errorf("attempts = %d, want 3", got)
	}
}

func TestWritesAreNeverRetried(t *testing.T) {
	// The point of the whole retry policy: a payment must not be posted twice
	// because a gateway returned 503 after WHMCS had already recorded it.
	for _, name := range []string{"AddInvoicePayment", "AddClient", "DeleteClient"} {
		t.Run(name, func(t *testing.T) {
			f := whmcstest.New(t)
			f.Always(whmcstest.Status(http.StatusServiceUnavailable))
			c := newClient(t, f, nil)

			_, err := c.Call(context.Background(), action(t, name), nil)
			if code(err) != errs.CodeUpstreamUnavailable {
				t.Fatalf("code = %s, want %s", code(err), errs.CodeUpstreamUnavailable)
			}
			if got := f.CallCount(name); got != 1 {
				t.Errorf("%s was attempted %d times; write actions must never be retried", name, got)
			}
		})
	}
}

func TestRetriesAreBounded(t *testing.T) {
	f := whmcstest.New(t)
	f.Always(whmcstest.Status(http.StatusServiceUnavailable))
	c := newClient(t, f, func(cfg *whmcs.Config) { cfg.MaxRetries = 2 })

	if _, err := c.Call(context.Background(), action(t, "GetClients"), nil); code(err) != errs.CodeUpstreamUnavailable {
		t.Fatalf("code = %s, want %s", code(err), errs.CodeUpstreamUnavailable)
	}
	if got := f.CallCount("GetClients"); got != 3 {
		t.Errorf("attempts = %d, want 3 (1 + 2 retries)", got)
	}
}

func TestAuthFailureIsNotRetried(t *testing.T) {
	f := whmcstest.New(t)
	f.Always(whmcstest.Status(http.StatusForbidden))
	c := newClient(t, f, nil)

	_, err := c.Call(context.Background(), action(t, "GetClients"), nil)
	if code(err) != errs.CodeForbidden {
		t.Fatalf("code = %s, want %s", code(err), errs.CodeForbidden)
	}
	if got := f.CallCount("GetClients"); got != 1 {
		t.Errorf("attempts = %d; a rejected credential must not be retried", got)
	}
	if errs.Coded(err).Retryable {
		t.Error("a credential rejection is marked retryable")
	}
}

func TestWHMCSReasonSurvivesA4xx(t *testing.T) {
	// WHMCS explains itself in the body even on a 403. An IP missing from the
	// API allowlist looks exactly like a wrong secret unless that body is
	// reported, and the two have completely different fixes.
	f := whmcstest.New(t)
	f.Always(whmcstest.Reply{
		Status:      http.StatusForbidden,
		ContentType: "application/json",
		Body:        `{"result":"error","message":"Invalid IP 203.0.113.7"}`,
	})
	c := newClient(t, f, nil)

	_, err := c.Call(context.Background(), action(t, "GetClients"), nil)
	if code(err) != errs.CodeForbidden {
		t.Fatalf("code = %s, want %s", code(err), errs.CodeForbidden)
	}
	if !strings.Contains(err.Error(), "Invalid IP 203.0.113.7") {
		t.Errorf("the WHMCS reason was discarded, leaving only a status code: %v", err)
	}
	if remedy, _ := errs.Coded(err).Details["remedy"].(string); remedy == "" {
		t.Error("no remedy offered for a rejection the operator has to go and fix")
	}
}

func TestBareForbiddenStillExplainsItself(t *testing.T) {
	// A WAF or proxy returns 403 with no WHMCS body. The message must still
	// point somewhere useful rather than being empty.
	f := whmcstest.New(t)
	f.Always(whmcstest.Reply{Status: http.StatusForbidden, ContentType: "text/html", Body: "<html>blocked</html>"})
	c := newClient(t, f, nil)

	_, err := c.Call(context.Background(), action(t, "GetClients"), nil)
	if code(err) != errs.CodeForbidden {
		t.Fatalf("code = %s, want %s", code(err), errs.CodeForbidden)
	}
	if !strings.Contains(err.Error(), "IP allowlist") {
		t.Errorf("error does not suggest what to check: %v", err)
	}
}

func TestCredentialsNeverAppearInErrors(t *testing.T) {
	// WHMCS echoing a credential back in an error message, or a transport error
	// carrying the request, must not put the secret in front of the model.
	f := whmcstest.New(t)
	f.Always(whmcstest.APIError("bad credential " + testSecret + " for " + testIdentifier))
	c := newClient(t, f, nil)

	_, err := c.Call(context.Background(), action(t, "GetClients"), nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), testSecret) {
		t.Error("the API secret leaked into an error message")
	}
	if strings.Contains(err.Error(), testIdentifier) {
		t.Error("the API identifier leaked into an error message")
	}
}

func TestUnreachableHostIsUpstreamUnavailable(t *testing.T) {
	c, err := whmcs.New(whmcs.Config{
		// Reserved for documentation, guaranteed not to resolve to a service.
		BaseURL:    "https://localhost:1",
		Identifier: testIdentifier,
		Secret:     testSecret,
		Timeout:    time.Second,
		MaxRetries: 0,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, callErr := c.Call(context.Background(), action(t, "GetClients"), nil)
	if code(callErr) != errs.CodeUpstreamUnavailable {
		t.Fatalf("code = %s, want %s (err: %v)", code(callErr), errs.CodeUpstreamUnavailable, callErr)
	}
	if !errs.Coded(callErr).Retryable {
		t.Error("an unreachable upstream should be marked retryable")
	}
}

func TestSuccessfulResponseIsReturnedStructured(t *testing.T) {
	f := whmcstest.New(t)
	f.On("GetInvoice", whmcstest.Success(`"invoiceid":42,"status":"Paid"`))
	c := newClient(t, f, nil)

	resp, err := c.Call(context.Background(), action(t, "GetInvoice"), url.Values{"invoiceid": {"42"}})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if resp.Result != "success" {
		t.Errorf("Result = %q, want success", resp.Result)
	}
	if resp.Data["status"] != "Paid" {
		t.Errorf("payload not decoded: %+v", resp.Data)
	}
}
