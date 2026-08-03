// Package shape projects WHMCS responses onto declared output fields.
//
// A curated tool declares the fields it returns. Everything else in the
// upstream response is dropped, so a field WHMCS adds in a future version is
// excluded by default rather than included by default.
//
// This is also where the redaction pipeline is made structural. A handler
// returns a Spec and a source map; only Project turns that into output. There
// is no code path where a handler hands back an unprojected upstream response,
// because the dispatcher does not accept one.
package shape

import (
	"fmt"
	"strings"

	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/redact"
	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/untrusted"
)

// Kind classifies an output field, which decides how it is treated rather than
// whether it appears.
type Kind int

const (
	// Plain is ordinary, non-sensitive data written by staff or the system.
	Plain Kind = iota
	// Untrusted is customer-authored text. It is wrapped in an envelope.
	Untrusted
	// PII is personal data. Withheld unless the caller opts into full detail.
	PII
	// Notes is internal admin commentary. Withheld unless explicitly requested.
	Notes
)

// Field is one declared output field.
type Field struct {
	// Name is the key in the tool result.
	Name string
	// From is the key in the WHMCS response. Empty means the same as Name.
	From string
	// Type is the JSON Schema type reported in the tool's outputSchema.
	Type string
	// Desc documents the field in the output schema.
	Desc string
	// Kind decides redaction and envelope treatment.
	Kind Kind
	// Origin labels an Untrusted field's provenance, e.g. "ticket_reply".
	Origin string
}

func (f Field) source() string {
	if f.From != "" {
		return f.From
	}
	return f.Name
}

func (f Field) jsonType() string {
	if f.Type != "" {
		return f.Type
	}
	return "string"
}

// Spec is the declared shape of a tool's result object.
type Spec struct {
	// Title names the object in the output schema.
	Title  string
	Fields []Field
}

// Options carries the caller's explicit opt-ins.
type Options struct {
	IncludePII   bool
	IncludeNotes bool
}

// Validate rejects a spec that declares a credential field.
//
// Called at startup for every registered tool, so declaring a password field
// is a boot failure rather than a leak discovered in production.
func (s Spec) Validate() error {
	if len(s.Fields) == 0 {
		return fmt.Errorf("output spec %q declares no fields", s.Title)
	}
	seen := make(map[string]bool, len(s.Fields))
	for _, f := range s.Fields {
		if f.Name == "" {
			return fmt.Errorf("output spec %q has a field with no name", s.Title)
		}
		if seen[f.Name] {
			return fmt.Errorf("output spec %q declares %q twice", s.Title, f.Name)
		}
		seen[f.Name] = true

		if redact.IsSecret(f.Name) || redact.IsSecret(f.source()) {
			return fmt.Errorf(
				"output spec %q declares field %q, which names a credential; credentials are never returned",
				s.Title, f.Name)
		}
		if f.Kind == Untrusted && f.Origin == "" {
			return fmt.Errorf("output spec %q declares untrusted field %q with no origin", s.Title, f.Name)
		}
		// An untrusted field is wrapped as text. Declaring a structured type
		// means the value goes through fmt %v, which emits Go's map[...] debug
		// syntax into the model's context: unreadable, and it buries whatever
		// the customer actually wrote. Project the structure and wrap its
		// string leaves instead.
		if f.Kind == Untrusted && f.Type != "" && f.Type != "string" {
			return fmt.Errorf(
				"output spec %q declares untrusted field %q as %s; only string fields can be wrapped, "+
					"project the structure and wrap its text leaves", s.Title, f.Name, f.Type)
		}
	}
	return nil
}

// Project maps one WHMCS object onto the declared fields.
func (s Spec) Project(src map[string]any, opts Options) map[string]any {
	out := make(map[string]any, len(s.Fields))
	for _, f := range s.Fields {
		raw, ok := lookup(src, f.source())
		if !ok || raw == nil {
			continue
		}

		switch f.Kind {
		case PII:
			if !opts.IncludePII {
				continue
			}
		case Notes:
			if !opts.IncludeNotes {
				continue
			}
		case Untrusted:
			out[f.Name] = untrusted.Wrap(f.Origin, toString(raw))
			continue
		}

		// Second line of defence. The allowlist should already have excluded
		// anything sensitive; this catches a field whose upstream value turns
		// out to be a nested object carrying one.
		out[f.Name] = redact.Value(raw, redact.Options{
			IncludePII:   opts.IncludePII,
			IncludeNotes: opts.IncludeNotes,
		})
	}
	return out
}

// ProjectList maps a list of WHMCS objects.
func (s Spec) ProjectList(src []any, opts Options) []map[string]any {
	out := make([]map[string]any, 0, len(src))
	for _, item := range src {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, s.Project(m, opts))
	}
	return out
}

// JSONSchema renders the object schema for a tool's outputSchema declaration.
func (s Spec) JSONSchema() map[string]any {
	props := make(map[string]any, len(s.Fields))
	for _, f := range s.Fields {
		if f.Kind == Untrusted {
			props[f.Name] = map[string]any{
				"type":        "object",
				"description": f.Desc + " Customer-authored content, returned in an untrusted-data envelope.",
				"properties": map[string]any{
					"_untrusted": map[string]any{"type": "boolean"},
					"_notice":    map[string]any{"type": "string"},
					"origin":     map[string]any{"type": "string"},
					"content":    map[string]any{"type": "string"},
					"truncated":  map[string]any{"type": "boolean"},
				},
			}
			continue
		}
		desc := f.Desc
		switch f.Kind {
		case PII:
			desc += " Personal data: returned only when include_personal_details is set."
		case Notes:
			desc += " Internal note: returned only when include_notes is set."
		}
		props[f.Name] = map[string]any{
			"type":        f.jsonType(),
			"description": strings.TrimSpace(desc),
		}
	}
	return map[string]any{
		"type":       "object",
		"title":      s.Title,
		"properties": props,
	}
}

// lookup resolves a source key case-insensitively. WHMCS is inconsistent about
// casing between actions, and a case mismatch would silently drop a field,
// which looks identical to "the record has no value here".
func lookup(src map[string]any, key string) (any, bool) {
	if v, ok := src[key]; ok {
		return v, true
	}
	for k, v := range src {
		if strings.EqualFold(k, key) {
			return v, true
		}
	}
	return nil, false
}

func toString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", t)
	}
}
