package mdconv

import (
	"bytes"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

var storageMarkdown = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithParserOptions(parser.WithASTTransformers(util.Prioritized(admonitionTransformer{}, 100))),
	goldmark.WithRendererOptions(
		html.WithXHTML(),
		html.WithUnsafe(), // raw HTML, including storage-format macros, passes through
		renderer.WithNodeRenderers(util.Prioritized(storageRenderer{}, 100)),
	),
)

// ToStorage converts Markdown to Confluence storage format (XHTML). It is the
// inverse of htmlconv.Convert for the constructs both support:
//
//   - fenced code blocks become code macros (language kept)
//   - "> **Note:** ..." quotes become note/info/warning/tip macros
//   - images become ac:image (URLs as ri:url, bare names as attachments)
//   - task list checkboxes become literal "[x] " / "[ ] " text
//
// Raw HTML is passed through. Storage-format elements (ac:*, ri:*) are not
// valid CommonMark HTML, so embed them in a fenced block with the info string
// "storage", whose content is copied verbatim:
//
//	```storage
//	<ac:structured-macro ac:name="toc" />
//	```
func ToStorage(markdown string) (string, error) {
	var buf bytes.Buffer
	if err := storageMarkdown.Convert([]byte(markdown), &buf); err != nil {
		return "", err
	}
	return strings.TrimSpace(buf.String()), nil
}

// --- admonitions ---

var kindAdmonition = ast.NewNodeKind("Admonition")

// admonition is a block quote that htmlconv produced from a Confluence
// note/info/warning/tip macro.
type admonition struct {
	ast.BaseBlock
	macro string
}

func (n *admonition) Kind() ast.NodeKind { return kindAdmonition }

func (n *admonition) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, map[string]string{"macro": n.macro}, nil)
}

var admonitionLabel = regexp.MustCompile(`^(?i)(note|info|warning|tip):$`)

type admonitionTransformer struct{}

// Transform replaces block quotes that start with a bold "Note:"-style label
// by admonition nodes, dropping the label.
func (admonitionTransformer) Transform(doc *ast.Document, reader text.Reader, _ parser.Context) {
	src := reader.Source()
	var quotes []*ast.Blockquote
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if q, ok := n.(*ast.Blockquote); ok && entering {
			quotes = append(quotes, q)
		}
		return ast.WalkContinue, nil
	})

	for _, q := range quotes {
		para, ok := q.FirstChild().(*ast.Paragraph)
		if !ok {
			continue
		}
		label, ok := para.FirstChild().(*ast.Emphasis)
		if !ok || label.Level != 2 {
			continue
		}
		m := admonitionLabel.FindStringSubmatch(plainText(label, src))
		if m == nil {
			continue
		}

		para.RemoveChild(para, label)
		if t, ok := para.FirstChild().(*ast.Text); ok {
			t.Segment = t.Segment.TrimLeftSpace(src)
		}
		if para.ChildCount() == 0 || plainText(para, src) == "" {
			q.RemoveChild(q, para)
		}

		adm := &admonition{macro: strings.ToLower(m[1])}
		for c := q.FirstChild(); c != nil; {
			next := c.NextSibling()
			q.RemoveChild(q, c)
			adm.AppendChild(adm, c)
			c = next
		}
		q.Parent().ReplaceChild(q.Parent(), q, adm)
	}
}

func plainText(n ast.Node, src []byte) string {
	var sb strings.Builder
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		if t, ok := c.(*ast.Text); ok {
			sb.Write(t.Segment.Value(src))
		} else {
			sb.WriteString(plainText(c, src))
		}
	}
	return strings.TrimSpace(sb.String())
}

// --- rendering ---

// storageFence is the info string of fenced blocks copied verbatim into the
// storage output.
const storageFence = "storage"

type storageRenderer struct{}

func (r storageRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindFencedCodeBlock, r.code)
	reg.Register(ast.KindCodeBlock, r.code)
	reg.Register(ast.KindImage, r.image)
	reg.Register(kindAdmonition, r.admonition)
	reg.Register(extast.KindTaskCheckBox, r.checkbox)
}

func (storageRenderer) code(w util.BufWriter, src []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	var body strings.Builder
	lines := n.Lines()
	for i := 0; i < lines.Len(); i++ {
		seg := lines.At(i)
		body.Write(seg.Value(src))
	}
	fc, fenced := n.(*ast.FencedCodeBlock)
	if fenced && string(fc.Language(src)) == storageFence {
		_, _ = w.WriteString(body.String())
		return ast.WalkSkipChildren, nil
	}

	_, _ = w.WriteString(`<ac:structured-macro ac:name="code">`)
	if fenced {
		if lang := fc.Language(src); len(lang) > 0 {
			_, _ = w.WriteString(`<ac:parameter ac:name="language">`)
			_, _ = w.Write(util.EscapeHTML(lang))
			_, _ = w.WriteString(`</ac:parameter>`)
		}
	}
	// "]]>" cannot appear inside CDATA; split it across two sections.
	code := strings.ReplaceAll(strings.TrimSuffix(body.String(), "\n"), "]]>", "]]]]><![CDATA[>")
	_, _ = w.WriteString(`<ac:plain-text-body><![CDATA[` + code + `]]></ac:plain-text-body></ac:structured-macro>` + "\n")
	return ast.WalkSkipChildren, nil
}

func (storageRenderer) image(w util.BufWriter, src []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	img := n.(*ast.Image)
	_, _ = w.WriteString(`<ac:image`)
	if alt := plainText(img, src); alt != "" {
		_, _ = w.WriteString(` ac:alt="`)
		_, _ = w.Write(util.EscapeHTML([]byte(alt)))
		_, _ = w.WriteString(`"`)
	}
	_, _ = w.WriteString(`>`)
	dest := util.EscapeHTML(img.Destination)
	if bytes.Contains(img.Destination, []byte("://")) {
		_, _ = w.WriteString(`<ri:url ri:value="` + string(dest) + `" />`)
	} else {
		_, _ = w.WriteString(`<ri:attachment ri:filename="` + string(dest) + `" />`)
	}
	_, _ = w.WriteString(`</ac:image>`)
	return ast.WalkSkipChildren, nil
}

func (storageRenderer) admonition(w util.BufWriter, _ []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		_, _ = w.WriteString(`<ac:structured-macro ac:name="` + n.(*admonition).macro + `"><ac:rich-text-body>` + "\n")
	} else {
		_, _ = w.WriteString("</ac:rich-text-body></ac:structured-macro>\n")
	}
	return ast.WalkContinue, nil
}

func (storageRenderer) checkbox(w util.BufWriter, _ []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		if n.(*extast.TaskCheckBox).IsChecked {
			_, _ = w.WriteString("[x] ")
		} else {
			_, _ = w.WriteString("[ ] ")
		}
	}
	return ast.WalkContinue, nil
}
