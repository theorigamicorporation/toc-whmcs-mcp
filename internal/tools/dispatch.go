package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/audit"
	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/confirm"
	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/errs"
	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/registry"
	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/shape"
)

// dispatch runs one tool call through every stage.
//
// The order matters. Policy is checked before arguments are validated, so a
// forbidden call never reveals whether its arguments were well formed.
// Confirmation is checked before the request is built, so a destructive call
// with no token cannot reach the network by any path.
func dispatch(ctx context.Context, d Deps, def Tool, class registry.Class, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	started := time.Now()
	args := Args(req.GetArguments())
	if args == nil {
		args = Args{}
	}

	opts := shape.Options{
		IncludePII:   def.PIIOptIn && args.Bool("include_personal_details"),
		IncludeNotes: def.NotesOptIn && args.Bool("include_notes"),
	}

	op := audit.Operation{
		ID:           audit.NewOperationID(),
		Tool:         def.Name,
		Action:       def.Action,
		Class:        string(class),
		Profile:      string(d.Policy.Profile()),
		ArgKeys:      args.Keys(),
		PIIRequested: opts.IncludePII,
	}
	d.Audit.Start(ctx, op)

	result, err := def.run(ctx, d, class, args, opts, op.ID)

	op.Duration = durationSince(started)
	if err != nil {
		coded := errs.Coded(err)
		op.Outcome = "error"
		op.Code = string(coded.Code)
		d.Audit.Finish(ctx, op)
		return errorResult(coded), nil
	}

	op.Outcome = "success"
	d.Audit.Finish(ctx, op)
	return successResult(def.Name, result)
}

// run performs the call, returning the projected payload or a coded error.
func (t Tool) run(ctx context.Context, d Deps, class registry.Class, args Args, opts shape.Options, opID audit.OperationID) (any, error) {
	if t.Local != nil {
		return t.Local(ctx, d, args)
	}

	// The escape hatch resolves its own action, so it runs the policy and
	// confirmation stages against that action rather than a fixed one.
	if t.Action == anyAction {
		return t.runGeneric(ctx, d, args, opts, opID)
	}

	action, err := registry.Resolve(t.Action)
	if err != nil {
		return nil, err
	}
	if err := d.Policy.AllowsAction(action.Name); err != nil {
		d.Audit.Denied(ctx, opID, t.Name, action.Name, string(d.Policy.Profile()), errs.Coded(err).Message)
		return nil, err
	}

	if class.NeedsConfirmation() {
		if done, res, err := t.checkConfirmation(ctx, d, action.Name, args, opID); !done {
			return res, err
		}
	}

	params, err := t.Params(args, d.Limits)
	if err != nil {
		return nil, err
	}
	values, err := registry.Validate(action, params)
	if err != nil {
		return nil, err
	}

	resp, err := d.Client.Call(ctx, action, values)
	if err != nil {
		return nil, err
	}

	return t.Extract(resp.Data, t.Out, opts, d.Limits.resolve(), args), nil
}

// checkConfirmation implements the prepare/confirm protocol.
//
// It returns done=false with a preview result when no valid token was
// presented, which is the case that must never fall through to execution.
func (t Tool) checkConfirmation(ctx context.Context, d Deps, action string, args Args, opID audit.OperationID) (done bool, preview any, err error) {
	token := args.String(confirm.ArgKey)

	if token == "" {
		issued, issueErr := d.Confirm.Issue(action, args)
		if issueErr != nil {
			return false, nil, issueErr
		}
		d.Audit.ConfirmationIssued(ctx, opID, action, issued.ExpiresAt)

		impact := "This operation cannot be undone."
		if t.Preview != nil {
			impact = t.Preview(args)
		}
		return false, map[string]any{
			"status":             "confirmation_required",
			"impact":             impact,
			"action":             action,
			"confirmation_token": issued.Value,
			"expires_at":         issued.ExpiresAt.UTC().Format(time.RFC3339),
			"next_step": "Nothing has been changed. Show the impact statement to a human. " +
				"If they approve, call this tool again with identical arguments plus confirmation_token.",
		}, nil
	}

	if verr := d.Confirm.Verify(token, action, args); verr != nil {
		d.Audit.ConfirmationRejected(ctx, opID, action, string(errs.Coded(verr).Code))
		return false, nil, verr
	}
	d.Audit.ConfirmationConsumed(ctx, opID, action)
	return true, nil, nil
}

// successResult renders a projected payload as a structured MCP result.
//
// Results carry structuredContent so a client gets machine-readable data rather
// than having to parse prose. The text fallback exists for clients that do not
// support structured content; it is the same data, not a different rendering.
func successResult(tool string, payload any) (*mcp.CallToolResult, error) {
	if payload == nil {
		payload = map[string]any{"status": "ok"}
	}
	wrapped, ok := payload.(map[string]any)
	if !ok {
		wrapped = map[string]any{"result": payload}
	}

	text, err := json.MarshalIndent(wrapped, "", "  ")
	if err != nil {
		return errorResult(errs.Wrap(err, errs.CodeInternal, "encode result for %s", tool)), nil
	}
	return mcp.NewToolResultStructured(wrapped, string(text)), nil
}

// errorResult renders a coded error.
//
// Failures come back as tool results with isError set, never as protocol-level
// errors, and never as a successful result containing an error document. A
// client must be able to tell "this did not happen" from "this happened and
// returned nothing".
func errorResult(e *errs.Error) *mcp.CallToolResult {
	payload := map[string]any{
		"error":     true,
		"code":      string(e.Code),
		"message":   e.Message,
		"retryable": e.Retryable,
	}
	if len(e.Details) > 0 {
		payload["details"] = e.Details
	}

	text, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("%s: %s", e.Code, e.Message))
	}

	result := mcp.NewToolResultStructured(payload, string(text))
	result.IsError = true
	return result
}
