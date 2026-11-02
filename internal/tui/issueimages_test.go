package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/zhani/atlassian-cli/internal/jira"
	"github.com/zhani/atlassian-cli/internal/termimg"
)

// imageSource is fakeSource plus attachment downloads.
type imageSource struct {
	fakeSource
	files     map[string][]byte
	downloads []string // URLs fetched through the (authenticated) source
}

func (s *imageSource) DownloadAttachment(_ context.Context, u string) (io.ReadCloser, error) {
	s.mu.Lock()
	s.downloads = append(s.downloads, u)
	s.mu.Unlock()
	data, ok := s.files[u]
	if !ok {
		return nil, errors.New("404")
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func pngBytes(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func testIssue(t *testing.T) *jira.Issue {
	t.Helper()
	var is jira.Issue
	raw := `{"key":"T-1","fields":{"summary":"radio test",
		"attachment":[{"filename":"log.txt","mimeType":"text/plain","content":"u/log"},
		              {"filename":"photo","mimeType":"image/png","content":"u/photo"}],
		"customfield_7":{"steps":[{"index":1,"fields":{"Action":"a"},
			"attachments":[{"id":3,"fileName":"setup.png","fileURL":"u/setup"}]}]}}}`
	if err := json.Unmarshal([]byte(raw), &is); err != nil {
		t.Fatal(err)
	}
	var run map[string]any
	json.Unmarshal([]byte(`{"id":5001,"status":"FAIL","evidences":[{"id":8,"fileName":"run.jpg","fileURL":"u/run"}],
		"steps":[{"index":1,"status":"PASS","evidences":[]},
		         {"index":6,"status":"FAIL","evidences":[{"id":9,"fileName":"scope.png","fileURL":"u/scope"},
		                                                 {"id":10,"fileName":"notes.pdf","fileURL":"u/notes"}]}]}`), &run)
	is.XrayRuns = []map[string]any{run}
	return &is
}

func TestIssueImageRefsOrder(t *testing.T) {
	var got []string
	for _, r := range issueImageRefs(testIssue(t)) {
		got = append(got, r.title)
	}
	want := []string{"Step 6 (FAIL) evidence: scope.png", "Test run evidence: run.jpg",
		"Step 1 attachment: setup.png", "Attachment: photo"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestDetailViewOpensImages(t *testing.T) {
	is := testIssue(t)
	pngData := pngBytes(t)
	src := &imageSource{files: map[string][]byte{"u/scope": pngData, "u/setup": pngData, "u/photo": pngData, "u/run": []byte("not an image")}}
	m := newModel(context.Background(), Options{Source: src, Issues: []jira.Issue{*is}}, plainRenderer)
	m.mode, m.detailKey = modeDetail, is.Key
	m.cache[is.Key] = is

	// No image support: a message, nothing downloaded.
	next, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	if cmd != nil || !strings.Contains(next.(model).status, "no images") {
		t.Fatalf("without image support: cmd %v, status %q", cmd != nil, next.(model).status)
	}

	m.gfx = &graphics{caps: termimg.Caps{Preview: termimg.Kitty}}
	next, cmd = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	if cmd == nil || !strings.Contains(next.(model).status, "Loading 4 image(s)") {
		t.Fatalf("with image support: cmd %v, status %q", cmd != nil, next.(model).status)
	}
	msg := m.loadIssueImages(is.Key, issueImageRefs(is))().(issueImagesMsg)
	if len(msg.items) != 3 || msg.failed != 1 || msg.items[0].Title != "Step 6 (FAIL) evidence: scope.png" {
		t.Fatalf("loaded %d, failed %d: %+v", len(msg.items), msg.failed, msg.items)
	}
	next, cmd = m.Update(msg)
	if cmd == nil {
		t.Fatal("loaded images should open the preview")
	}
	got := next.(model)
	if len(got.imgCache[is.Key]) != 3 || !strings.Contains(got.status, "1 image(s) could not be loaded") {
		t.Errorf("cache %d, status %q", len(got.imgCache[is.Key]), got.status)
	}
	// Again: straight from the cache, no new download.
	if _, cmd := got.previewIssueImages(); cmd == nil || len(got.imgCache[is.Key]) != 3 {
		t.Error("second time should open the cached images")
	}
}

func TestNoImagesOnIssue(t *testing.T) {
	is := &jira.Issue{Key: "T-2"}
	m := newModel(context.Background(), Options{Source: &imageSource{}, Issues: []jira.Issue{*is}}, plainRenderer)
	m.mode, m.detailKey, m.gfx = modeDetail, is.Key, &graphics{caps: termimg.Caps{Preview: termimg.Kitty}}
	m.cache[is.Key] = is
	next, cmd := m.previewIssueImages()
	if cmd != nil || !strings.Contains(next.(model).status, "No images on T-2") {
		t.Errorf("cmd %v, status %q", cmd != nil, next.(model).status)
	}
}
