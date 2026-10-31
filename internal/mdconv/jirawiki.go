// Package mdconv converts GitHub-flavored Markdown to Atlassian markup.
package mdconv

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

var mdParser = goldmark.New(goldmark.WithExtensions(extension.GFM)).Parser()

// ToJiraWiki converts Markdown to Jira wiki markup, the text format of the
// Jira REST API v2 (descriptions, comments).
//
// Supported: headings, paragraphs, emphasis, strikethrough, inline code,
// fenced and indented code blocks, links, autolinks, images, bullet, ordered
// and task lists (nested), block quotes, tables and horizontal rules. Raw
// HTML is passed through unchanged.
func ToJiraWiki(markdown string) string {
	src := []byte(markdown)
	w := &wikiWriter{src: src}
	w.blocks(mdParser.Parse(text.NewReader(src)), "\n")
	return strings.TrimRight(w.sb.String(), "\n") + "\n"
}

type wikiWriter struct {
	src []byte
	sb  strings.Builder
}

// blocks renders the block children of n, separated by sep.
func (w *wikiWriter) blocks(n ast.Node, sep string) {
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		if c != n.FirstChild() {
			w.sb.WriteString(sep)
		}
		w.block(c)
	}
}

func (w *wikiWriter) block(n ast.Node) {
	switch n := n.(type) {
	case *ast.Heading:
		w.sb.WriteString("h" + string(rune('0'+n.Level)) + ". " + w.inline(n) + "\n")
	case *ast.Paragraph, *ast.TextBlock:
		w.sb.WriteString(escapeLineStart(w.inline(n)) + "\n")
	case *ast.ThematicBreak:
		w.sb.WriteString("----\n")
	case *ast.FencedCodeBlock:
		open := "{code}"
		if lang := n.Language(w.src); len(lang) > 0 {
			open = "{code:" + string(lang) + "}"
		}
		w.sb.WriteString(open + "\n" + w.lines(n) + "{code}\n")
	case *ast.CodeBlock:
		w.sb.WriteString("{code}\n" + w.lines(n) + "{code}\n")
	case *ast.HTMLBlock:
		w.sb.WriteString(w.lines(n))
		if n.HasClosure() {
			w.sb.WriteString(string(n.ClosureLine.Value(w.src)))
		}
	case *ast.Blockquote:
		w.sb.WriteString("{quote}\n")
		w.blocks(n, "\n")
		w.sb.WriteString("{quote}\n")
	case *ast.List:
		w.list(n, "")
	case *extast.Table:
		w.table(n)
	default:
		w.blocks(n, "\n")
	}
}

// list renders a list; prefix holds the markers of the enclosing lists, since
// Jira nests lists by repeating markers ("*", "**", "#*", ...).
func (w *wikiWriter) list(l *ast.List, prefix string) {
	marker := "*"
	if l.IsOrdered() {
		marker = "#"
	}
	prefix += marker
	for item := l.FirstChild(); item != nil; item = item.NextSibling() {
		var text []string
		var nested []*ast.List
		for c := item.FirstChild(); c != nil; c = c.NextSibling() {
			if sub, ok := c.(*ast.List); ok {
				nested = append(nested, sub)
				continue
			}
			// Jira list items hold a single line; join extra blocks with
			// forced line breaks.
			text = append(text, strings.TrimRight(w.render(c), "\n"))
		}
		w.sb.WriteString(prefix + " " + strings.Join(text, " \\\\ ") + "\n")
		for _, sub := range nested {
			w.list(sub, prefix)
		}
	}
}

func (w *wikiWriter) table(t *extast.Table) {
	for row := t.FirstChild(); row != nil; row = row.NextSibling() {
		sep := "|"
		if _, ok := row.(*extast.TableHeader); ok {
			sep = "||"
		}
		var cells []string
		for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
			c := strings.ReplaceAll(w.inline(cell), "|", `\|`)
			if c == "" {
				c = " " // Jira collapses empty cells
			}
			cells = append(cells, c)
		}
		w.sb.WriteString(sep + strings.Join(cells, sep) + sep + "\n")
	}
}

// render returns the markup of a single block without writing it.
func (w *wikiWriter) render(n ast.Node) string {
	sub := &wikiWriter{src: w.src}
	sub.block(n)
	return sub.sb.String()
}

func (w *wikiWriter) lines(n ast.Node) string {
	var sb strings.Builder
	lines := n.Lines()
	for i := 0; i < lines.Len(); i++ {
		seg := lines.At(i)
		sb.Write(seg.Value(w.src))
	}
	return sb.String()
}

// inline renders the inline children of n.
func (w *wikiWriter) inline(n ast.Node) string {
	var sb strings.Builder
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		switch c := c.(type) {
		case *ast.Text:
			// Segments keep markdown backslash escapes; drop them before
			// applying Jira's escaping.
			sb.WriteString(escapeText(string(util.UnescapePunctuations(c.Segment.Value(w.src)))))
			switch {
			case c.HardLineBreak():
				sb.WriteString("\n")
			case c.SoftLineBreak():
				sb.WriteString(" ")
			}
		case *ast.String:
			sb.WriteString(escapeText(string(c.Value)))
		case *ast.CodeSpan:
			sb.WriteString("{{" + w.rawText(c) + "}}")
		case *ast.Emphasis:
			mark := "_"
			if c.Level >= 2 {
				mark = "*"
			}
			sb.WriteString(mark + w.inline(c) + mark)
		case *extast.Strikethrough:
			sb.WriteString("-" + w.inline(c) + "-")
		case *ast.Link:
			label, dest := w.inline(c), string(c.Destination)
			if label == "" || label == escapeText(dest) {
				sb.WriteString("[" + dest + "]")
			} else {
				sb.WriteString("[" + label + "|" + dest + "]")
			}
		case *ast.AutoLink:
			dest := string(c.URL(w.src))
			if c.AutoLinkType == ast.AutoLinkEmail && !strings.HasPrefix(dest, "mailto:") {
				dest = "mailto:" + dest
			}
			sb.WriteString("[" + dest + "]")
		case *ast.Image:
			sb.WriteString("!" + string(c.Destination) + "!")
		case *ast.RawHTML:
			for i := 0; i < c.Segments.Len(); i++ {
				seg := c.Segments.At(i)
				sb.Write(seg.Value(w.src))
			}
		case *extast.TaskCheckBox:
			if c.IsChecked {
				sb.WriteString("[x] ")
			} else {
				sb.WriteString("[ ] ")
			}
		default:
			sb.WriteString(w.inline(c))
		}
	}
	return sb.String()
}

// rawText returns the unescaped text content of an inline node.
func (w *wikiWriter) rawText(n ast.Node) string {
	var sb strings.Builder
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		if t, ok := c.(*ast.Text); ok {
			sb.Write(t.Segment.Value(w.src))
		} else {
			sb.WriteString(w.rawText(c))
		}
	}
	return sb.String()
}

// effectChars start Jira text effects (*bold*, _italic_, -strike-, +under+,
// ^sup^, ~sub~, !image!) when they open a word.
const effectChars = "*_-+^~!"

// escapeText escapes characters that Jira would read as markup. Brackets and
// braces always start links and macros; effect characters only matter at
// the start of a word, so "snake_case" and "well-known" are left alone.
func escapeText(s string) string {
	var sb strings.Builder
	runes := []rune(s)
	for i, r := range runes {
		switch {
		case r == '{' || r == '}' || r == '[' || r == ']':
			sb.WriteRune('\\')
		case strings.ContainsRune(effectChars, r):
			atWordStart := i == 0 || unicode.IsSpace(runes[i-1]) || unicode.IsPunct(runes[i-1])
			nextIsText := i+1 < len(runes) && !unicode.IsSpace(runes[i+1])
			if atWordStart && nextIsText {
				sb.WriteRune('\\')
			}
		}
		sb.WriteRune(r)
	}
	return sb.String()
}

var lineStartMarkup = regexp.MustCompile(`^(?:[*#-]+ |h[1-6]\. |bq\. )`)

// escapeLineStart escapes paragraph text that Jira would otherwise read as a
// list item, heading or quote.
func escapeLineStart(s string) string {
	if lineStartMarkup.MatchString(s) {
		return `\` + s
	}
	return s
}
