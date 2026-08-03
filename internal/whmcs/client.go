// Package whmcs is the transport to the WHMCS Admin API.
//
// It exists to serve the MCP layer, not to be a general SDK. Everything here is
// bounded: requests have deadlines, responses have a size cap, retries only
// happen for actions the registry classifies as reads, and a response is
// validated before any part of it is treated as data.
package whmcs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/errs"
	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/registry"
)

// Defaults chosen so that an unconfigured deployment is still bounded.
const (
	DefaultTimeout          = 30 * time.Second
	DefaultMaxResponseBytes = 8 << 20 // 8 MiB
	DefaultMaxRetries       = 2
	apiPath                 = "/includes/api.php"
)

// Config configures the client. Identifier and Secret are the WHMCS API
// credential pair; they are sent in the request body and must never appear in
// output.
type Config struct {
	BaseURL          string
	Identifier       string
	Secret           string
	Timeout          time.Duration
	MaxResponseBytes int64
	MaxRetries       int
	// AccessKey is the optional WHMCS API access key, required when the WHMCS
	// instance restricts API access by IP.
	AccessKey string
	// HTTPClient is injected by tests. Nil means a client built from Timeout.
	HTTPClient *http.Client
}

// Client calls the WHMCS Admin API.
type Client struct {
	endpoint   string
	identifier string
	secret     string
	accessKey  string
	maxBytes   int64
	maxRetries int
	http       *http.Client
	// scrubber removes credential values from any string that might be shown
	// to a caller. Belt and braces: credentials travel in the body, so they
	// should not appear in an error, but "should not" is not a control.
	scrubber *strings.Replacer
}

// New builds a client. It validates the configuration eagerly so a
// misconfigured deployment fails at startup rather than on first tool call.
func New(cfg Config) (*Client, error) {
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		return nil, fmt.Errorf("WHMCS base URL is required")
	}
	u, err := url.Parse(base)
	if err != nil {
		return nil, fmt.Errorf("WHMCS base URL is not a valid URL: %w", err)
	}
	if u.Scheme != "https" && u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" {
		// Credentials travel in the request body on every call. Plaintext to a
		// remote host would put them on the wire.
		return nil, fmt.Errorf("WHMCS base URL must use https (got %q)", u.Scheme)
	}
	if cfg.Identifier == "" || cfg.Secret == "" {
		return nil, fmt.Errorf("WHMCS API identifier and secret are both required")
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	maxBytes := cfg.MaxResponseBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxResponseBytes
	}
	retries := cfg.MaxRetries
	if retries < 0 {
		retries = DefaultMaxRetries
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: timeout}
	}

	// Do not scrub short secrets: replacing a two-character string would
	// mangle unrelated text and could itself leak information.
	var pairs []string
	for _, s := range []string{cfg.Secret, cfg.Identifier, cfg.AccessKey} {
		if len(s) >= 8 {
			pairs = append(pairs, s, "[redacted]")
		}
	}

	endpoint := base + apiPath
	if strings.HasSuffix(base, apiPath) {
		endpoint = base
	}

	return &Client{
		endpoint:   endpoint,
		identifier: cfg.Identifier,
		secret:     cfg.Secret,
		accessKey:  cfg.AccessKey,
		maxBytes:   maxBytes,
		maxRetries: retries,
		http:       httpClient,
		scrubber:   strings.NewReplacer(pairs...),
	}, nil
}

// Endpoint returns the resolved API endpoint, for logging. It contains no
// credentials.
func (c *Client) Endpoint() string { return c.endpoint }

// Response is a decoded WHMCS API response.
type Response struct {
	// Result is the vendor's "success" or "error" marker.
	Result string
	// Data is the full decoded payload.
	Data map[string]any
}

// Call executes an action. values must already have been validated against the
// registry; Call adds the action name and credentials.
//
// Retries are decided here rather than by the caller so that no call site can
// accidentally make a payment twice.
func (c *Client) Call(ctx context.Context, action registry.Action, values url.Values) (*Response, error) {
	form := url.Values{}
	for k, vs := range values {
		for _, v := range vs {
			form.Add(k, v)
		}
	}
	form.Set("action", action.Name)
	form.Set("identifier", c.identifier)
	form.Set("secret", c.secret)
	form.Set("responsetype", "json")
	if c.accessKey != "" {
		form.Set("accesskey", c.accessKey)
	}
	body := form.Encode()

	retriable := !registry.Classify(action.Name).Mutating()
	attempts := 1
	if retriable {
		attempts += c.maxRetries
	}

	var lastErr error
	for attempt := range attempts {
		if attempt > 0 {
			if err := sleep(ctx, backoff(attempt)); err != nil {
				return nil, err
			}
		}
		resp, err := c.do(ctx, body)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if !retriable || !errs.Coded(err).Retryable {
			return nil, err
		}
	}
	return nil, lastErr
}

// do performs one attempt.
func (c *Client) do(ctx context.Context, body string) (*Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, strings.NewReader(body))
	if err != nil {
		return nil, errs.Wrap(err, errs.CodeInternal, "build request")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, c.transportError(ctx, err)
	}
	defer func() {
		// Drain a little so the connection can be reused, but never the whole
		// body: that would defeat the size cap.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
	}()

	if err := statusError(resp.StatusCode); err != nil {
		return nil, err
	}

	// Read one byte past the cap so that hitting it exactly is distinguishable
	// from exceeding it.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, c.maxBytes+1))
	if err != nil {
		return nil, c.transportError(ctx, err)
	}
	if int64(len(raw)) > c.maxBytes {
		return nil, errs.New(errs.CodeResponseTooLarge,
			"WHMCS response exceeded the %d byte limit and was discarded; narrow the query with a smaller limit or more specific filters",
			c.maxBytes)
	}

	return c.decode(resp.Header.Get("Content-Type"), raw)
}

// decode validates and decodes a response body.
//
// A 200 status and a JSON content type are not evidence of a valid API
// response. A WHMCS instance behind maintenance mode, a login redirect, or a
// WAF will happily return HTML with 200, and treating that as an empty result
// is how "this client has no invoices" gets reported to an operator who is
// about to make a decision on it.
func (c *Client) decode(contentType string, raw []byte) (*Response, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return nil, errs.New(errs.CodeInvalidResponse, "WHMCS returned an empty response body")
	}
	if trimmed[0] != '{' && trimmed[0] != '[' {
		return nil, errs.New(errs.CodeInvalidResponse,
			"WHMCS returned %s instead of a JSON API response; check that the base URL points at the WHMCS root and that the instance is not in maintenance mode",
			describeBody(contentType, trimmed))
	}

	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, errs.Wrap(err, errs.CodeInvalidResponse,
			"WHMCS returned malformed JSON (%d bytes)", len(raw))
	}

	result, _ := data["result"].(string)
	if result == "" {
		return nil, errs.New(errs.CodeInvalidResponse,
			"WHMCS response has no result field; this is not an API response")
	}
	if !strings.EqualFold(result, "success") {
		msg, _ := data["message"].(string)
		if msg == "" {
			msg = result
		}
		return nil, errs.New(errs.CodeWHMCSError, "WHMCS reported an error: %s", c.scrub(msg)).
			WithDetails(map[string]any{"whmcs_result": result})
	}

	return &Response{Result: result, Data: data}, nil
}

// transportError classifies a transport failure. Cancellation and deadline are
// distinguished because only one of them is the caller's doing.
func (c *Client) transportError(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, context.Canceled) || ctx.Err() == context.Canceled:
		return errs.Wrap(err, errs.CodeCancelled, "the request was cancelled")
	case errors.Is(err, context.DeadlineExceeded) || isTimeout(err):
		return errs.Wrap(err, errs.CodeTimeout,
			"WHMCS did not respond before the request deadline")
	default:
		return errs.Wrap(err, errs.CodeUpstreamUnavailable,
			"could not reach WHMCS: %s", c.scrub(networkReason(err)))
	}
}

// statusError maps an HTTP status to a coded error, deciding retryability.
func statusError(code int) error {
	switch {
	case code >= 200 && code < 300:
		return nil
	case code == http.StatusUnauthorized || code == http.StatusForbidden:
		// Not retryable and not the model's fault: the credential is wrong or
		// the caller's IP is not allowlisted in WHMCS.
		return errs.New(errs.CodeForbidden,
			"WHMCS rejected the API credential (HTTP %d); check the identifier, secret, and API IP allowlist", code)
	case code == http.StatusTooManyRequests:
		return errs.New(errs.CodeUpstreamUnavailable, "WHMCS is rate limiting requests (HTTP %d)", code)
	case code >= 500:
		return errs.New(errs.CodeUpstreamUnavailable, "WHMCS returned HTTP %d", code)
	default:
		return errs.New(errs.CodeInvalidResponse, "WHMCS returned HTTP %d", code)
	}
}

// backoff returns the delay before the given attempt, with jitter so that
// several concurrent tool calls do not retry in lockstep.
func backoff(attempt int) time.Duration {
	base := time.Duration(1<<uint(attempt-1)) * 250 * time.Millisecond
	if base > 4*time.Second {
		base = 4 * time.Second
	}
	return base + time.Duration(rand.Int64N(int64(base/2)+1))
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return errs.Wrap(ctx.Err(), errs.CodeCancelled, "the request was cancelled while backing off")
	case <-t.C:
		return nil
	}
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

// networkReason renders a transport failure without the URL, because a URL can
// carry credentials and because the endpoint is not useful to a model.
func networkReason(err error) string {
	var ue *url.Error
	if errors.As(err, &ue) && ue.Err != nil {
		return ue.Err.Error()
	}
	return err.Error()
}

// describeBody characterises a non-JSON body for an error message without
// echoing its content, which could be an attacker-controlled page.
func describeBody(contentType, body string) string {
	switch {
	case strings.Contains(strings.ToLower(contentType), "html"),
		strings.HasPrefix(strings.ToLower(body), "<!doctype"),
		strings.HasPrefix(strings.ToLower(body), "<html"):
		return "an HTML page"
	case contentType != "":
		return fmt.Sprintf("a %s response", strings.SplitN(contentType, ";", 2)[0])
	default:
		return "a non-JSON response"
	}
}

func (c *Client) scrub(s string) string {
	if c.scrubber == nil {
		return s
	}
	return c.scrubber.Replace(s)
}
