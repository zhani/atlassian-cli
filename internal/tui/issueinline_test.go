package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/zhani/atlassian-cli/internal/jira"
	"github.com/zhani/atlassian-cli/internal/termimg"
)

func drainIssues(m model, cmd tea.Cmd) model {
	queue := []tea.Cmd{cmd}
	for i := 0; len(queue) > 0 && i < 100; i++ {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		switch msg := c().(type) {
		case tea.BatchMsg:
			queue = append(queue, msg...)
		case spinner.TickMsg, tea.QuitMsg, nil:
		default:
			next, nc := m.Update(msg)
			m = next.(model)
			queue = append(queue, nc)
		}
	}
	return m
}

// inlineIssue: an Xray test whose step 2 failed with evidence, an image
// attachment, a text attachment and an external image in its description.
func inlineIssue(t *testing.T, external string) *jira.Issue {
	t.Helper()
	var is jira.Issue
	raw := `{"key":"T-1","fields":{"summary":"radio test","description":"see image",
		"attachment":[{"filename":"photo.png","mimeType":"image/png","content":"/secure/attachment/1/photo.png"},
		              {"filename":"log.txt","mimeType":"text/plain","content":"/secure/attachment/2/log.txt"}],
		"customfield_7":{"steps":[{"index":1,"fields":{"Action":"a1"}},{"index":2,"fields":{"Action":"a2"}}]}},
		"renderedFields":{"description":"<p><img src=\"` + external + `\"/></p>"}}`
	if err := json.Unmarshal([]byte(raw), &is); err != nil {
		t.Fatal(err)
	}
	var run map[string]any
	json.Unmarshal([]byte(`{"id":5001,"status":"FAIL","steps":[{"index":1,"status":"PASS"},
		{"index":2,"status":"FAIL","evidences":[{"id":9,"fileName":"scope 1.png",
		 "fileURL":"https://jira.example.com/plugins/servlet/raven/attachment/9/scope 1.png"}]}]}`), &run)
	is.XrayRuns = []map[string]any{run}
	return &is
}

func openInline(t *testing.T, gfx *graphics) (model, *imageSource, *atomic.Int32) {
	t.Helper()
	img := testPNG(t)
	var external atomic.Int32
	ext := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("credentials sent to an external host")
		}
		external.Add(1)
		w.Write(img)
	}))
	t.Cleanup(ext.Close)
	is := inlineIssue(t, ext.URL+"/pic.png")
	src := &imageSource{files: map[string][]byte{
		"https://jira.example.com/plugins/servlet/raven/attachment/9/scope%201.png": img,
		"/secure/attachment/1/photo.png":                                            img,
	}}
	m := newModel(context.Background(), Options{Source: src, Issues: []jira.Issue{*is}}, plainRenderer)
	m.gfx = gfx
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 400})
	m = next.(model)
	m.mode, m.detailKey = modeDetail, is.Key
	next, cmd := m.Update(issueLoadedMsg{key: is.Key, issue: is})
	return drainIssues(next.(model), cmd), src, &external
}

func TestIssueInlineImagesAsBlocks(t *testing.T) {
	m, src, external := openInline(t, &graphics{caps: termimg.Caps{InPage: termimg.Blocks, Preview: termimg.Blocks}, cellW: 8, cellH: 16})
	view := m.detail.View()
	for _, want := range []string{"▣ image 1: pic.png", "▣ image 2: scope 1.png", "▣ image 3: photo.png", "▀"} {
		if !strings.Contains(view, want) {
			t.Errorf("missing %q in:\n%s", want, view)
		}
	}
	if strings.Contains(view, "loading") || strings.Contains(view, "log.txt)") {
		t.Errorf("all images should be drawn, text files not embedded:\n%s", view)
	}
	if got := strings.Join(src.downloads, ","); !strings.Contains(got, "scope%201.png") || !strings.Contains(got, "/secure/attachment/1/photo.png") || strings.Contains(got, "pic.png") {
		t.Errorf("site images through the client, the external one not: %s", got)
	}
	if external.Load() != 1 {
		t.Errorf("external image fetched %d times", external.Load())
	}
}

func TestIssueInlineCaptionsWhileLoading(t *testing.T) {
	is := inlineIssue(t, "https://elsewhere.example/pic.png")
	m := newModel(context.Background(), Options{Source: &imageSource{}, Issues: []jira.Issue{*is}}, plainRenderer)
	m.gfx = &graphics{caps: termimg.Caps{InPage: termimg.Blocks}, cellW: 8, cellH: 16}
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 400})
	m = next.(model)
	m.mode, m.detailKey = modeDetail, is.Key
	next, cmd := m.Update(issueLoadedMsg{key: is.Key, issue: is})
	if cmd == nil || !strings.Contains(next.(model).detail.View(), "image 2: scope 1.png (loading…)") {
		t.Errorf("want loading captions and a download command:\n%s", next.(model).detail.View())
	}
	// A result for an issue loaded again meanwhile is dropped.
	m = next.(model)
	old := m.inline[is.Key]
	again := *is
	next, _ = m.Update(issueLoadedMsg{key: is.Key, issue: &again})
	m = next.(model)
	next, _ = m.Update(inlineImageMsg{key: is.Key, in: old, idx: 0, img: nil})
	if next.(model).inline[is.Key] == old || next.(model).inline[is.Key].imgs[0].loaded {
		t.Error("stale image result applied")
	}
}

func TestIssueInlineKitty(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "tty")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gfx := &graphics{caps: termimg.Caps{InPage: termimg.Kitty, Preview: termimg.Kitty}, out: termimg.NewOutput(f), cellW: 8, cellH: 16}
	m, _, _ := openInline(t, gfx)
	if v := m.detail.View(); !strings.ContainsRune(v, '\U0010EEEE') {
		t.Errorf("no kitty placeholders:\n%s", v)
	}
	sent, _ := os.ReadFile(f.Name())
	if !bytes.Contains(sent, []byte("\x1b_Ga=T,U=1,f=100")) {
		t.Errorf("images not transmitted: %.80q", sent)
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	_ = next
	if sent, _ = os.ReadFile(f.Name()); !bytes.Contains(sent, []byte("_Ga=d,d=I,")) {
		t.Error("images not deleted on refresh")
	}
}

func TestIssueInlineOffWithoutGraphics(t *testing.T) {
	m, src, external := openInline(t, nil)
	if len(src.downloads) != 0 || external.Load() != 0 || strings.Contains(m.detail.View(), "▣") {
		t.Errorf("no graphics: nothing downloaded or drawn (%v, %d)", src.downloads, external.Load())
	}
}
