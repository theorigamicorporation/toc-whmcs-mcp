// Package whmcstest provides an in-process fake WHMCS instance.
//
// The whole test suite runs against this. No test may require a live WHMCS
// instance, credentials, or network access: a test suite that only passes when
// pointed at production is not a test suite, and one that prints real customer
// records is a data leak.
package whmcstest

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"
)

// Reply is what the fake returns for one request.
type Reply struct {
	Status      int
	ContentType string
	Body        string
	// Delay stalls the response, for exercising timeouts and cancellation.
	Delay time.Duration
}

// JSON builds a successful WHMCS-shaped reply.
func JSON(body string) Reply {
	return Reply{Status: http.StatusOK, ContentType: "application/json", Body: body}
}

// Success builds a minimal successful reply carrying the given JSON fields.
func Success(fields string) Reply {
	if fields == "" {
		return JSON(`{"result":"success"}`)
	}
	return JSON(`{"result":"success",` + fields + `}`)
}

// APIError builds a WHMCS application-level error reply, which the vendor
// returns with HTTP 200.
func APIError(message string) Reply {
	return JSON(fmt.Sprintf(`{"result":"error","message":%q}`, message))
}

// HTML builds the maintenance-page or login-redirect reply that a
// misconfigured WHMCS returns with HTTP 200. Treating this as data is the
// failure mode the client's response validation exists to prevent.
func HTML() Reply {
	return Reply{
		Status:      http.StatusOK,
		ContentType: "text/html; charset=utf-8",
		Body:        "<!DOCTYPE html><html><body>Site is in maintenance mode</body></html>",
	}
}

// Status builds a bare HTTP status reply.
func Status(code int) Reply {
	return Reply{Status: code, ContentType: "text/plain", Body: http.StatusText(code)}
}

// Fake is a fake WHMCS endpoint.
type Fake struct {
	Server *httptest.Server

	mu       sync.Mutex
	replies  map[string][]Reply
	fallback Reply
	requests []url.Values
}

// New starts a fake. It is stopped automatically when the test ends.
func New(t *testing.T) *Fake {
	t.Helper()
	f := &Fake{
		replies:  make(map[string][]Reply),
		fallback: Success(""),
	}
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.Server.Close)
	return f
}

// URL is the base URL to configure a client with.
func (f *Fake) URL() string { return f.Server.URL }

// On queues replies for an action. Queued replies are consumed in order; once
// exhausted the fallback is used, which is what makes retry tests readable:
// queue two failures and assert the third attempt succeeds.
func (f *Fake) On(action string, replies ...Reply) *Fake {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replies[action] = append(f.replies[action], replies...)
	return f
}

// Always sets the reply used when an action has no queued replies left.
func (f *Fake) Always(r Reply) *Fake {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fallback = r
	return f
}

// Requests returns the form values of every request received, in order.
func (f *Fake) Requests() []url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]url.Values, len(f.requests))
	copy(out, f.requests)
	return out
}

// CallCount returns how many requests were received for an action.
func (f *Fake) CallCount(action string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, r := range f.requests {
		if r.Get("action") == action {
			n++
		}
	}
	return n
}

// Reset clears recorded requests and queued replies.
func (f *Fake) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = nil
	f.replies = make(map[string][]Reply)
}

func (f *Fake) serve(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	form, _ := url.ParseQuery(string(raw))

	f.mu.Lock()
	f.requests = append(f.requests, form)
	action := form.Get("action")
	reply := f.fallback
	if queued := f.replies[action]; len(queued) > 0 {
		reply = queued[0]
		f.replies[action] = queued[1:]
	}
	f.mu.Unlock()

	if reply.Delay > 0 {
		select {
		case <-time.After(reply.Delay):
		case <-r.Context().Done():
			return
		}
	}

	if reply.ContentType != "" {
		w.Header().Set("Content-Type", reply.ContentType)
	}
	status := reply.Status
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	_, _ = io.WriteString(w, reply.Body)
}
