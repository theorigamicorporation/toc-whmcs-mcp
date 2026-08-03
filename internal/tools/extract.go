package tools

import (
	"encoding/json"

	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/shape"
)

// ExtractIn is what an extractor receives. It is a struct rather than a
// parameter list so that adding context later does not touch every tool.
type ExtractIn struct {
	Data   map[string]any
	Out    shape.Spec
	Opts   shape.Options
	Limits Limits
	Args   Args
}

// outputSchema renders the tool's declared output schema.
//
// Collection tools wrap their records in a paging envelope, so the schema has
// to describe the envelope rather than the record shape alone.
func (t Tool) outputSchema() json.RawMessage {
	record := t.Out.JSONSchema()

	var schema map[string]any
	if t.Paginated {
		schema = map[string]any{
			"type":  "object",
			"title": t.Out.Title + "Page",
			"properties": map[string]any{
				"records": map[string]any{
					"type":        "array",
					"description": "The records on this page.",
					"items":       record,
				},
				"count":         map[string]any{"type": "integer", "description": "Records returned on this page."},
				"total":         map[string]any{"type": "integer", "description": "Total records matching the query, as reported by WHMCS."},
				"offset":        map[string]any{"type": "integer", "description": "Records skipped before this page."},
				"limit":         map[string]any{"type": "integer", "description": "Page size actually applied."},
				"has_more":      map[string]any{"type": "boolean", "description": "Whether records remain beyond this page."},
				"next_offset":   map[string]any{"type": "integer", "description": "Offset to pass to retrieve the next page."},
				"limit_clamped": map[string]any{"type": "boolean", "description": "Whether the requested limit was reduced to the server maximum."},
			},
		}
	} else {
		schema = record
	}

	encoded, err := json.Marshal(schema)
	if err != nil {
		// A schema that cannot be encoded is a defect in a literal, not a
		// runtime condition. Returning an empty object keeps the server
		// serving rather than failing a tool over it.
		return json.RawMessage(`{"type":"object"}`)
	}
	return encoded
}

// listExtract builds an extractor for a WHMCS collection response.
//
// WHMCS nests collections one level deeper than you would expect:
// {"clients": {"client": [...]}}. The container and item keys differ per
// action, so they are supplied per tool.
func listExtract(container, item string) func(ExtractIn) any {
	return func(in ExtractIn) any {
		limit, offset, clamped := in.Args.Page(in.Limits)

		records := in.Out.ProjectList(collection(in.Data, container, item), in.Opts)

		// WHMCS honours limitnum for most collections, but not all. Enforce the
		// page size locally too, so a tool whose action ignores the parameter
		// cannot return an unbounded list.
		if len(records) > limit {
			records = records[:limit]
		}

		total := intField(in.Data, "totalresults")
		if total == 0 {
			total = offset + len(records)
		}
		hasMore := offset+len(records) < total

		out := map[string]any{
			"records":       records,
			"count":         len(records),
			"total":         total,
			"offset":        offset,
			"limit":         limit,
			"has_more":      hasMore,
			"limit_clamped": clamped,
		}
		if hasMore {
			out["next_offset"] = offset + len(records)
		}
		return out
	}
}

// objectExtract builds an extractor for a single-object response.
//
// path names the nested object to project, if any. WHMCS is inconsistent:
// GetInvoice returns its fields at the top level, GetClientsDetails nests them
// under "client". An empty path means the top level.
func objectExtract(path string) func(ExtractIn) any {
	return func(in ExtractIn) any {
		src := in.Data
		if path != "" {
			if nested, ok := in.Data[path].(map[string]any); ok {
				src = nested
			}
		}
		return in.Out.Project(src, in.Opts)
	}
}

// mergedExtract projects the top level and then overlays a nested object, for
// responses that split one logical record across both.
func mergedExtract(path string) func(ExtractIn) any {
	return func(in ExtractIn) any {
		out := in.Out.Project(in.Data, in.Opts)
		if nested, ok := in.Data[path].(map[string]any); ok {
			for k, v := range in.Out.Project(nested, in.Opts) {
				out[k] = v
			}
		}
		return out
	}
}

// ackExtract is the extractor for a write whose response is only a result
// marker. It reports what happened rather than echoing an empty response.
func ackExtract(what string) func(ExtractIn) any {
	return func(in ExtractIn) any {
		out := map[string]any{
			"status":  "done",
			"summary": what,
		}
		// Carry through any identifier WHMCS minted, which is the one piece of
		// a write response that is genuinely useful.
		for _, key := range []string{"noteid", "ticketid", "invoiceid", "clientid", "orderid", "id"} {
			if v, ok := in.Data[key]; ok {
				out[key] = v
			}
		}
		return out
	}
}

// collection pulls a nested WHMCS list out of a response, tolerating the two
// shapes the vendor uses: a nested object with an item key, and a bare array.
func collection(data map[string]any, container, item string) []any {
	raw, ok := data[container]
	if !ok {
		return nil
	}
	switch t := raw.(type) {
	case []any:
		return t
	case map[string]any:
		if inner, ok := t[item].([]any); ok {
			return inner
		}
		// A single-element collection is sometimes returned as one object.
		if inner, ok := t[item].(map[string]any); ok {
			return []any{inner}
		}
	}
	return nil
}

func intField(data map[string]any, key string) int {
	switch v := data[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	default:
		return 0
	}
}

// page maps resolved paging onto the WHMCS parameter names.
func pageParams(args Args, lim Limits, params map[string]any) {
	limit, offset, _ := args.Page(lim)
	params["limitnum"] = limit
	if offset > 0 {
		params["limitstart"] = offset
	}
}
