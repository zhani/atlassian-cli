package htmlconv

import "testing"

func TestConvert(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty string", "", ""},
		{"plain text", "hello", "hello"},
		{"h1", "<h1>Title</h1>", "# Title"},
		{"h2", "<h2>Section</h2>", "## Section"},
		{"h3", "<h3>Sub</h3>", "### Sub"},
		{"h4", "<h4>Sub</h4>", "#### Sub"},
		{"h5", "<h5>Sub</h5>", "##### Sub"},
		{"h6", "<h6>Sub</h6>", "###### Sub"},
		{"p", "<p>Hello</p>", "Hello"},
		{"br", "a<br>b", "a\nb"},
		{"strong", "<strong>bold</strong>", "**bold**"},
		{"b", "<b>bold</b>", "**bold**"},
		{"em", "<em>italic</em>", "*italic*"},
		{"i", "<i>italic</i>", "*italic*"},
		{"code inline", "<code>x</code>", "`x`"},
		{"anchor", `<a href="https://example.com">link</a>`, "[link](https://example.com)"},
		{"pre", "<pre>code here</pre>", "```\ncode here\n```"},
		{"hr", "<hr>", "---"},
		{"blockquote", "<blockquote>quoted</blockquote>", "> quoted"},
		{"ul", "<ul><li>A</li><li>B</li></ul>", "- A\n- B"},
		{"ol", "<ol><li>First</li><li>Second</li></ol>", "1. First\n2. Second"},
		{
			"table simple",
			"<table><tr><th>Name</th><th>Val</th></tr><tr><td>foo</td><td>bar</td></tr></table>",
			"| Name | Val |\n|---|---|\n| foo | bar |",
		},
		{"img", `<img src="img.png" alt="desc">`, "![desc](img.png)"},
		{
			"code macro no lang",
			`<ac:structured-macro ac:name="code"><ac:plain-text-body>x</ac:plain-text-body></ac:structured-macro>`,
			"```\nx\n```",
		},
		{
			"code macro with lang",
			`<ac:structured-macro ac:name="code"><ac:parameter ac:name="language">go</ac:parameter><ac:plain-text-body>package main</ac:plain-text-body></ac:structured-macro>`,
			"```go\npackage main\n```",
		},
		{
			"code macro with CDATA body",
			`<ac:structured-macro ac:name="code"><ac:parameter ac:name="language">go</ac:parameter><ac:plain-text-body><![CDATA[if a < b && c {
	x()
}]]></ac:plain-text-body></ac:structured-macro>`,
			"```go\nif a < b && c {\n\tx()\n}\n```",
		},
		{
			"multi-paragraph note stays quoted",
			`<ac:structured-macro ac:name="note"><ac:rich-text-body><p>first</p><p>second</p></ac:rich-text-body></ac:structured-macro>`,
			"> **Note:** first\n>\n> second",
		},
		{
			"info macro",
			`<ac:structured-macro ac:name="info"><ac:rich-text-body>This is info</ac:rich-text-body></ac:structured-macro>`,
			"> **Info:** This is info",
		},
		{
			"note macro",
			`<ac:structured-macro ac:name="note"><ac:rich-text-body>This is note</ac:rich-text-body></ac:structured-macro>`,
			"> **Note:** This is note",
		},
		{
			"warning macro",
			`<ac:structured-macro ac:name="warning"><ac:rich-text-body>This is warning</ac:rich-text-body></ac:structured-macro>`,
			"> **Warning:** This is warning",
		},
		{
			"tip macro",
			`<ac:structured-macro ac:name="tip"><ac:rich-text-body>This is tip</ac:rich-text-body></ac:structured-macro>`,
			"> **Tip:** This is tip",
		},
		{
			"ac:link with page title",
			`<ac:link><ri:page ri:content-title="My Page"/></ac:link>`,
			"[My Page](#)",
		},
		{"emoticon skipped, following text preserved", `text<ac:emoticon ac:name="smile"/>more`, "textmore"},
		{
			"h2 with emoticon and nbsp",
			"<h2><ac:emoticon ac:name=\"blue-star\" ac:emoji-shortname=\":thinking:\" /> 問題の提示</h2>",
			"## 問題の提示",
		},
		{
			"ac:placeholder text preserved",
			"<p><ac:placeholder>プレースホルダーテキスト</ac:placeholder></p>",
			"プレースホルダーテキスト",
		},
		{
			"status macro",
			`<ac:structured-macro ac:name="status"><ac:parameter ac:name="title">進行中</ac:parameter><ac:parameter ac:name="colour">Yellow</ac:parameter></ac:structured-macro>`,
			"進行中",
		},
		{"p with inline code", "<p>Use <code>go test</code> to run</p>", "Use `go test` to run"},
		{
			"jira issue macro",
			`<p>Tracked in <ac:structured-macro ac:name="jira" ac:schema-version="1"><ac:parameter ac:name="server">Corp Jira</ac:parameter><ac:parameter ac:name="serverId">abc-123</ac:parameter><ac:parameter ac:name="key">DEV-42</ac:parameter></ac:structured-macro></p>`,
			"Tracked in **DEV-42**",
		},
		{
			"jira query macro",
			`<ac:structured-macro ac:name="jira"><ac:parameter ac:name="server">Corp Jira</ac:parameter><ac:parameter ac:name="jqlQuery">project = DEV</ac:parameter></ac:structured-macro>`,
			"Jira issues: `project = DEV`",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := Convert(tt.input)
			if got != tt.want {
				t.Errorf("Convert(%q)\n  got:  %q\n  want: %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestConvertViewPDF(t *testing.T) {
	t.Parallel()
	in := `<p>Deck:</p><ac:structured-macro ac:name="viewpdf"><ac:parameter ac:name="name"><ri:attachment ri:filename="Intro deck.pdf" /></ac:parameter>` +
		`<ac:parameter ac:name="embedded-url">https://docs.google.com/x</ac:parameter></ac:structured-macro>`
	want := "Deck:\n\n![Intro deck.pdf](Intro%20deck.pdf)\n\n*Offline copy of https://docs.google.com/x*"
	if got := Convert(in); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got := Convert(`<ac:structured-macro ac:name="viewpdf"></ac:structured-macro>`); got != "" {
		t.Errorf("no attachment: got %q", got)
	}
}

// A task list as Confluence stores it: task ids/statuses must not leak as
// text, mentions and page links must render.
func TestConvertTaskList(t *testing.T) {
	t.Parallel()
	in := `<h3>Development</h3><ac:task-list>
<ac:task>
<ac:task-id>6</ac:task-id>
<ac:task-status>complete</ac:task-status>
<ac:task-body><span>Review stories. <ac:link><ri:user ri:userkey="k1" /></ac:link> </span></ac:task-body>
</ac:task>
<ac:task>
<ac:task-id>2461</ac:task-id>
<ac:task-status>incomplete</ac:task-status>
<ac:task-body><span><span>L</span>ink stories as defined in <ac:link><ri:page ri:space-key="DEV" ri:content-title="Feature intro" /></ac:link>. <ac:link><ri:user ri:userkey="k2" /></ac:link> </span></ac:task-body>
</ac:task>
</ac:task-list><ul><li style="list-style-type: none;"><ac:task-list>
<ac:task>
<ac:task-id>2330</ac:task-id>
<ac:task-uuid>ce225876</ac:task-uuid>
<ac:task-status>complete</ac:task-status>
<ac:task-body><span>Feature specification </span><ac:task-list>
<ac:task>
<ac:task-id>2608</ac:task-id>
<ac:task-status>incomplete</ac:task-status>
<ac:task-body><span>Nested<br /></span></ac:task-body>
</ac:task>
</ac:task-list></ac:task-body>
</ac:task>
</ac:task-list></li></ul>`
	users := func(r UserRef) (User, bool) {
		if r.Key == "k1" {
			return User{DisplayName: "Doe, Jane", Email: "jane@example.com"}, true
		}
		return User{}, false
	}
	want := "### Development\n\n" +
		"- [x] Review stories. @Doe, Jane \\<jane@example.com\\>\n" +
		"- [ ] Link stories as defined in [Feature intro](#). @unknown user\n" +
		"\n\n- [x] Feature specification\n" +
		"  - [ ] Nested"
	if got := ConvertWith(in, Options{User: users}); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	if got := Mentions(in); len(got) != 2 || got[0].Key != "k1" || got[1].Key != "k2" {
		t.Errorf("Mentions = %+v", got)
	}
}

func TestConvertMentionFallbacks(t *testing.T) {
	t.Parallel()
	if got := Convert(`<p>by <ac:link><ri:user ri:username="jdoe" /></ac:link></p>`); got != "by @jdoe" {
		t.Errorf("username: got %q", got)
	}
	got := Convert(`<ac:link><ri:page ri:content-title="T" /><ac:plain-text-link-body><![CDATA[shown]]></ac:plain-text-link-body></ac:link>`)
	if got != "[shown](#)" {
		t.Errorf("link body: got %q", got)
	}
}
