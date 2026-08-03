package audit_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/audit"
)

func decode(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var records []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("audit record is not JSON: %q", line)
		}
		records = append(records, rec)
	}
	return records
}

func TestOperationsAreAttributable(t *testing.T) {
	var buf bytes.Buffer
	log := audit.New(&buf, slog.LevelInfo)
	ctx := context.Background()

	op := audit.Operation{
		Tool:    "whmcs_service_terminate",
		Action:  "ModuleTerminate",
		Class:   "destructive",
		Profile: "admin",
		ArgKeys: []string{"service_id"},
	}
	op.ID = log.Start(ctx, op)
	op.Outcome = "success"
	op.Duration = 12 * time.Millisecond
	log.Finish(ctx, op)

	records := decode(t, &buf)
	if len(records) != 2 {
		t.Fatalf("got %d records, want a start and a finish", len(records))
	}
	if records[0]["op"] == "" || records[0]["op"] != records[1]["op"] {
		t.Error("the start and finish records do not share an operation ID")
	}
	if records[1]["outcome"] != "success" {
		t.Errorf("outcome = %v, want success", records[1]["outcome"])
	}
	if records[0]["tool"] != "whmcs_service_terminate" {
		t.Error("the record does not name the tool")
	}
}

func TestConfirmationIsTraceableEndToEnd(t *testing.T) {
	var buf bytes.Buffer
	log := audit.New(&buf, slog.LevelInfo)
	ctx := context.Background()

	id := audit.NewOperationID()
	log.ConfirmationIssued(ctx, id, "ModuleTerminate", time.Now().Add(time.Minute))
	log.ConfirmationConsumed(ctx, id, "ModuleTerminate")

	records := decode(t, &buf)
	if len(records) != 2 {
		t.Fatalf("got %d records, want 2", len(records))
	}
	// Both events carry the executed mutation's operation ID, so the pair is
	// one grep away from each other.
	if records[0]["op"] != records[1]["op"] {
		t.Error("issuance and consumption are not correlated")
	}
	if records[0]["msg"] != "confirmation.issued" || records[1]["msg"] != "confirmation.consumed" {
		t.Errorf("unexpected event names: %v, %v", records[0]["msg"], records[1]["msg"])
	}
}

func TestRejectedConfirmationIsAWarning(t *testing.T) {
	// A rejected token means something replayed or forged one, which is the
	// signal worth alerting on.
	var buf bytes.Buffer
	log := audit.New(&buf, slog.LevelInfo)

	log.ConfirmationRejected(context.Background(), audit.NewOperationID(), "DeleteClient", "confirmation_mismatch")

	records := decode(t, &buf)
	if records[0]["level"] != "WARN" {
		t.Errorf("level = %v, want WARN", records[0]["level"])
	}
}

func TestAuditRecordsCarryNoValues(t *testing.T) {
	// The audit stream must not become the leak channel the redactor closed.
	// Argument names and counts are recorded; argument values are not.
	var buf bytes.Buffer
	log := audit.New(&buf, slog.LevelInfo)

	op := audit.Operation{
		Tool:    "whmcs_ticket_reply",
		Action:  "AddTicketReply",
		Class:   "destructive",
		Profile: "support",
		ArgKeys: []string{"ticket_id", "message"},
	}
	op.ID = log.Start(context.Background(), op)
	op.Outcome = "success"
	log.Finish(context.Background(), op)

	rendered := buf.String()
	// Neither the customer's content nor a credential should ever be here,
	// because neither was passed in: only the key names were.
	for _, forbidden := range []string{"hunter2", "the customer wrote this", "ada@example.com"} {
		if strings.Contains(rendered, forbidden) {
			t.Errorf("a value leaked into the audit stream: %s", forbidden)
		}
	}
	if !strings.Contains(rendered, "ticket_id") || !strings.Contains(rendered, "message") {
		t.Error("argument names were not recorded; the record is not useful for review")
	}
	if !strings.Contains(rendered, `"arg_count":2`) {
		t.Error("argument count was not recorded")
	}
}

func TestPIIAccessIsGreppable(t *testing.T) {
	// A deliberate full-contact-record read should be findable after the fact.
	var buf bytes.Buffer
	log := audit.New(&buf, slog.LevelInfo)

	log.Start(context.Background(), audit.Operation{
		Tool:         "whmcs_client_get",
		Action:       "GetClientsDetails",
		PIIRequested: true,
	})

	if !strings.Contains(buf.String(), `"pii_requested":true`) {
		t.Error("a personal-detail opt-in was not recorded")
	}
}

func TestDenialIsRecorded(t *testing.T) {
	var buf bytes.Buffer
	log := audit.New(&buf, slog.LevelInfo)

	log.Denied(context.Background(), audit.NewOperationID(),
		"whmcs_call_action", "DeleteClient", "readonly", "the server is running in the readonly profile")

	records := decode(t, &buf)
	if records[0]["msg"] != "policy.denied" {
		t.Errorf("msg = %v, want policy.denied", records[0]["msg"])
	}
	if records[0]["profile"] != "readonly" {
		t.Error("the denial does not record the active profile")
	}
}

func TestOperationIDsAreUnique(t *testing.T) {
	seen := make(map[audit.OperationID]bool)
	for range 1000 {
		id := audit.NewOperationID()
		if seen[id] {
			t.Fatalf("duplicate operation ID: %s", id)
		}
		seen[id] = true
	}
}

func TestRecordsAreOneJSONObjectPerLine(t *testing.T) {
	// Audit goes to stderr under the stdio transport. It must be parseable by
	// ordinary log tooling, and it must never be written to stdout, which is
	// the MCP channel.
	var buf bytes.Buffer
	log := audit.New(&buf, slog.LevelInfo)

	for range 3 {
		log.Start(context.Background(), audit.Operation{Tool: "whmcs_status"})
	}
	if got := len(decode(t, &buf)); got != 3 {
		t.Errorf("decoded %d records, want 3", got)
	}
}
