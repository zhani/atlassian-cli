package htmlconv

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestUnsupported(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		storage string
		want    []string
	}{
		{
			name:    "supported content",
			storage: `<h1>T</h1><p><strong>a</strong> <a href="x">b</a></p><table class="wrapped"><colgroup><col/></colgroup><tbody><tr><th>h</th></tr><tr><td>c</td></tr></tbody></table><ac:structured-macro ac:name="code"><ac:parameter ac:name="language">go</ac:parameter><ac:plain-text-body><![CDATA[x]]></ac:plain-text-body></ac:structured-macro><ac:image><ri:attachment ri:filename="a.png" /></ac:image>`,
			want:    []string{},
		},
		{
			name: "lossy features",
			storage: `<p><ac:link><ri:page ri:content-title="Other" /></ac:link> <ac:link><ri:user ri:account-id="1" /></ac:link></p>` +
				`<ac:structured-macro ac:name="jira"><ac:parameter ac:name="key">A-1</ac:parameter></ac:structured-macro>` +
				`<ac:structured-macro ac:name="code"><ac:parameter ac:name="title">T</ac:parameter><ac:plain-text-body><![CDATA[x]]></ac:plain-text-body></ac:structured-macro>` +
				`<p style="text-align: center">c</p><table><tbody><tr><td colspan="2">m</td></tr></tbody></table><p><u>u</u></p>`,
			want: []string{
				"ac:link (2)",
				"macro code parameter title (1)",
				"macro jira (1)",
				"merged table cells (1)",
				"style attribute (1)",
				"u (1)",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if diff := cmp.Diff(tt.want, Unsupported(tt.storage)); diff != "" {
				t.Errorf("Unsupported() diff (-want +got):\n%s", diff)
			}
		})
	}
}
