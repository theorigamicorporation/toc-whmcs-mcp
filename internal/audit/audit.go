// Package audit records what the agent actually did.
//
// Every tool invocation gets a unique operation ID. Confirmation issuance and
// consumption are recorded against the same ID, so "who authorised this
// termination" is answerable from the log alone.
//
// Records carry field names and counts, never values: the audit stream must not
// become the leak channel that the redactor closed. Records go to stderr or a
// configured sink, never to stdout, because stdout is the MCP stdio transport
// and a stray byte there corrupts the protocol.
package audit

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"io"
	"log/slog"
	"sort"
	"time"
)

// Logger writes audit records.
type Logger struct {
	log *slog.Logger
	now func() time.Time
}

// New builds a logger writing JSON records to w.
func New(w io.Writer, level slog.Level) *Logger {
	return &Logger{
		log: slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level})),
		now: time.Now,
	}
}

// NewWithClock is New with an injected clock, for tests.
func NewWithClock(w io.Writer, level slog.Level, now func() time.Time) *Logger {
	l := New(w, level)
	if now != nil {
		l.now = now
	}
	return l
}

// OperationID is a per-invocation correlation ID.
type OperationID string

// NewOperationID mints a correlation ID.
func NewOperationID() OperationID {
	b := make([]byte, 9)
	if _, err := rand.Read(b); err != nil {
		// A degraded ID is better than failing a tool call over entropy. It is
		// still unique enough to correlate within a process lifetime.
		return OperationID("op-" + time.Now().Format("150405.000000000"))
	}
	return OperationID("op-" + base64.RawURLEncoding.EncodeToString(b))
}

// Operation describes one tool invocation.
type Operation struct {
	ID       OperationID
	Tool     string
	Action   string
	Class    string
	Profile  string
	ArgKeys  []string
	Outcome  string
	Code     string
	Duration time.Duration
	// Confirmed reports whether the call presented a valid confirmation token.
	Confirmed bool
	// PIIRequested reports whether the caller opted into full personal detail.
	// Deliberate access to a full contact record should be greppable.
	PIIRequested bool
}

// Start records the beginning of an operation and returns its ID.
func (l *Logger) Start(ctx context.Context, op Operation) OperationID {
	if op.ID == "" {
		op.ID = NewOperationID()
	}
	l.log.LogAttrs(ctx, slog.LevelInfo, "tool.start",
		slog.String("op", string(op.ID)),
		slog.String("tool", op.Tool),
		slog.String("action", op.Action),
		slog.String("class", op.Class),
		slog.String("profile", op.Profile),
		slog.Any("arg_keys", sortedCopy(op.ArgKeys)),
		slog.Int("arg_count", len(op.ArgKeys)),
		slog.Bool("confirmed", op.Confirmed),
		slog.Bool("pii_requested", op.PIIRequested),
	)
	return op.ID
}

// Finish records the outcome of an operation.
func (l *Logger) Finish(ctx context.Context, op Operation) {
	level := slog.LevelInfo
	if op.Outcome != "success" {
		level = slog.LevelWarn
	}
	l.log.LogAttrs(ctx, level, "tool.finish",
		slog.String("op", string(op.ID)),
		slog.String("tool", op.Tool),
		slog.String("action", op.Action),
		slog.String("class", op.Class),
		slog.String("outcome", op.Outcome),
		slog.String("code", op.Code),
		slog.Duration("duration", op.Duration),
	)
}

// ConfirmationIssued records that a preview handed out a token.
func (l *Logger) ConfirmationIssued(ctx context.Context, id OperationID, action string, expires time.Time) {
	l.log.LogAttrs(ctx, slog.LevelInfo, "confirmation.issued",
		slog.String("op", string(id)),
		slog.String("action", action),
		slog.Time("expires_at", expires),
	)
}

// ConfirmationConsumed records that a token authorised an execution. This and
// the issuance share the executed mutation's operation ID, so the pair is one
// grep away from each other.
func (l *Logger) ConfirmationConsumed(ctx context.Context, id OperationID, action string) {
	l.log.LogAttrs(ctx, slog.LevelInfo, "confirmation.consumed",
		slog.String("op", string(id)),
		slog.String("action", action),
	)
}

// ConfirmationRejected records a token that failed verification, which is the
// signal worth alerting on: it means something replayed or forged a token.
func (l *Logger) ConfirmationRejected(ctx context.Context, id OperationID, action, code string) {
	l.log.LogAttrs(ctx, slog.LevelWarn, "confirmation.rejected",
		slog.String("op", string(id)),
		slog.String("action", action),
		slog.String("code", code),
	)
}

// Denied records a policy refusal.
func (l *Logger) Denied(ctx context.Context, id OperationID, tool, action, profile, reason string) {
	l.log.LogAttrs(ctx, slog.LevelWarn, "policy.denied",
		slog.String("op", string(id)),
		slog.String("tool", tool),
		slog.String("action", action),
		slog.String("profile", profile),
		slog.String("reason", reason),
	)
}

// Startup records the effective security posture once, at boot, so a log always
// says what the process was allowed to do.
func (l *Logger) Startup(ctx context.Context, attrs ...slog.Attr) {
	l.log.LogAttrs(ctx, slog.LevelInfo, "server.start", attrs...)
}

// Slog exposes the underlying slog logger for non-audit diagnostics that must
// share the same sink.
func (l *Logger) Slog() *slog.Logger { return l.log }

func sortedCopy(s []string) []string {
	out := make([]string, len(s))
	copy(out, s)
	sort.Strings(out)
	return out
}
