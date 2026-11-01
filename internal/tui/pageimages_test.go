package tui

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/putcho01/atlassian-cli/internal/confluence"
	"github.com/putcho01/atlassian-cli/internal/termimg"
)

func TestExtractImages(t *testing.T) {
	md := "Intro ![Arch](arch.png) text\n\n| a | ![](https://x/y/chart.jpg) |\n\n![](/download/attachments/1/c.gif?version=2)"
	got, imgs := extractImages(md)
	if len(imgs) != 3 {
		t.Fatalf("got %d images", len(imgs))
	}
	names := []string{imgs[0].name, imgs[1].name, imgs[2].name}
	if strings.Join(names, ",") != "Arch,chart.jpg,c.gif" {
		t.Errorf("names = %v", names)
	}
	if imgs[1].src != "https://x/y/chart.jpg" {
		t.Errorf("src = %q", imgs[1].src)
	}
	if !strings.Contains(got, "\n\n@@img0@@\n\n") || !strings.Contains(got, "\n\n@@img2@@\n\n") {
		t.Errorf("markers missing: %q", got)
	}
	if !strings.Contains(got, "| a | [image 2: chart.jpg] |") {
		t.Errorf("table images must stay inline captions: %q", got)
	}
}

func TestIsAttachmentRef(t *testing.T) {
	for ref, want := range map[string]bool{"a.png": true, "/download/a.png": false, "https://x/a.png": false, "": false} {
		if got := isAttachmentRef(ref); got != want {
			t.Errorf("isAttachmentRef(%q) = %v", ref, got)
		}
	}
}

// imagePages serves a page with an attached image, a missing attachment and
// an image linked by a URL on the wiki.
type imagePages struct {
	fakePages
	png []byte
}

func (f *imagePages) GetPage(_ context.Context, id string, _ []string) (*confluence.Page, error) {
	f.record("page " + id)
	return &confluence.Page{ID: id, Title: "Page " + id, Body: confluence.PageBody{Storage: &confluence.BodyContent{Value: `<p>Top</p>` +
		`<ac:image><ri:attachment ri:filename="a.png" /></ac:image>` +
		`<ac:image><ri:attachment ri:filename="gone.png" /></ac:image>` +
		`<ac:image><ri:url ri:value="https://wiki/download/b.png" /></ac:image>`}}}, nil
}

func (f *imagePages) GetAttachments(_ context.Context, pageID string) ([]confluence.Attachment, error) {
	f.record("attachments " + pageID)
	return []confluence.Attachment{{Title: "a.png", Links: map[string]string{"download": "/download/a.png"}}}, nil
}

func (f *imagePages) DownloadAttachment(_ context.Context, path string) (io.ReadCloser, error) {
	f.record("download " + path)
	switch path {
	case "/download/a.png", "/download/b.png":
		return io.NopCloser(bytes.NewReader(f.png)), nil
	}
	return nil, errors.New("not found")
}

func (f *imagePages) BaseURL() string { return "https://wiki" }

func testPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 64, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 64; x++ {
			img.Set(x, y, color.RGBA{uint8(x * 4), 100, uint8(y * 8), 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// openImagePage opens page 1 of the image source in a tree with gfx.
func openImagePage(t *testing.T, gfx *graphics) (treeModel, *imagePages) {
	t.Helper()
	src := &imagePages{png: testPNG(t)}
	m := newTree(context.Background(), TreeOptions{Source: src, Space: "DEV"}, plainRenderer)
	m.gfx = gfx
	m = drainTree(m, m.Init())
	return m, src
}

func TestPageImagesAsBlocks(t *testing.T) {
	m, src := openImagePage(t, &graphics{caps: termimg.Caps{InPage: termimg.Blocks, Preview: termimg.Blocks}, cellW: 8, cellH: 16})
	view := m.content.View()
	if !strings.Contains(view, "▣ image 1: a.png") || !strings.Contains(view, "▀") {
		t.Errorf("image 1 not drawn:\n%s", view)
	}
	all := strings.Join(src.calls, ",")
	for _, want := range []string{"attachments 1", "download /download/a.png", "download /download/b.png"} {
		if !strings.Contains(all, want) {
			t.Errorf("missing call %q in %s", want, all)
		}
	}
	m.content.SetYOffset(0)
	m.content.Height = 200
	m.showContent()
	if v := m.content.View(); !strings.Contains(v, "image 2: gone.png (attachment not found)") || !strings.Contains(v, "image 3: b.png") {
		t.Errorf("captions:\n%s", v)
	}
	if len(m.imageAt) != 3 || m.imageAt[0] < 0 {
		t.Errorf("imageAt = %v", m.imageAt)
	}
}

func TestPageImagesCaptionsOnly(t *testing.T) {
	m, src := openImagePage(t, nil)
	if v := m.content.View(); !strings.Contains(v, "▣ image 1: a.png") || strings.Contains(v, "▀") {
		t.Errorf("want captions only:\n%s", v)
	}
	if strings.Contains(strings.Join(src.calls, ","), "download") {
		t.Error("images downloaded without graphics")
	}
}

func TestPageImagesKittyPlaceholders(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "tty")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gfx := &graphics{caps: termimg.Caps{InPage: termimg.Kitty, Preview: termimg.Kitty, Tmux: true, Passthrough: true}, out: termimg.NewOutput(f), cellW: 8, cellH: 16}
	m, _ := openImagePage(t, gfx)
	if v := m.content.View(); !strings.ContainsRune(v, '\U0010EEEE') {
		t.Errorf("no kitty placeholders:\n%s", v)
	}
	sent, _ := os.ReadFile(f.Name())
	if !bytes.Contains(sent, []byte("\x1bPtmux;\x1b\x1b_Ga=T,U=1,f=100")) {
		t.Errorf("image not transmitted through tmux: %.80q", sent)
	}

	// Reloading the page frees its images.
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	_ = next
	sent, _ = os.ReadFile(f.Name())
	if !bytes.Contains(sent, []byte("_Ga=d,d=I,")) {
		t.Error("images not deleted on reload")
	}
}

func TestPreviewKey(t *testing.T) {
	m, _ := openImagePage(t, &graphics{caps: termimg.Caps{InPage: termimg.Blocks, Preview: termimg.Blocks}, cellW: 8, cellH: 16})
	m.focusContent = true
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	if cmd == nil {
		t.Fatalf("no preview started; status %q", next.(treeModel).status)
	}

	// Without images the key only reports that.
	m2 := newTree(context.Background(), TreeOptions{Source: &fakePages{}, Space: "DEV"}, plainRenderer)
	m2 = drainTree(m2, m2.Init())
	next, cmd = m2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	if cmd != nil || next.(treeModel).status != "No images on this page" {
		t.Errorf("status %q", next.(treeModel).status)
	}
}
