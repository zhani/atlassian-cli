package htmlconv

import (
	"net/url"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

// UserRef identifies the user of a mention (<ri:user>): Server/DC pages name
// the user key, Cloud pages the account ID, old pages the username.
type UserRef struct {
	Key, AccountID, Username string
}

// User is a resolved mention.
type User struct {
	DisplayName, Email string
}

// Options tunes Convert.
type Options struct {
	// User resolves a mention; nil, or ok false, leaves it as @username
	// (or "@unknown user"), since storage carries no names.
	User func(ref UserRef) (u User, ok bool)
}

// Convert transforms HTML (including Confluence storage format) into GitHub-flavored Markdown.
func Convert(htmlStr string) string {
	return ConvertWith(htmlStr, Options{})
}

// ConvertWith is Convert with mentions resolved through opts.
func ConvertWith(htmlStr string, opts Options) string {
	if htmlStr == "" {
		return ""
	}

	doc, err := html.Parse(strings.NewReader(htmlStr))
	if err != nil {
		return htmlStr
	}

	var b strings.Builder
	c := converter{opts: opts}
	c.walkNode(&b, doc)
	return strings.TrimSpace(b.String())
}

// Mentions lists the users the storage mentions, each once.
func Mentions(storageXHTML string) []UserRef {
	doc, err := html.Parse(strings.NewReader(storageXHTML))
	if err != nil {
		return nil
	}
	var out []UserRef
	seen := map[UserRef]bool{}
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "ri:user" {
			if r := userRef(n); r != (UserRef{}) && !seen[r] {
				seen[r] = true
				out = append(out, r)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return out
}

func userRef(n *html.Node) UserRef {
	return UserRef{Key: getAttr(n, "ri:userkey"), AccountID: getAttr(n, "ri:account-id"), Username: getAttr(n, "ri:username")}
}

type converter struct {
	opts Options
}

func (cv converter) walkNode(b *strings.Builder, n *html.Node) {
	switch n.Type {
	case html.TextNode:
		b.WriteString(strings.ReplaceAll(n.Data, " ", " "))
		return
	case html.ElementNode:
		cv.renderElement(b, n)
		return
	case html.CommentNode:
		// The HTML parser reads storage-format CDATA sections (used for code
		// macro bodies) as comments.
		if text, ok := cdata(n); ok {
			b.WriteString(text)
		}
		return
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		cv.walkNode(b, c)
	}
}

func (cv converter) renderElement(b *strings.Builder, n *html.Node) {
	tag := n.Data

	// Handle Confluence storage format macros
	if tag == "ac:structured-macro" {
		cv.renderMacro(b, n)
		return
	}
	if tag == "ac:link" {
		cv.renderACLink(b, n)
		return
	}
	if tag == "ac:image" {
		cv.renderACImage(b, n)
		return
	}
	if tag == "ac:emoticon" {
		cv.walkChildren(b, n) // parser may absorb following text nodes as children
		return
	}
	if tag == "ac:task-list" {
		cv.renderTaskList(b, n)
		return
	}
	if tag == "ac:plain-text-body" || tag == "ac:rich-text-body" {
		cv.walkChildren(b, n)
		return
	}
	if strings.HasPrefix(tag, "ac:") || strings.HasPrefix(tag, "ri:") {
		cv.walkChildren(b, n)
		return
	}

	switch tag {
	case "h1", "h2", "h3", "h4", "h5", "h6":
		level := map[string]string{"h1": "#", "h2": "##", "h3": "###", "h4": "####", "h5": "#####", "h6": "######"}[tag]
		var inner strings.Builder
		cv.walkChildren(&inner, n)
		b.WriteString("\n")
		b.WriteString(level)
		b.WriteString(" ")
		b.WriteString(strings.TrimSpace(inner.String()))
		b.WriteString("\n\n")
	case "p":
		cv.walkChildren(b, n)
		b.WriteString("\n\n")
	case "br":
		b.WriteString("\n")
	case "strong", "b":
		b.WriteString("**")
		cv.walkChildren(b, n)
		b.WriteString("**")
	case "em", "i":
		b.WriteString("*")
		cv.walkChildren(b, n)
		b.WriteString("*")
	case "s", "del", "strike":
		b.WriteString("~~")
		cv.walkChildren(b, n)
		b.WriteString("~~")
	case "code":
		b.WriteString("`")
		cv.walkChildren(b, n)
		b.WriteString("`")
	case "pre":
		b.WriteString("\n```\n")
		cv.walkChildren(b, n)
		b.WriteString("\n```\n\n")
	case "a":
		href := getAttr(n, "href")
		b.WriteString("[")
		cv.walkChildren(b, n)
		b.WriteString("](")
		b.WriteString(href)
		b.WriteString(")")
	case "ul":
		b.WriteString("\n")
		cv.renderList(b, n, false)
		b.WriteString("\n")
	case "ol":
		b.WriteString("\n")
		cv.renderList(b, n, true)
		b.WriteString("\n")
	case "li":
		cv.walkChildren(b, n)
	case "table":
		cv.renderTable(b, n)
	case "blockquote":
		var inner strings.Builder
		cv.walkChildren(&inner, n)
		for _, line := range strings.Split(strings.TrimSpace(inner.String()), "\n") {
			b.WriteString("> ")
			b.WriteString(line)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	case "hr":
		b.WriteString("\n---\n\n")
	case "img":
		alt := getAttr(n, "alt")
		src := getAttr(n, "src")
		b.WriteString("![")
		b.WriteString(alt)
		b.WriteString("](")
		b.WriteString(src)
		b.WriteString(")")
	default:
		cv.walkChildren(b, n)
	}
}

func (cv converter) renderMacro(b *strings.Builder, n *html.Node) {
	macroName := getAttr(n, "ac:name")

	switch macroName {
	case "code":
		lang := getMacroParam(n, "language")
		b.WriteString("\n```")
		b.WriteString(lang)
		b.WriteString("\n")
		body := cv.getMacroBody(n)
		b.WriteString(body)
		if !strings.HasSuffix(body, "\n") {
			b.WriteString("\n")
		}
		b.WriteString("```\n\n")

	case "info", "note", "warning", "tip":
		label := strings.ToUpper(macroName[:1]) + macroName[1:]
		var inner strings.Builder
		cv.walkChildren(&inner, n)
		text := strings.TrimSpace(inner.String())
		b.WriteString("> **")
		b.WriteString(label)
		b.WriteString(":** ")
		b.WriteString(quoteContinuation(text))
		b.WriteString("\n\n")

	case "status":
		title := getMacroParam(n, "title")
		b.WriteString(title)

	case "toc":
		// Remove table of contents macro

	case "jira":
		// The issue key (the page browser's L follows it), or the query of
		// an issue list; never the server name/ID parameters.
		switch k, jql := getMacroParam(n, "key"), getMacroParam(n, "jqlQuery"); {
		case k != "":
			b.WriteString("**" + strings.TrimSpace(k) + "**")
		case jql != "":
			b.WriteString("Jira issues: `" + strings.TrimSpace(jql) + "`")
		}

	case "viewpdf":
		// An embedded PDF, as an image of the attachment: the page browser
		// shows such an "image" as a slide show of its pages.
		name := macroAttachment(n, "name")
		if name == "" {
			return
		}
		if out := b.String(); out != "" && !strings.HasSuffix(out, "\n\n") {
			b.WriteString("\n\n")
		}
		// Escaped: a Markdown link target can't hold spaces.
		b.WriteString("![" + name + "](" + url.PathEscape(name) + ")\n\n")
		if u := strings.TrimSpace(getMacroParam(n, "embedded-url")); u != "" {
			b.WriteString("*Offline copy of " + u + "*\n\n")
		}

	default:
		cv.walkChildren(b, n)
	}
}

// macroAttachment is the attachment filename a macro parameter names
// (<ri:attachment ri:filename="…"/>).
func macroAttachment(n *html.Node, paramName string) string {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode || c.Data != "ac:parameter" || getAttr(c, "ac:name") != paramName {
			continue
		}
		for a := c.FirstChild; a != nil; a = a.NextSibling {
			if a.Type == html.ElementNode && a.Data == "ri:attachment" {
				return getAttr(a, "ri:filename")
			}
		}
	}
	return ""
}

func (cv converter) renderACLink(b *strings.Builder, n *html.Node) {
	var page, user *html.Node
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode {
			continue
		}
		switch c.Data {
		case "ri:page":
			page = c
		case "ri:user":
			user = c
		}
	}
	switch {
	case user != nil:
		b.WriteString(cv.mention(userRef(user)))
	case page != nil && getAttr(page, "ri:content-title") != "":
		// No URL: the page is named by space and title, and a mirror's
		// base URL would lead nowhere. "#" renders as a link without one.
		text := strings.TrimSpace(cv.linkBody(n))
		if text == "" {
			text = getAttr(page, "ri:content-title")
		}
		b.WriteString("[" + strings.ReplaceAll(text, "]", `\]`) + "](#)")
	default:
		cv.walkChildren(b, n)
	}
}

// linkBody is the text an ac:link shows instead of its target's name. It is
// searched for in depth: the HTML parser ignores "/>" on unknown tags, so
// <ri:page/> holds the elements after it.
func (cv converter) linkBody(n *html.Node) string {
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode {
			continue
		}
		if c.Data == "ac:link-body" || c.Data == "ac:plain-text-link-body" {
			cv.walkChildren(&b, c)
		} else {
			b.WriteString(cv.linkBody(c))
		}
	}
	return b.String()
}

// mention renders a user mention the way Confluence shows it: the user's
// name, here with the e-mail address the hover card would show.
func (cv converter) mention(r UserRef) string {
	if cv.opts.User != nil {
		if u, ok := cv.opts.User(r); ok && u.DisplayName != "" {
			if u.Email != "" {
				// Escaped: an autolink would render with a "mailto:" copy.
				return "@" + u.DisplayName + ` \<` + u.Email + `\>`
			}
			return "@" + u.DisplayName
		}
	}
	if r.Username != "" {
		return "@" + r.Username
	}
	return "@unknown user"
}

// renderTaskList renders an ac:task-list as GFM task items; nested lists
// (in a task body) are indented under their task.
func (cv converter) renderTaskList(b *strings.Builder, n *html.Node) {
	if out := b.String(); out != "" && !strings.HasSuffix(out, "\n") {
		b.WriteString("\n")
	}
	b.WriteString(cv.taskLines(n))
	b.WriteString("\n")
}

func (cv converter) taskLines(list *html.Node) string {
	var b strings.Builder
	for t := list.FirstChild; t != nil; t = t.NextSibling {
		if t.Type != html.ElementNode || t.Data != "ac:task" {
			continue
		}
		done := false
		var text, nested strings.Builder
		for c := t.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode {
				continue
			}
			switch c.Data {
			case "ac:task-status":
				done = strings.TrimSpace(textOf(c)) == "complete"
			case "ac:task-body":
				cv.taskBody(&text, &nested, c)
			}
		}
		if done {
			b.WriteString("- [x] ")
		} else {
			b.WriteString("- [ ] ")
		}
		b.WriteString(strings.Join(strings.Fields(text.String()), " "))
		b.WriteString("\n")
		for _, line := range strings.Split(strings.TrimRight(nested.String(), "\n"), "\n") {
			if line != "" {
				b.WriteString("  " + line + "\n")
			}
		}
	}
	return b.String()
}

// taskBody splits a task body into its own text and its nested task lists,
// which may sit inside the body's inline wrappers.
func (cv converter) taskBody(text, nested *strings.Builder, n *html.Node) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		switch {
		case c.Type == html.ElementNode && c.Data == "ac:task-list":
			nested.WriteString(cv.taskLines(c))
		case c.Type == html.ElementNode && hasTaskList(c) && (c.Data == "span" || c.Data == "p" || c.Data == "div"):
			cv.taskBody(text, nested, c)
		default:
			cv.walkNode(text, c)
		}
	}
}

func hasTaskList(n *html.Node) bool {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && (c.Data == "ac:task-list" || hasTaskList(c)) {
			return true
		}
	}
	return false
}

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

// renderACImage renders an ac:image as a Markdown image whose source is the
// URL (ri:url) or the attachment filename (ri:attachment).
func (cv converter) renderACImage(b *strings.Builder, n *html.Node) {
	src := ""
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode {
			continue
		}
		switch c.Data {
		case "ri:url":
			src = getAttr(c, "ri:value")
		case "ri:attachment":
			src = getAttr(c, "ri:filename")
		}
	}
	b.WriteString("![")
	b.WriteString(getAttr(n, "ac:alt"))
	b.WriteString("](")
	b.WriteString(src)
	b.WriteString(")")
}

func (cv converter) renderList(b *strings.Builder, n *html.Node, ordered bool) {
	idx := 1
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data == "li" {
			if tl := onlyTaskList(c); tl != nil {
				b.WriteString(cv.taskLines(tl))
				continue
			}
			if ordered {
				b.WriteString(strconv.Itoa(idx))
				b.WriteString(". ")
				idx++
			} else {
				b.WriteString("- ")
			}
			var inner strings.Builder
			cv.walkChildren(&inner, c)
			b.WriteString(strings.TrimSpace(inner.String()))
			b.WriteString("\n")
		}
	}
}

// onlyTaskList returns the task list a list item holds when it holds nothing
// else: Confluence indents a task list by wrapping it in a bullet-less item.
func onlyTaskList(li *html.Node) *html.Node {
	var tl *html.Node
	for c := li.FirstChild; c != nil; c = c.NextSibling {
		switch {
		case c.Type == html.ElementNode && c.Data == "ac:task-list" && tl == nil:
			tl = c
		case c.Type == html.TextNode && strings.TrimSpace(c.Data) == "":
		default:
			return nil
		}
	}
	return tl
}

func (cv converter) renderTable(b *strings.Builder, n *html.Node) {
	var rows [][]string
	cv.collectRows(n, &rows)
	if len(rows) == 0 {
		return
	}

	// Compute column widths
	cols := 0
	for _, row := range rows {
		if len(row) > cols {
			cols = len(row)
		}
	}

	b.WriteString("\n")
	// Header row
	b.WriteString("|")
	for i := 0; i < cols; i++ {
		cell := ""
		if i < len(rows[0]) {
			cell = rows[0][i]
		}
		b.WriteString(" ")
		b.WriteString(cell)
		b.WriteString(" |")
	}
	b.WriteString("\n")

	// Separator
	b.WriteString("|")
	for i := 0; i < cols; i++ {
		b.WriteString("---|")
	}
	b.WriteString("\n")

	// Data rows
	for _, row := range rows[1:] {
		b.WriteString("|")
		for i := 0; i < cols; i++ {
			cell := ""
			if i < len(row) {
				cell = row[i]
			}
			b.WriteString(" ")
			b.WriteString(cell)
			b.WriteString(" |")
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
}

func (cv converter) collectRows(n *html.Node, rows *[][]string) {
	if n.Type == html.ElementNode && n.Data == "tr" {
		var cells []string
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && (c.Data == "td" || c.Data == "th") {
				var cellBuf strings.Builder
				cv.walkChildren(&cellBuf, c)
				cells = append(cells, strings.TrimSpace(cellBuf.String()))
			}
		}
		*rows = append(*rows, cells)
		return
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		cv.collectRows(c, rows)
	}
}

func (cv converter) walkChildren(b *strings.Builder, n *html.Node) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		cv.walkNode(b, c)
	}
}

// cdata returns the content of a comment node that holds a CDATA section.
func cdata(n *html.Node) (string, bool) {
	inner, ok := strings.CutPrefix(n.Data, "[CDATA[")
	if !ok {
		return "", false
	}
	return strings.TrimSuffix(inner, "]]"), true
}

// quoteContinuation prefixes every line after the first with "> " so that
// multi-line text stays inside a block quote. Runs of blank lines collapse
// into one paragraph break.
func quoteContinuation(text string) string {
	var out []string
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, " ")
		switch {
		case i == 0:
			out = append(out, line)
		case line == "":
			if out[len(out)-1] != ">" {
				out = append(out, ">")
			}
		default:
			out = append(out, "> "+line)
		}
	}
	return strings.Join(out, "\n")
}

func getAttr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		attrKey := a.Key
		if a.Namespace != "" {
			attrKey = a.Namespace + ":" + a.Key
		}
		if attrKey == key {
			return a.Val
		}
	}
	return ""
}

func getMacroParam(n *html.Node, paramName string) string {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data == "ac:parameter" {
			name := getAttr(c, "ac:name")
			if name == paramName && c.FirstChild != nil {
				return c.FirstChild.Data
			}
		}
	}
	return ""
}

func (cv converter) getMacroBody(n *html.Node) string {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data == "ac:plain-text-body" {
			var b strings.Builder
			cv.walkChildren(&b, c)
			return b.String()
		}
	}
	return ""
}
