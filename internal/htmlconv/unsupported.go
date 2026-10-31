package htmlconv

import (
	"fmt"
	"sort"
	"strings"

	"golang.org/x/net/html"
)

// Tags that Convert renders faithfully, or whose loss is purely cosmetic
// (table structure, wrappers).
var supportedTags = map[string]bool{
	"html": true, "head": true, "body": true,
	"p": true, "br": true, "hr": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"strong": true, "b": true, "em": true, "i": true, "s": true, "del": true, "strike": true,
	"code": true, "pre": true, "a": true, "img": true, "blockquote": true,
	"ul": true, "ol": true, "li": true,
	"table": true, "thead": true, "tbody": true, "tr": true, "th": true, "td": true,
	"colgroup": true, "col": true, "div": true, "span": true,
	"ac:plain-text-body": true, "ac:rich-text-body": true,
	"ac:image": true, "ri:url": true, "ri:attachment": true,
}

// macroParams lists the macros Convert renders and the parameters it keeps.
var macroParams = map[string]map[string]bool{
	"code":    {"language": true},
	"info":    {},
	"note":    {},
	"warning": {},
	"tip":     {},
}

// Unsupported lists the storage-format features in storageXHTML that a
// Markdown round trip (Convert, edit, mdconv.ToStorage) would lose, such as
// unknown macros, page links, mentions, inline styles or merged table cells.
// Each entry is a feature name with its number of occurrences, e.g.
// "ac:link (3)". The result is empty when the page can be safely edited as
// Markdown.
func Unsupported(storageXHTML string) []string {
	doc, err := html.Parse(strings.NewReader(storageXHTML))
	if err != nil {
		return []string{"unparseable content"}
	}
	counts := map[string]int{}
	findUnsupported(doc, counts)

	out := make([]string, 0, len(counts))
	for name, n := range counts {
		out = append(out, fmt.Sprintf("%s (%d)", name, n))
	}
	sort.Strings(out)
	return out
}

func findUnsupported(n *html.Node, counts map[string]int) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode {
			continue
		}
		if feature := unsupportedFeature(c); feature != "" {
			counts[feature]++
			continue // a lost element takes its content with it
		}
		findUnsupported(c, counts)
	}
}

// unsupportedFeature names what would be lost by converting element n, or
// returns "" when n survives.
func unsupportedFeature(n *html.Node) string {
	if n.Data == "ac:structured-macro" {
		name := getAttr(n, "ac:name")
		params, ok := macroParams[name]
		if !ok {
			return "macro " + name
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && c.Data == "ac:parameter" && !params[getAttr(c, "ac:name")] {
				return "macro " + name + " parameter " + getAttr(c, "ac:name")
			}
		}
		return ""
	}
	if n.Data == "ac:parameter" {
		return "" // checked with its macro
	}
	if !supportedTags[n.Data] {
		return n.Data
	}
	if getAttr(n, "style") != "" {
		return "style attribute"
	}
	for _, attr := range []string{"colspan", "rowspan"} {
		if v := getAttr(n, attr); v != "" && v != "1" {
			return "merged table cells"
		}
	}
	return ""
}
