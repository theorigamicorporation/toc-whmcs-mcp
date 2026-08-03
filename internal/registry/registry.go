package registry

import (
	"fmt"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/errs"
)

// MaxStringLen bounds any single string parameter. It is generous enough for a
// ticket body and small enough that a runaway generation cannot post megabytes
// into the billing system.
const MaxStringLen = 32768

var (
	byName     map[string]Action
	categories []string
)

func init() {
	byName = make(map[string]Action, len(generatedActions))
	seen := make(map[string]bool)
	for _, a := range generatedActions {
		byName[canonicalName(a.Name)] = a
		if !seen[a.Category] {
			seen[a.Category] = true
			categories = append(categories, a.Category)
		}
	}
	sort.Strings(categories)
}

func canonicalName(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

func equalFold(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

// Lookup returns an action by name, matched case-insensitively.
func Lookup(name string) (Action, bool) {
	a, ok := byName[canonicalName(name)]
	return a, ok
}

// All returns every action, ordered by category then name.
func All() []Action {
	out := make([]Action, 0, len(generatedActions))
	out = append(out, generatedActions...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Category != out[j].Category {
			return out[i].Category < out[j].Category
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// Categories returns the sorted list of documentation categories.
func Categories() []string {
	out := make([]string, len(categories))
	copy(out, categories)
	return out
}

// ByCategory returns the actions in a category, matched case-insensitively.
func ByCategory(category string) []Action {
	var out []Action
	for _, a := range All() {
		if equalFold(a.Category, category) {
			out = append(out, a)
		}
	}
	return out
}

// Count returns the number of known actions.
func Count() int { return len(generatedActions) }

// Resolve looks up an action and rejects unknown and hard-blocked ones before
// any argument is examined, so a blocked action never reaches validation, the
// policy layer, or the network.
func Resolve(name string) (Action, error) {
	a, ok := Lookup(name)
	if !ok {
		return Action{}, errs.New(errs.CodeUnknownAction,
			"unknown WHMCS action %q", name).
			WithDetails(map[string]any{
				"hint": "call whmcs_list_actions to see the available actions",
			})
	}
	if Classify(a.Name) == ClassBlocked {
		return Action{}, errs.New(errs.CodeForbidden,
			"action %s is permanently blocked by this server and cannot be enabled by configuration", a.Name).
			WithDetails(map[string]any{
				"reason": "the action returns or issues credentials",
			})
	}
	return a, nil
}

// Validate checks arguments against the action's documented parameters and
// returns the form values to POST.
//
// Validation is deliberately strict and happens before any network call: a
// misspelled parameter is an error rather than a silently ignored field, which
// is the difference between "your filter did nothing" and "you updated every
// client".
func Validate(a Action, args map[string]any) (url.Values, error) {
	values := url.Values{}

	// Reject unknown and deprecated parameters first, so the caller learns
	// about a typo even if a required field is also missing.
	for key, raw := range args {
		p, ok := a.Param(key)
		if !ok {
			return nil, errs.New(errs.CodeInvalidParams,
				"unknown parameter %q for action %s", key, a.Name).
				WithDetails(map[string]any{
					"action":              a.Name,
					"accepted_parameters": a.ParamNames(),
				})
		}
		if p.Deprecated {
			return nil, errs.New(errs.CodeInvalidParams,
				"parameter %q of action %s is deprecated and is not accepted by this server", p.Name, a.Name).
				WithDetails(map[string]any{
					"reason": "deprecated WHMCS parameters are predominantly raw cardholder data",
				})
		}
		if raw == nil {
			continue
		}
		encoded, err := encode(a, p, raw)
		if err != nil {
			return nil, err
		}
		values.Set(p.Name, encoded)
	}

	for _, p := range a.Params {
		if !p.Required || p.Deprecated {
			continue
		}
		if values.Get(p.Name) == "" {
			return nil, errs.New(errs.CodeInvalidParams,
				"missing required parameter %q for action %s", p.Name, a.Name).
				WithDetails(map[string]any{
					"action":              a.Name,
					"parameter":           p.Name,
					"description":         p.Description,
					"accepted_parameters": a.ParamNames(),
				})
		}
	}

	return values, nil
}

// encode converts one argument to its form representation, enforcing the
// declared type and the bounds that keep a plausible-looking call from becoming
// an operationally invalid one.
func encode(a Action, p Param, raw any) (string, error) {
	invalid := func(format string, v ...any) error {
		return errs.New(errs.CodeInvalidParams,
			"parameter %q of action %s "+format, append([]any{p.Name, a.Name}, v...)...)
	}

	switch p.Type {
	case TypeInt:
		n, ok := toInt(raw)
		if !ok {
			return "", invalid("must be a whole number, got %v", raw)
		}
		// Identifier parameters address a specific record. Zero and negative
		// values are not "no filter" in WHMCS; they are undefined behaviour.
		if isIdentifier(p.Name) && n < 1 {
			return "", invalid("is an identifier and must be 1 or greater, got %d", n)
		}
		if n < 0 {
			return "", invalid("must not be negative, got %d", n)
		}
		return strconv.FormatInt(n, 10), nil

	case TypeFloat:
		f, ok := toFloat(raw)
		if !ok {
			return "", invalid("must be a number, got %v", raw)
		}
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return "", invalid("must be a finite number")
		}
		return strconv.FormatFloat(f, 'f', -1, 64), nil

	case TypeBool:
		b, ok := toBool(raw)
		if !ok {
			return "", invalid("must be a boolean, got %v", raw)
		}
		if b {
			return "1", nil
		}
		return "0", nil

	default:
		s, ok := raw.(string)
		if !ok {
			// Numbers and booleans are accepted where a string is declared,
			// because WHMCS itself is loosely typed and models routinely send
			// 42 where "42" is documented. Anything structured is not.
			switch v := raw.(type) {
			case float64:
				s = strconv.FormatFloat(v, 'f', -1, 64)
			case int:
				s = strconv.Itoa(v)
			case bool:
				s = strconv.FormatBool(v)
			default:
				return "", invalid("must be a string, got %T", raw)
			}
		}
		if len(s) > MaxStringLen {
			return "", invalid("exceeds the maximum length of %d characters (got %d)", MaxStringLen, len(s))
		}
		return s, nil
	}
}

// isIdentifier reports whether a parameter names a specific record. WHMCS is
// consistent enough here that a suffix match is reliable, and the cost of a
// false positive is only that a caller cannot pass zero.
func isIdentifier(name string) bool {
	n := strings.ToLower(name)
	switch n {
	case "id", "clientid", "userid", "invoiceid", "ticketid", "orderid", "serviceid",
		"domainid", "contactid", "quoteid", "projectid", "taskid", "noteid", "replyid",
		"transactionid", "addonid", "productid", "pid", "accountid", "paymethodid":
		return true
	}
	return strings.HasSuffix(n, "_id")
}

func toInt(raw any) (int64, bool) {
	switch v := raw.(type) {
	case int:
		return int64(v), true
	case int32:
		return int64(v), true
	case int64:
		return v, true
	case float64:
		// JSON numbers decode to float64. A fractional value is not an integer
		// no matter how it was spelled.
		if v != math.Trunc(v) {
			return 0, false
		}
		return int64(v), true
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err != nil {
			return 0, false
		}
		return n, true
	default:
		return 0, false
	}
}

func toFloat(raw any) (float64, bool) {
	switch v := raw.(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil {
			return 0, false
		}
		return f, true
	default:
		return 0, false
	}
}

func toBool(raw any) (bool, bool) {
	switch v := raw.(type) {
	case bool:
		return v, true
	case string:
		b, err := strconv.ParseBool(strings.TrimSpace(v))
		if err != nil {
			return false, false
		}
		return b, true
	case float64:
		return v != 0, true
	default:
		return false, false
	}
}

// Describe renders an action's schema for whmcs_describe_action.
func Describe(a Action) map[string]any {
	params := make([]map[string]any, 0, len(a.Params))
	for _, p := range a.Params {
		if p.Deprecated {
			continue
		}
		params = append(params, map[string]any{
			"name":        p.Name,
			"type":        string(p.Type),
			"required":    p.Required,
			"description": p.Description,
		})
	}
	return map[string]any{
		"action":             a.Name,
		"category":           a.Category,
		"summary":            a.Summary,
		"classification":     string(Classify(a.Name)),
		"mutating":           Classify(a.Name).Mutating(),
		"needs_confirmation": Classify(a.Name).NeedsConfirmation(),
		"documentation":      a.DocURL(),
		"parameters":         params,
	}
}

// Summarise renders the cheap one-line form used by whmcs_list_actions. It
// deliberately omits parameter schemas: listing every action with full schemas
// would cost as much context as registering every action as a tool.
func Summarise(a Action) map[string]any {
	return map[string]any{
		"action":         a.Name,
		"category":       a.Category,
		"summary":        a.Summary,
		"classification": string(Classify(a.Name)),
	}
}

// String renders an action for log and error messages.
func (a Action) String() string {
	return fmt.Sprintf("%s (%s, %s)", a.Name, a.Category, Classify(a.Name))
}
