package main

import (
	"fmt"
	"io"
	"strings"

	"golang.org/x/net/html"

	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/registry"
)

// indexEntry is one action discovered on the API index page.
type indexEntry struct {
	Name     string
	Slug     string
	Category string
}

// parseIndex extracts every action link from the API index page, attributing
// each to the h3 category heading that precedes it.
//
// The page is a flat sequence of h3 headings followed by ul blocks, so the
// category is simply "the most recent h3 seen". Anything after the trailing
// site-navigation headings has no action links, so it contributes nothing.
func parseIndex(r io.Reader) ([]indexEntry, error) {
	doc, err := html.Parse(r)
	if err != nil {
		return nil, fmt.Errorf("parse index html: %w", err)
	}

	var entries []indexEntry
	category := ""

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "h3":
				category = strings.TrimSpace(textOf(n))
			case "a":
				href := attr(n, "href")
				if slug, ok := actionSlug(href); ok {
					name := strings.TrimSpace(textOf(n))
					if name != "" && category != "" {
						entries = append(entries, indexEntry{
							Name:     name,
							Slug:     slug,
							Category: category,
						})
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	if len(entries) == 0 {
		return nil, fmt.Errorf("no action links found; the index page layout has probably changed")
	}
	return entries, nil
}

// actionSlug extracts the slug from an api-reference href, tolerating the
// protocol-relative and absolute forms the site mixes.
func actionSlug(href string) (string, bool) {
	const marker = "/api-reference/"
	i := strings.Index(href, marker)
	if i < 0 {
		return "", false
	}
	slug := strings.Trim(href[i+len(marker):], "/")
	if slug == "" || strings.Contains(slug, "/") {
		return "", false
	}
	return slug, true
}

// actionPage is the parsed content of a single action reference page.
type actionPage struct {
	Summary  string
	Params   []registry.Param
	Response []registry.Param
}

// parseAction extracts the summary paragraph and the request and response
// parameter tables from an action reference page.
//
// Tables are matched by the id of the h3 that introduces them rather than by
// document order, so an extra table elsewhere on the page cannot be mistaken
// for the parameter list.
func parseAction(action string, r io.Reader) (actionPage, error) {
	doc, err := html.Parse(r)
	if err != nil {
		return actionPage{}, fmt.Errorf("parse action html: %w", err)
	}

	var page actionPage
	section := ""
	sawH1 := false

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "h1":
				sawH1 = true
			case "h3":
				section = attr(n, "id")
				if section == "" {
					section = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(textOf(n)), " ", "-"))
				}
			case "p":
				// The first paragraph after the title is the action summary.
				if sawH1 && section == "" && page.Summary == "" {
					page.Summary = collapse(textOf(n))
				}
			case "table":
				switch section {
				case "request-parameters":
					page.Params = parseParamTable(action, n, true)
				case "response-parameters":
					page.Response = parseParamTable(action, n, false)
				}
				// Do not descend into a table; nested tables are not a thing
				// on these pages and skipping avoids re-entering rows.
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	if len(page.Params) == 0 && len(page.Response) == 0 {
		return actionPage{}, fmt.Errorf("no parameter tables found; the action page layout has probably changed")
	}
	return page, nil
}

// parseParamTable converts a parameter table into Params. withRequired selects
// the four-column request form; response tables have no requiredness column.
//
// The synthetic "action" row is dropped: the client supplies it, and exposing
// it would let a caller override the action after policy resolution.
func parseParamTable(action string, table *html.Node, withRequired bool) []registry.Param {
	var out []registry.Param

	for _, row := range findAll(table, "tr") {
		cells := findAll(row, "td")
		if len(cells) < 2 {
			continue // header row
		}
		name := collapse(textOf(cells[0]))
		if name == "" || strings.EqualFold(name, "action") {
			continue
		}
		p := registry.Param{
			Name: name,
			Type: normaliseType(collapse(textOf(cells[1]))),
		}
		if len(cells) > 2 {
			p.Description = describe(action, name, collapse(textOf(cells[2])))
		}
		if withRequired && len(cells) > 3 {
			req := strings.ToLower(collapse(textOf(cells[3])))
			p.Deprecated = strings.Contains(req, "deprecated")
			p.Required = strings.HasPrefix(req, "required") && !p.Deprecated
		}
		out = append(out, p)
	}
	return out
}

// describe decides what description, if any, is emitted for a parameter.
//
// Short vendor text is a statement of fact and passes through. Anything longer
// than registry.LongDescriptionThreshold is treated as authored prose that this
// project has no licence to redistribute, so it is replaced with our own text
// where we have written one, and dropped where we have not. See
// internal/registry/descriptions.go for the reasoning.
func describe(action, param, vendor string) string {
	if len(vendor) <= registry.LongDescriptionThreshold {
		return vendor
	}
	if ours, ok := registry.Description(action, param); ok {
		return ours
	}
	return ""
}

// normaliseType maps the vendor's free-text type column onto the registry's
// closed set. Unrecognised types become string, which is how WHMCS transports
// everything anyway, so an unfamiliar type degrades to "send it verbatim"
// rather than to a validation failure.
func normaliseType(s string) registry.ParamType {
	t := strings.ToLower(s)
	switch {
	case strings.Contains(t, "bool"):
		return registry.TypeBool
	case strings.Contains(t, "float"), strings.Contains(t, "decimal"), strings.Contains(t, "double"):
		return registry.TypeFloat
	case strings.Contains(t, "array"):
		return registry.TypeArray
	case strings.Contains(t, "object"):
		return registry.TypeObject
	case strings.Contains(t, "int"):
		return registry.TypeInt
	default:
		return registry.TypeString
	}
}

// --- small html helpers ----------------------------------------------------

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

// textOf returns the concatenated text of a node's subtree. Inline markup such
// as <code> is flattened, which is what the parameter descriptions need.
func textOf(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

func findAll(n *html.Node, tag string) []*html.Node {
	var out []*html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == tag {
			out = append(out, n)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return out
}

// collapse normalises whitespace and the typographic quotes the vendor site
// emits, so the generated file is stable across regenerations.
func collapse(s string) string {
	s = strings.NewReplacer(
		"“", `"`, "”", `"`,
		"‘", "'", "’", "'",
		"–", "-", "—", "-",
		" ", " ",
	).Replace(s)
	return strings.Join(strings.Fields(s), " ")
}
