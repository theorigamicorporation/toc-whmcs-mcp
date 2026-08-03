package tools

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/audit"
	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/errs"
	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/redact"
	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/registry"
	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/shape"
	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/untrusted"
)

// anyAction marks the escape hatch, whose target action is chosen at call time.
const anyAction = "*"

// maxGenericResultBytes bounds an escape-hatch result. Curated tools are
// bounded by their projections; this one has no schema to bound it, so the
// limit is explicit.
const maxGenericResultBytes = 256 << 10

// untrustedGenericKeys name fields that commonly carry customer-authored text.
// The escape hatch has no per-action schema, so it wraps by name. This is
// necessarily approximate, which is one of the reasons curated tools are
// preferred where one exists.
var untrustedGenericKeys = []string{
	"message", "note", "notes", "subject", "body", "reply", "description",
	"comment", "content", "customfield", "answer", "question", "title",
}

// genericTools are the three tools that keep every WHMCS action reachable
// without advertising 162 of them.
//
// The split matters for context cost: listing is cheap and returns names only,
// describing is precise and returns one action's schema, calling is the only
// one that touches WHMCS. A model that needs an uncommon action pays for one
// schema, not all of them.
func genericTools() []Tool {
	return []Tool{
		{
			Name: "whmcs_list_actions",
			Desc: "List the WHMCS API actions this server can reach, with a one-line summary and safety " +
				"classification for each. Use this to discover an action when no purpose-built tool exists. " +
				"Returns names and summaries only, not parameter schemas; call whmcs_describe_action for those.",
			LocalReadOnly: true,
			Args: []Arg{
				{Name: "category", Type: "string", Desc: "Restrict to one category, e.g. Billing, Client, Tickets, Domains. Omit to list every category."},
				{Name: "search", Type: "string", Desc: "Case-insensitive substring match against action names and summaries."},
			},
			Out: shape.Spec{
				Title: "ActionList",
				Fields: []shape.Field{
					{Name: "actions", Type: "array", Desc: "Matching actions."},
					{Name: "categories", Type: "array", Desc: "Every available category."},
					{Name: "count", Type: "integer", Desc: "Number of actions returned."},
				},
			},
			Local: listActions,
		},
		{
			Name: "whmcs_describe_action",
			Desc: "Return the full parameter schema for one WHMCS action: every parameter with its type, " +
				"whether it is required, and what it means. Call this before whmcs_call_action so the call is " +
				"built from the actual schema rather than guessed.",
			LocalReadOnly: true,
			Args: []Arg{
				{Name: "action", Type: "string", Required: true, Desc: "The action name, e.g. AddOrder. Case-insensitive."},
			},
			Out: shape.Spec{
				Title: "ActionSchema",
				Fields: []shape.Field{
					{Name: "action", Desc: "The action name."},
					{Name: "category", Desc: "Documentation category."},
					{Name: "summary", Desc: "One-line description."},
					{Name: "classification", Desc: "read, write or destructive."},
					{Name: "mutating", Type: "boolean", Desc: "Whether the action changes state."},
					{Name: "needs_confirmation", Type: "boolean", Desc: "Whether calling it requires a confirmation token."},
					{Name: "permitted", Type: "boolean", Desc: "Whether the active profile permits this action."},
					{Name: "denied_reason", Desc: "Why the action is not permitted, when it is not."},
					{Name: "documentation", Desc: "Vendor documentation URL. Authoritative; this server's copy may lag."},
					{Name: "source", Desc: "Attribution for the schema data."},
					{Name: "parameters", Type: "array", Desc: "Request parameters."},
				},
			},
			Local: describeAction,
		},
		{
			Name:   "whmcs_call_action",
			Action: anyAction,
			Desc: "Call any WHMCS API action by name. This is the escape hatch for actions with no purpose-built " +
				"tool. Prefer a purpose-built tool where one exists: this one filters its output by denylist " +
				"rather than projecting it onto a declared schema, so it returns more of the raw response, and " +
				"it is annotated destructive because its target is chosen at call time. Parameters are validated " +
				"against the action's real schema before the call, and the active profile, the confirmation " +
				"protocol and redaction all apply exactly as they do to a purpose-built tool.",
			Args: []Arg{
				{Name: "action", Type: "string", Required: true, Desc: "The action name, e.g. GetAutomationLog. Call whmcs_describe_action first to learn its parameters."},
				{Name: "parameters", Type: "object", Desc: "The action's request parameters as an object. Unknown or misspelled names are rejected rather than ignored."},
			},
			Out: shape.Spec{
				Title: "ActionResult",
				Fields: []shape.Field{
					{Name: "action", Desc: "The action that was called."},
					{Name: "result", Desc: "The WHMCS result marker."},
					{Name: "data", Type: "object", Desc: "The response, denylist-filtered."},
				},
			},
			Preview: func(args Args) string {
				return "Calls the WHMCS action " + args.String("action") +
					", which this server classifies as destructive. It cannot be undone from here."
			},
			// Params and Extract are unused for the escape hatch; runGeneric
			// handles it. They are set so validate() passes uniformly.
			Params:  func(Args, Limits) (map[string]any, error) { return nil, nil },
			Extract: func(map[string]any, shape.Spec, shape.Options, Limits, Args) any { return nil },
		},
	}
}

func listActions(_ context.Context, d Deps, args Args) (any, error) {
	category := args.String("category")
	search := strings.ToLower(args.String("search"))

	var actions []map[string]any
	for _, a := range registry.All() {
		if registry.Classify(a.Name) == registry.ClassBlocked {
			continue
		}
		if category != "" && !strings.EqualFold(a.Category, category) {
			continue
		}
		if search != "" &&
			!strings.Contains(strings.ToLower(a.Name), search) &&
			!strings.Contains(strings.ToLower(a.Summary), search) {
			continue
		}
		entry := registry.Summarise(a)
		// Tell the caller up front whether this profile can actually run it,
		// so it does not discover the refusal by attempting the call.
		entry["permitted"] = d.Policy.AllowsAction(a.Name) == nil
		actions = append(actions, entry)
	}

	return map[string]any{
		"actions":    actions,
		"categories": registry.Categories(),
		"count":      len(actions),
		"profile":    string(d.Policy.Profile()),
	}, nil
}

func describeAction(_ context.Context, d Deps, args Args) (any, error) {
	name := args.String("action")
	if name == "" {
		return nil, errs.New(errs.CodeInvalidParams, "the action parameter is required")
	}
	a, err := registry.Resolve(name)
	if err != nil {
		return nil, err
	}

	out := registry.Describe(a)
	if permErr := d.Policy.AllowsAction(a.Name); permErr != nil {
		out["permitted"] = false
		out["denied_reason"] = errs.Coded(permErr).Message
	} else {
		out["permitted"] = true
	}
	return out, nil
}

// runGeneric executes whmcs_call_action.
//
// It resolves the action from the arguments and then runs exactly the same
// stages a curated tool runs. The escape hatch must not be a way around policy,
// so the checks live here rather than being inherited from a fixed action.
func (t Tool) runGeneric(ctx context.Context, d Deps, args Args, opts shape.Options, opID audit.OperationID) (any, error) {
	name := args.String("action")
	if name == "" {
		return nil, errs.New(errs.CodeInvalidParams, "the action parameter is required").
			WithDetails(map[string]any{"hint": "call whmcs_list_actions to discover action names"})
	}

	action, err := registry.Resolve(name)
	if err != nil {
		return nil, err
	}
	if err := d.Policy.AllowsAction(action.Name); err != nil {
		d.Audit.Denied(ctx, opID, t.Name, action.Name, string(d.Policy.Profile()), errs.Coded(err).Message)
		return nil, err
	}

	params, err := genericParams(args)
	if err != nil {
		return nil, err
	}

	// Confirmation is bound to the resolved action and its parameters, not to
	// the literal tool arguments, so a token cannot be carried from one target
	// to another by reshaping the wrapper.
	if registry.Classify(action.Name).NeedsConfirmation() {
		bound := Args{"action": action.Name}
		for k, v := range params {
			bound[k] = v
		}
		if tok := args.String("confirmation_token"); tok != "" {
			bound["confirmation_token"] = tok
		}
		if done, preview, cerr := t.checkConfirmation(ctx, d, action.Name, bound, opID); !done {
			return preview, cerr
		}
	}

	values, err := registry.Validate(action, params)
	if err != nil {
		return nil, err
	}

	resp, err := d.Client.Call(ctx, action, values)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"action": action.Name,
		"result": resp.Result,
		"data":   filterGeneric(resp.Data, opts),
	}, nil
}

// genericParams extracts and shallow-validates the parameters object.
func genericParams(args Args) (map[string]any, error) {
	raw, ok := args["parameters"]
	if !ok || raw == nil {
		return map[string]any{}, nil
	}
	params, ok := raw.(map[string]any)
	if !ok {
		// Some clients serialise an object argument as a JSON string. Parsing
		// it is friendlier than refusing, and the result is validated against
		// the action's real schema either way.
		if encoded, isString := raw.(string); isString {
			if strings.TrimSpace(encoded) == "" {
				return map[string]any{}, nil
			}
			if err := json.Unmarshal([]byte(encoded), &params); err != nil {
				return nil, errs.New(errs.CodeInvalidParams,
					"parameters must be an object mapping parameter names to values; "+
						"received a string that is not valid JSON")
			}
		} else {
			return nil, errs.New(errs.CodeInvalidParams,
				"parameters must be an object mapping parameter names to values, got %T", raw)
		}
	}
	for k, v := range params {
		switch v.(type) {
		case map[string]any, []any:
			return nil, errs.New(errs.CodeInvalidParams,
				"parameter %q must be a scalar value; WHMCS takes form-encoded parameters, not nested structures", k)
		}
	}
	return params, nil
}

// filterGeneric applies denylist redaction, untrusted-content wrapping and a
// size cap to an arbitrary response.
//
// This is weaker than the allowlist projection curated tools get, and the tool
// description says so. It is the price of full API coverage without a 162-tool
// listing.
func filterGeneric(data map[string]any, opts shape.Options) any {
	cleaned := redact.Map(data, redact.Options{
		IncludePII:   opts.IncludePII,
		IncludeNotes: opts.IncludeNotes,
	})
	wrapped := wrapUntrusted(cleaned, 0)

	encoded, err := json.Marshal(wrapped)
	if err == nil && len(encoded) > maxGenericResultBytes {
		return map[string]any{
			"_truncated": true,
			"_notice": "The response exceeded the size limit for a generic action call and was withheld. " +
				"Narrow the query with a smaller limit or more specific filters, or use a purpose-built tool.",
			"_size_bytes":  len(encoded),
			"_limit_bytes": maxGenericResultBytes,
		}
	}
	return wrapped
}

// wrapUntrusted envelopes string values whose key suggests customer-authored
// content.
func wrapUntrusted(v any, depth int) any {
	if depth > redact.MaxDepth {
		return v
	}
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			if s, ok := val.(string); ok && s != "" && looksUntrusted(k) {
				out[k] = untrusted.Wrap("whmcs:"+k, s)
				continue
			}
			out[k] = wrapUntrusted(val, depth+1)
		}
		return out
	case []any:
		out := make([]any, 0, len(t))
		for _, val := range t {
			out = append(out, wrapUntrusted(val, depth+1))
		}
		return out
	default:
		return v
	}
}

func looksUntrusted(key string) bool {
	k := strings.ToLower(key)
	for _, s := range untrustedGenericKeys {
		if strings.Contains(k, s) {
			return true
		}
	}
	return false
}

// statusTool reports the server's effective security posture. An operator (or
// an agent asked "what can you do here") should be able to find that out
// without reading the deployment's environment.
func statusTool() Tool {
	return Tool{
		Name: "whmcs_status",
		Desc: "Report this server's effective security posture: the active profile, whether destructive " +
			"actions are enabled, how many WHMCS actions are permitted, and the page size limits. " +
			"Makes no WHMCS call and reveals no customer data.",
		LocalReadOnly: true,
		Out: shape.Spec{
			Title: "ServerStatus",
			Fields: []shape.Field{
				{Name: "profile", Desc: "The active capability profile."},
				{Name: "destructive_enabled", Type: "boolean", Desc: "Whether destructive actions are enabled."},
				{Name: "permitted_action_count", Type: "integer", Desc: "How many WHMCS actions this profile permits."},
				{Name: "total_action_count", Type: "integer", Desc: "How many actions exist in the registry."},
				{Name: "default_page_size", Type: "integer", Desc: "Page size applied when a tool call omits limit."},
				{Name: "max_page_size", Type: "integer", Desc: "Ceiling applied to any requested limit."},
				{Name: "confirmation_ttl_seconds", Type: "integer", Desc: "How long a confirmation token stays valid."},
			},
		},
		Local: func(_ context.Context, d Deps, _ Args) (any, error) {
			permitted := d.Policy.PermittedActions()
			sort.Strings(permitted)
			lim := d.Limits.resolve()
			return map[string]any{
				"profile":                  string(d.Policy.Profile()),
				"destructive_enabled":      d.Policy.DestructiveEnabled(),
				"permitted_action_count":   len(permitted),
				"total_action_count":       registry.Count(),
				"default_page_size":        lim.DefaultPageSize,
				"max_page_size":            lim.MaxPageSize,
				"confirmation_ttl_seconds": int(d.Confirm.TTL().Seconds()),
			}, nil
		},
	}
}
