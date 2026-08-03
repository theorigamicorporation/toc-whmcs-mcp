// Package errs defines the typed errors that cross package boundaries and the
// stable machine-readable codes they map to in MCP tool results.
//
// Every failure surfaced to a caller carries a Code. Callers distinguish "you
// asked for something you are not allowed to do" from "WHMCS is down" without
// parsing prose, and the retryable flag tells an agent whether trying again can
// possibly help.
package errs

import (
	"errors"
	"fmt"
)

// Code is a stable, machine-readable failure classification. These strings are
// part of the tool contract: renaming one is a breaking change.
type Code string

const (
	// CodeInvalidParams covers arguments rejected before any network call:
	// missing required fields, unknown fields, bad types, out-of-range values.
	CodeInvalidParams Code = "invalid_params"

	// CodeUnknownAction means the requested WHMCS action is not in the registry.
	CodeUnknownAction Code = "unknown_action"

	// CodeForbidden means the active profile, allowlist, or a hard block
	// forbids this operation. Changing arguments will not help.
	CodeForbidden Code = "forbidden"

	// CodeConfirmationRequired means a consequential operation was invoked
	// without a confirmation token. The result carries a preview and a token.
	CodeConfirmationRequired Code = "confirmation_required"

	// CodeConfirmationMismatch means the token was issued for a different
	// action or different arguments than the ones presented.
	CodeConfirmationMismatch Code = "confirmation_mismatch"

	// CodeConfirmationExpired means the token's TTL elapsed.
	CodeConfirmationExpired Code = "confirmation_expired"

	// CodeConfirmationConsumed means the token was already used. The operation
	// it authorised has already run exactly once.
	CodeConfirmationConsumed Code = "confirmation_consumed"

	// CodeWHMCSError is an application-level error reported by WHMCS itself.
	CodeWHMCSError Code = "whmcs_error"

	// CodeInvalidResponse means WHMCS returned something that is not a valid
	// API response: an HTML page, malformed JSON, a missing result field.
	CodeInvalidResponse Code = "invalid_response"

	// CodeResponseTooLarge means the response exceeded the configured cap and
	// was not buffered. Narrow the query.
	CodeResponseTooLarge Code = "response_too_large"

	// CodeUpstreamUnavailable covers transport failures: connection refused,
	// DNS failure, 5xx after retries.
	CodeUpstreamUnavailable Code = "upstream_unavailable"

	// CodeTimeout means the request exceeded its deadline.
	CodeTimeout Code = "timeout"

	// CodeCancelled means the caller's context was cancelled mid-flight.
	CodeCancelled Code = "cancelled"

	// CodeInternal is the fallback for defects. It should be rare and is never
	// retryable from the caller's point of view.
	CodeInternal Code = "internal"
)

// Error is a coded failure. Message is safe to show a caller: it must never
// contain credentials or customer content.
type Error struct {
	Code    Code
	Message string
	// Retryable reports whether an identical retry could plausibly succeed.
	Retryable bool
	// Details carries structured, non-sensitive context, such as the list of
	// accepted parameter names when one was misspelled.
	Details map[string]any
	// wrapped is the underlying cause, kept for errors.Is/As but never rendered
	// to the caller, since it may carry a URL with credentials in it.
	wrapped error
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error { return e.wrapped }

// New builds a coded error. Retryability is derived from the code so that two
// call sites cannot disagree about whether the same failure is retryable.
func New(code Code, format string, a ...any) *Error {
	return &Error{
		Code:      code,
		Message:   fmt.Sprintf(format, a...),
		Retryable: retryable(code),
	}
}

// Wrap builds a coded error carrying an underlying cause. The cause is
// available to errors.Is and errors.As but is never included in Message,
// because upstream errors routinely embed the request URL.
func Wrap(err error, code Code, format string, a ...any) *Error {
	e := New(code, format, a...)
	e.wrapped = err
	return e
}

// WithDetails attaches structured context and returns the same error, so it can
// be used inline at a return site.
func (e *Error) WithDetails(d map[string]any) *Error {
	e.Details = d
	return e
}

// retryable is the single definition of which failures are worth repeating.
// Note that write actions are additionally barred from automatic retry by the
// client, regardless of what this reports.
func retryable(code Code) bool {
	switch code {
	case CodeUpstreamUnavailable, CodeTimeout:
		return true
	default:
		return false
	}
}

// Coded extracts the *Error from an error chain, or synthesises an internal
// error. Every boundary that renders an error to a caller goes through this, so
// an unclassified error becomes CodeInternal rather than leaking a raw string.
func Coded(err error) *Error {
	if err == nil {
		return nil
	}
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return &Error{
		Code:      CodeInternal,
		Message:   "internal error",
		Retryable: false,
		wrapped:   err,
	}
}
