// Package registry describes every WHMCS Admin API action: its parameters,
// their types and requiredness, and how dangerous the action is.
//
// The parameter data in actions_gen.go is generated from the vendor's published
// API reference by cmd/docgen and must not be hand-edited. The safety
// classification in classification.go is deliberately hand-maintained, because
// the vendor documentation does not say whether an action reads or destroys.
package registry

// ParamType is the declared type of a WHMCS request parameter, normalised from
// the vendor documentation's type column.
type ParamType string

// The closed set of parameter types. The vendor's free-text type column is
// normalised onto these; an unrecognised type becomes TypeString, which is how
// WHMCS transports everything anyway.
const (
	TypeString ParamType = "string"
	TypeInt    ParamType = "int"
	TypeFloat  ParamType = "float"
	TypeBool   ParamType = "bool"
	TypeArray  ParamType = "array"
	TypeObject ParamType = "object"
)

// Param is one request or response parameter.
type Param struct {
	Name        string
	Type        ParamType
	Description string
	Required    bool
	// Deprecated marks parameters the vendor documents as deprecated. This
	// server refuses to send them: they are overwhelmingly raw card data.
	Deprecated bool
}

// Action is one WHMCS API action.
type Action struct {
	// Name is the exact action string sent to WHMCS, e.g. "AddClient".
	Name string
	// Category is the vendor's documentation grouping, e.g. "Client".
	Category string
	// Slug is the path segment on developers.whmcs.com.
	Slug string
	// Summary is a one-line description, used by whmcs_list_actions where
	// returning full schemas would be too expensive.
	Summary string
	// Params are the request parameters, excluding the "action" parameter
	// itself, which the client supplies.
	Params []Param
	// Response are the documented response fields.
	Response []Param
}

// DocURL returns the vendor documentation page for the action.
func (a Action) DocURL() string {
	return "https://developers.whmcs.com/api-reference/" + a.Slug + "/"
}

// Param returns the named request parameter, matched case-insensitively as
// WHMCS itself does.
func (a Action) Param(name string) (Param, bool) {
	for _, p := range a.Params {
		if equalFold(p.Name, name) {
			return p, true
		}
	}
	return Param{}, false
}

// ParamNames returns the accepted, non-deprecated request parameter names. It
// is included in validation errors so a misspelling is self-correcting.
func (a Action) ParamNames() []string {
	names := make([]string, 0, len(a.Params))
	for _, p := range a.Params {
		if !p.Deprecated {
			names = append(names, p.Name)
		}
	}
	return names
}

// Class is how dangerous an action is. It drives MCP tool annotations, which
// profile permits the action, and whether the confirmation protocol applies.
type Class string

const (
	// ClassRead performs no modification. Safe to retry, safe to call
	// speculatively, annotated readOnlyHint.
	ClassRead Class = "read"

	// ClassWrite modifies state in a way that is reversible by an operator and
	// affects one record. Never retried automatically.
	ClassWrite Class = "write"

	// ClassDestructive is irreversible, moves money, changes provisioning, or
	// sends mail to a customer. Off by default in every profile including
	// admin, and always subject to the confirmation protocol.
	ClassDestructive Class = "destructive"

	// ClassBlocked is never reachable through this server in any profile or
	// configuration. Enabling one requires a code change and a review.
	ClassBlocked Class = "blocked"
)

// Mutating reports whether the class changes state. Used to derive the
// readOnlyHint annotation and to decide whether a retry is permissible.
func (c Class) Mutating() bool { return c != ClassRead }

// NeedsConfirmation reports whether the prepare/confirm protocol applies.
func (c Class) NeedsConfirmation() bool { return c == ClassDestructive }
