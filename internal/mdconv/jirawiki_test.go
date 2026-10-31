package mdconv

import "testing"

func TestToJiraWiki(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		md   string
		want string
	}{
		{
			name: "headings and paragraphs",
			md:   "# Title\n\nSome text\nwrapped here.\n\n### Sub",
			want: "h1. Title\n\nSome text wrapped here.\n\nh3. Sub\n",
		},
		{
			name: "inline formatting",
			md:   "**bold** *italic* ~~gone~~ `x := y[0]`",
			want: "*bold* _italic_ -gone- {{x := y[0]}}\n",
		},
		{
			name: "links and images",
			md:   "[docs](https://example.com) <https://a.b> https://c.d ![shot](img.png)",
			want: "[docs|https://example.com] [https://a.b] [https://c.d] !img.png!\n",
		},
		{
			name: "code block with language",
			md:   "```go\nfunc main() {}\n```",
			want: "{code:go}\nfunc main() {}\n{code}\n",
		},
		{
			name: "code block without language keeps content verbatim",
			md:   "```\n*not bold* {x}\n```",
			want: "{code}\n*not bold* {x}\n{code}\n",
		},
		{
			name: "nested lists",
			md:   "- one\n- two\n  1. a\n  2. b\n     - deep\n- three",
			want: "* one\n* two\n*# a\n*# b\n*#* deep\n* three\n",
		},
		{
			name: "task list",
			md:   "- [x] done\n- [ ] todo",
			want: "* [x] done\n* [ ] todo\n",
		},
		{
			name: "loose list item with two paragraphs",
			md:   "- first\n\n  second\n- next",
			want: "* first \\\\ second\n* next\n",
		},
		{
			name: "quote",
			md:   "> quoted\n> text",
			want: "{quote}\nquoted text\n{quote}\n",
		},
		{
			name: "table",
			md:   "| A | B |\n|---|---|\n| 1 | x \\| y |\n|   | 2 |",
			want: "||A||B||\n|1|x \\| y|\n| |2|\n",
		},
		{
			name: "horizontal rule",
			md:   "a\n\n---\n\nb",
			want: "a\n\n----\n\nb\n",
		},
		{
			name: "escapes markup characters",
			md:   "use {braces} and \\[brackets\\], \\*stars\\* but keep snake_case and well-known",
			want: "use \\{braces\\} and \\[brackets\\], \\*stars* but keep snake_case and well-known\n",
		},
		{
			name: "escapes line-start markup",
			md:   "\\- not a list\n\nh1. not a heading",
			want: "\\- not a list\n\n\\h1. not a heading\n",
		},
		{
			name: "hard line break",
			md:   "line one  \nline two",
			want: "line one\nline two\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ToJiraWiki(tt.md); got != tt.want {
				t.Errorf("ToJiraWiki(%q) =\n%q\nwant\n%q", tt.md, got, tt.want)
			}
		})
	}
}
