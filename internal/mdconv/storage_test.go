package mdconv

import (
	"strings"
	"testing"

	"github.com/zhani/atlassian-cli/internal/htmlconv"
)

func TestToStorage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		md   string
		want string
	}{
		{
			name: "code block becomes code macro",
			md:   "```go\nif a < b {}\n```",
			want: `<ac:structured-macro ac:name="code"><ac:parameter ac:name="language">go</ac:parameter><ac:plain-text-body><![CDATA[if a < b {}]]></ac:plain-text-body></ac:structured-macro>`,
		},
		{
			name: "CDATA terminator is split",
			md:   "```\nx]]>y\n```",
			want: `<ac:structured-macro ac:name="code"><ac:plain-text-body><![CDATA[x]]]]><![CDATA[>y]]></ac:plain-text-body></ac:structured-macro>`,
		},
		{
			name: "note quote becomes note macro",
			md:   "> **Note:** be careful",
			want: "<ac:structured-macro ac:name=\"note\"><ac:rich-text-body>\n<p>be careful</p>\n</ac:rich-text-body></ac:structured-macro>",
		},
		{
			name: "plain quote stays a quote",
			md:   "> **Bold** start",
			want: "<blockquote>\n<p><strong>Bold</strong> start</p>\n</blockquote>",
		},
		{
			name: "images",
			md:   "![diagram](https://x.y/d.png) ![](shot.png)",
			want: `<p><ac:image ac:alt="diagram"><ri:url ri:value="https://x.y/d.png" /></ac:image> <ac:image><ri:attachment ri:filename="shot.png" /></ac:image></p>`,
		},
		{
			name: "task list",
			md:   "- [x] done",
			want: "<ul>\n<li>[x] done</li>\n</ul>",
		},
		{
			name: "storage fence passes through",
			md:   "Intro\n\n```storage\n<ac:structured-macro ac:name=\"toc\" />\n```",
			want: "<p>Intro</p>\n<ac:structured-macro ac:name=\"toc\" />",
		},
		{
			name: "raw html passes through",
			md:   "<div>x</div>",
			want: "<div>x</div>",
		},
		{
			name: "xhtml line breaks",
			md:   "a  \nb\n\n---",
			want: "<p>a<br />\nb</p>\n<hr />",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ToStorage(tt.md)
			if err != nil {
				t.Fatalf("ToStorage() error: %v", err)
			}
			if got != tt.want {
				t.Errorf("ToStorage(%q) =\n%s\nwant\n%s", tt.md, got, tt.want)
			}
		})
	}
}

// TestStorageRoundTrip checks that Markdown produced by htmlconv survives a
// trip through ToStorage and back, which is what `confluence page edit` does.
func TestStorageRoundTrip(t *testing.T) {
	t.Parallel()
	docs := []string{
		"# Title\n\nSome **bold**, *italic*, ~~gone~~ and `code`.",
		"## List\n\n- one\n- two\n\n1. first\n2. second",
		"```python\nprint(\"hi\")\nif a < b:\n    pass\n```",
		"> **Warning:** first line\n>\n> second paragraph",
		"| A | B |\n|---|---|\n| 1 | 2 |",
		"See [docs](https://example.com) and ![d](diagram.png).",
		"> quoted text",
	}
	for _, md := range docs {
		storage, err := ToStorage(md)
		if err != nil {
			t.Fatalf("ToStorage(%q): %v", md, err)
		}
		if lost := htmlconv.Unsupported(storage); len(lost) > 0 {
			t.Errorf("ToStorage(%q) produced unsupported features %v:\n%s", md, lost, storage)
		}
		back := htmlconv.Convert(storage)
		if normalize(back) != normalize(md) {
			t.Errorf("round trip changed the document\nstart:\n%s\nstorage:\n%s\nback:\n%s", md, storage, back)
		}
	}
}

// normalize ignores blank-line differences between the two converters.
func normalize(s string) string {
	var lines []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimRight(l, " "); l != "" {
			lines = append(lines, l)
		}
	}
	return strings.Join(lines, "\n")
}
