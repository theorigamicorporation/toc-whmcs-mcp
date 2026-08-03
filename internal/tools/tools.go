// Package tools defines the MCP tool surface and the single dispatcher that
// every tool call passes through.
//
// The dispatcher is the reason the safety properties hold. Policy, the
// confirmation protocol, pagination clamping, projection, redaction and audit
// are applied here, once, for every tool. A tool definition supplies data:
// which action it calls, how its arguments map to WHMCS parameters, and what
// shape its result has. It never gets to skip a stage, because it never gets to
// build an MCP result itself.
package tools

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/audit"
	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/confirm"
	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/policy"
	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/registry"
	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/shape"
	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/whmcs"
)

// MaxAdvertisedTools bounds the tool listing. Exceeding it is a defect: the
// whole point of the curated-plus-escape-hatch design is that the listing stays
// small enough for real MCP clients and for useful tool selection.
const MaxAdvertisedTools = 30

// Limits bounds collection results.
type Limits struct {
	DefaultPageSize int
	MaxPageSize     int
}

// DefaultLimits are used when configuration does not override them.
func DefaultLimits() Limits {
	return Limits{DefaultPageSize: 25, MaxPageSize: 200}
}

func (l Limits) resolve() Limits {
	if l.DefaultPageSize <= 0 {
		l.DefaultPageSize = 25
	}
	if l.MaxPageSize <= 0 {
		l.MaxPageSize = 200
	}
	if l.DefaultPageSize > l.MaxPageSize {
		l.DefaultPageSize = l.MaxPageSize
	}
	return l
}

// Deps are the collaborators the dispatcher needs.
type Deps struct {
	Client  *whmcs.Client
	Policy  *policy.Policy
	Confirm *confirm.Store
	Audit   *audit.Logger
	Limits  Limits
}

// Arg is one tool input parameter.
type Arg struct {
	Name     string
	Desc     string
	Type     string // "string", "integer", "number", "boolean"
	Required bool
	Enum     []string
}

// Tool is a declarative tool definition.
type Tool struct {
	Name string
	Desc string
	// Action is the WHMCS action this tool performs. It drives the policy
	// check and the MCP annotations, so a tool cannot advertise itself as
	// read-only while calling something that is not.
	Action string
	Args   []Arg
	Out    shape.Spec

	// Params maps validated tool arguments onto WHMCS request parameters.
	Params func(args Args, lim Limits) (map[string]any, error)
	// Extract pulls the payload out of the WHMCS response and projects it onto
	// the declared output shape. It receives the projection spec rather than
	// the raw response alone, so a handler cannot return unprojected data.
	Extract func(data map[string]any, out shape.Spec, opts shape.Options, lim Limits, args Args) any
	// Preview describes what a destructive call would do, in one sentence, for
	// the confirmation step. Required for destructive tools.
	Preview func(args Args) string

	// PIIOptIn adds an include_personal_details argument.
	PIIOptIn bool
	// NotesOptIn adds an include_notes argument.
	NotesOptIn bool
	// Paginated adds limit and offset arguments.
	Paginated bool

	// Local marks a tool that answers from local state and makes no WHMCS call.
	Local func(ctx context.Context, d Deps, args Args) (any, error)
	// LocalReadOnly is the annotation for a Local tool, which has no action to
	// derive it from.
	LocalReadOnly bool
}

// Args is a tool's decoded arguments with typed accessors.
type Args map[string]any

// String returns a string argument.
func (a Args) String(name string) string {
	s, _ := a[name].(string)
	return strings.TrimSpace(s)
}

// Int returns an integer argument and whether it was present and integral.
//
// A string must be an integer in its entirety. Partial parsing is refused
// because it silently changes the target: "88x" parsing as 88 would show a
// human "service_id: 88x" in a confirmation preview while terminating service
// 88. The same principle the registry applies to a misspelled parameter, that
// it is an error rather than something to be salvaged, applies here.
func (a Args) Int(name string) (int, bool) {
	switch v := a[name].(type) {
	case float64:
		if v != float64(int(v)) {
			return 0, false
		}
		return int(v), true
	case int:
		return v, true
	case int64:
		return int(v), true
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return 0, false
		}
		return n, true
	default:
		return 0, false
	}
}

// Bool returns a boolean argument.
func (a Args) Bool(name string) bool {
	switch v := a[name].(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(v, "true") || v == "1"
	default:
		return false
	}
}

// Has reports whether an argument was supplied and non-empty.
func (a Args) Has(name string) bool {
	v, ok := a[name]
	if !ok || v == nil {
		return false
	}
	if s, isStr := v.(string); isStr {
		return strings.TrimSpace(s) != ""
	}
	return true
}

// Keys returns the supplied argument names, for the audit record. Names only:
// values may be customer data or, on a write, the content being written.
func (a Args) Keys() []string {
	out := make([]string, 0, len(a))
	for k := range a {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Page resolves limit and offset, clamping to the configured maximum.
//
// An unbounded query is the cheapest way to exhaust an agent's context and to
// put avoidable load on a billing system, so "no limit" is never an option.
func (a Args) Page(lim Limits) (limit, offset int, clamped bool) {
	lim = lim.resolve()
	limit = lim.DefaultPageSize
	if n, ok := a.Int("limit"); ok && n > 0 {
		limit = n
	}
	if limit > lim.MaxPageSize {
		limit = lim.MaxPageSize
		clamped = true
	}
	if n, ok := a.Int("offset"); ok && n > 0 {
		offset = n
	}
	return limit, offset, clamped
}

// Register validates and registers every permitted tool with the MCP server,
// returning the names registered.
//
// Tools the policy forbids are not registered at all. An advertised tool that
// always fails teaches a model to keep trying it, and it misrepresents what the
// server can do.
func Register(s *server.MCPServer, d Deps, defs []Tool) ([]string, error) {
	d.Limits = d.Limits.resolve()

	var registered []string
	for _, def := range defs {
		if err := def.validate(); err != nil {
			return nil, err
		}

		allowlisted := d.Policy.AllowsTool(def.Name)
		if !allowlisted {
			continue
		}
		// The escape hatch resolves its action per call, so there is no fixed
		// action to check here; runGeneric checks the resolved one.
		if def.Local == nil && def.Action != anyAction {
			if err := d.Policy.AllowsAction(def.Action); err != nil {
				// The allowlist can only subtract. If an operator explicitly
				// listed a tool the profile does not grant, say so at startup
				// rather than leaving them to wonder where it went.
				if d.Policy.HasAllowlist() {
					d.Policy.NoteIgnoredAllowlistEntry(def.Name)
				}
				continue
			}
		}

		tool, handler := build(d, def)
		s.AddTool(tool, handler)
		registered = append(registered, def.Name)
	}

	if len(registered) > MaxAdvertisedTools {
		return nil, fmt.Errorf(
			"%d tools would be advertised, over the limit of %d; the tool surface must stay small enough for MCP clients and for useful tool selection",
			len(registered), MaxAdvertisedTools)
	}
	sort.Strings(registered)
	return registered, nil
}

func (t Tool) validate() error {
	if t.Name == "" || t.Desc == "" {
		return fmt.Errorf("tool %q: name and description are required", t.Name)
	}
	if err := t.Out.Validate(); err != nil {
		return fmt.Errorf("tool %s: %w", t.Name, err)
	}
	if t.Local != nil {
		return nil
	}
	if t.Action == "" {
		return fmt.Errorf("tool %s: no WHMCS action declared", t.Name)
	}
	if _, ok := registry.Lookup(t.Action); !ok && t.Action != anyAction {
		return fmt.Errorf("tool %s: action %s is not in the registry", t.Name, t.Action)
	}
	if t.Params == nil || t.Extract == nil {
		return fmt.Errorf("tool %s: Params and Extract are required", t.Name)
	}
	if t.class().NeedsConfirmation() && t.Preview == nil {
		return fmt.Errorf("tool %s is destructive but declares no impact preview", t.Name)
	}
	return nil
}

// class is the tool's safety classification, taken from the registry rather
// than declared by the tool, so the two cannot disagree.
func (t Tool) class() registry.Class {
	if t.Local != nil {
		if t.LocalReadOnly {
			return registry.ClassRead
		}
		return registry.ClassWrite
	}
	if t.Action == anyAction {
		// The escape hatch chooses its action at call time, so it must be
		// annotated for the worst case it can reach.
		return registry.ClassDestructive
	}
	return registry.Classify(t.Action)
}

// build turns a definition into an MCP tool and its handler.
func build(d Deps, def Tool) (mcp.Tool, server.ToolHandlerFunc) {
	class := def.class()

	opts := []mcp.ToolOption{
		mcp.WithDescription(def.describe(class)),
		// Annotations are derived, never hand-set, so a client can trust that
		// readOnlyHint false really means this tool can change something.
		mcp.WithReadOnlyHintAnnotation(!class.Mutating()),
		mcp.WithDestructiveHintAnnotation(class == registry.ClassDestructive),
		mcp.WithIdempotentHintAnnotation(!class.Mutating()),
		// Every tool reaches a system outside this process.
		mcp.WithOpenWorldHintAnnotation(true),
	}

	for _, a := range def.allArgs() {
		opts = append(opts, a.option())
	}
	opts = append(opts, mcp.WithRawOutputSchema(def.outputSchema()))

	tool := mcp.NewTool(def.Name, opts...)

	handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return dispatch(ctx, d, def, class, req)
	}
	return tool, handler
}

// describe assembles the tool description, appending the machine-relevant
// safety facts. A description is documentation, not a control, but a model that
// is told an operation is irreversible selects it less carelessly.
func (t Tool) describe(class registry.Class) string {
	var b strings.Builder
	b.WriteString(t.Desc)

	switch class {
	case registry.ClassDestructive:
		b.WriteString("\n\nIRREVERSIBLE. This operation cannot be undone by this server. ")
		b.WriteString("The first call returns a preview and a confirmation_token and changes nothing; ")
		b.WriteString("pass that token back to execute. Show the preview to a human before confirming.")
	case registry.ClassWrite:
		b.WriteString("\n\nThis operation modifies data in the billing system.")
	}
	if t.Action != "" && t.Action != anyAction {
		b.WriteString("\n\nWHMCS action: " + t.Action + ".")
	}
	return b.String()
}

// allArgs returns the declared arguments plus the ones the dispatcher adds.
func (t Tool) allArgs() []Arg {
	args := make([]Arg, 0, len(t.Args)+4)
	args = append(args, t.Args...)

	if t.Paginated {
		args = append(args,
			Arg{Name: "limit", Type: "integer", Desc: "Maximum records to return. Clamped to the server maximum."},
			Arg{Name: "offset", Type: "integer", Desc: "Records to skip, for paging through results."},
		)
	}
	if t.PIIOptIn {
		args = append(args, Arg{
			Name: "include_personal_details",
			Type: "boolean",
			Desc: "Include postal address, phone number and tax identifier. Off by default. Set this only when the task genuinely requires the full contact record; the access is audited.",
		})
	}
	if t.NotesOptIn {
		args = append(args, Arg{
			Name: "include_notes",
			Type: "boolean",
			Desc: "Include internal admin notes. Off by default. These are written for staff, not for customers.",
		})
	}
	if t.class().NeedsConfirmation() {
		args = append(args, Arg{
			Name: confirm.ArgKey,
			Type: "string",
			Desc: "The confirmation token returned by a previous preview call. Omit it to get a preview. Tokens are issued by this server, are bound to these exact arguments, expire, and work once.",
		})
	}
	return args
}

func (a Arg) option() mcp.ToolOption {
	props := []mcp.PropertyOption{mcp.Description(a.Desc)}
	if a.Required {
		props = append(props, mcp.Required())
	}
	if len(a.Enum) > 0 {
		props = append(props, mcp.Enum(a.Enum...))
	}
	switch a.Type {
	case "integer", "number":
		return mcp.WithNumber(a.Name, props...)
	case "boolean":
		return mcp.WithBoolean(a.Name, props...)
	default:
		return mcp.WithString(a.Name, props...)
	}
}

// durationSince is extracted so the audit record and the result agree.
func durationSince(t time.Time) time.Duration { return time.Since(t) }
