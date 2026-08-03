package app_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/config"
	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/policy"
	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/whmcs/whmcstest"
)

// These audit the tool surface as a client sees it, rather than as the Go code
// declares it. Every defect they guard against was found by using the server,
// not by testing it, because the tests exercised internal types and skipped the
// JSON schema in between.

// adminHarness advertises every tool, so an audit covers the whole surface.
func adminHarness(t *testing.T) *harness {
	t.Helper()
	return newHarness(t, func(c *config.Config) {
		c.Profile = policy.ProfileAdmin
		c.AllowDestructive = true
	})
}

// publishedSchema decodes the input schema a client actually receives.
func publishedSchema(t *testing.T, tool mcp.Tool) map[string]struct {
	Type string `json:"type"`
} {
	t.Helper()
	raw, err := json.Marshal(tool.InputSchema)
	if err != nil {
		t.Fatalf("%s: marshal input schema: %v", tool.Name, err)
	}
	var decoded struct {
		Properties map[string]struct {
			Type string `json:"type"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("%s: decode input schema: %v", tool.Name, err)
	}
	return decoded.Properties
}

func TestEveryArgumentPublishesAConcreteType(t *testing.T) {
	// whmcs_call_action shipped with `parameters` published as a string,
	// because Arg.option() had no object case and fell through to WithString.
	// Every client sent a string and every call failed. An argument with no
	// declared type, or a type the schema builder does not understand, is the
	// same bug waiting to happen.
	h := adminHarness(t)

	valid := map[string]bool{"string": true, "integer": true, "number": true, "boolean": true, "object": true, "array": true}

	for _, tool := range h.listTools(t) {
		for name, prop := range publishedSchema(t, tool) {
			if prop.Type == "" {
				t.Errorf("%s.%s publishes no type; a client cannot know what to send", tool.Name, name)
				continue
			}
			if !valid[prop.Type] {
				t.Errorf("%s.%s publishes type %q, which is not a JSON Schema type", tool.Name, name, prop.Type)
			}
		}
	}
}

func TestStructuredArgumentsArePublishedAsStructured(t *testing.T) {
	// The specific regression. An argument that carries structure must not be
	// published as a string, whatever the Go side calls it.
	h := adminHarness(t)

	structured := map[string][]string{
		"whmcs_call_action": {"parameters"},
	}

	for _, tool := range h.listTools(t) {
		want, ok := structured[tool.Name]
		if !ok {
			continue
		}
		props := publishedSchema(t, tool)
		for _, name := range want {
			if got := props[name].Type; got != "object" {
				t.Errorf("%s.%s publishes as %q, want object", tool.Name, name, got)
			}
		}
	}
}

func TestNoToolReturnsGoDebugFormatting(t *testing.T) {
	// shape.Untrusted stringifies its value. Applied to a slice or a map that
	// produced Go's map[...] syntax in the model's context, burying the text an
	// operator was trying to read. shape.Spec.Validate now refuses it, and a
	// server that violates it fails to build, so reaching here means the guard
	// held for every shipped tool.
	h := adminHarness(t)
	h.fake.Always(whmcsTicketWithReplies())

	res := h.call(t, "whmcs_ticket_get", map[string]any{"ticket_id": 1})
	body := resultText(t, res)

	for _, marker := range []string{"map[", "]map", "0x"} {
		if strings.Contains(body, marker) {
			t.Errorf("result contains Go debug formatting %q: %s", marker, body)
		}
	}
}

func TestUntrustedEnvelopesWrapProseNotIdentifiers(t *testing.T) {
	// The escape hatch wrapped lastreply, a timestamp, and replyid, a number,
	// because both contain the substring "reply". Envelopes around values that
	// are obviously not prose are noise, and noise around a safety marker
	// teaches a reader to skip it.
	h := adminHarness(t)
	h.fake.Always(whmcsTicketWithReplies())

	res := h.call(t, "whmcs_call_action", map[string]any{
		"action":     "GetTicket",
		"parameters": map[string]any{"ticketid": 1},
	})
	structuredOut := structured(t, res)

	data, _ := structuredOut["data"].(map[string]any)
	for _, key := range []string{"lastreply", "replyid", "ticketid", "id"} {
		if v, ok := data[key].(map[string]any); ok && v["_untrusted"] == true {
			t.Errorf("%q was wrapped as untrusted content; it is an identifier or a timestamp", key)
		}
	}
	// The message still must be wrapped.
	replies, _ := data["replies"].(map[string]any)
	list, _ := replies["reply"].([]any)
	if len(list) > 0 {
		first, _ := list[0].(map[string]any)
		msg, ok := first["message"].(map[string]any)
		if !ok || msg["_untrusted"] != true {
			t.Error("the reply message is not wrapped as untrusted content")
		}
	}
}

func TestDocumentedToolsMatchAdvertisedTools(t *testing.T) {
	// docs/tools.md is the reference an operator reads. A tool added without a
	// docs entry, or removed without one, makes that reference misleading in a
	// way nobody notices until someone follows it.
	h := adminHarness(t)

	doc, err := os.ReadFile("../../docs/tools.md")
	if err != nil {
		t.Fatalf("read docs/tools.md: %v", err)
	}
	documented := string(doc)

	for _, tool := range h.listTools(t) {
		if !strings.Contains(documented, tool.Name) {
			t.Errorf("%s is advertised but absent from docs/tools.md", tool.Name)
		}
	}
}

// whmcsTicketWithReplies is a GetTicket response with a conversation, an
// identifier and a timestamp, covering every field the audits above assert on.
func whmcsTicketWithReplies() whmcstest.Reply {
	return whmcstest.JSON(`{"result":"success","id":1,"ticketid":1,"tid":"ABC-1",
	 "status":"Open","lastreply":"2026-07-22 15:30:01","subject":"help please",
	 "replies":{"reply":[{"date":"2026-07-22 15:30:01","name":"A Customer",
	   "email":"c@example.com","admin":"","replyid":"0",
	   "message":"my site is down, please help"}]}}`)
}
