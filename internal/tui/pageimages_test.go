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
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/zhani/atlassian-cli/internal/confluence"
	"github.com/zhani/atlassian-cli/internal/embeds"
	"github.com/zhani/atlassian-cli/internal/termimg"
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

// deckPages serves a page embedding a PDF (viewpdf) whose name has spaces.
type deckPages struct{ imagePages }

func (f *deckPages) GetPage(_ context.Context, id string, _ []string) (*confluence.Page, error) {
	return &confluence.Page{ID: id, Title: "Page " + id, Body: confluence.PageBody{Storage: &confluence.BodyContent{Value: `<p>Deck</p>` +
		`<ac:structured-macro ac:name="viewpdf"><ac:parameter ac:name="name"><ri:attachment ri:filename="Intro deck.pdf" /></ac:parameter></ac:structured-macro>`}}}, nil
}

func (f *deckPages) GetAttachments(context.Context, string) ([]confluence.Attachment, error) {
	return []confluence.Attachment{{Title: "Intro deck.pdf", Links: map[string]string{"download": "/download/deck.pdf"}}}, nil
}

func (f *deckPages) DownloadAttachment(_ context.Context, path string) (io.ReadCloser, error) {
	f.record("download " + path)
	return io.NopCloser(strings.NewReader("%PDF-1.4 deck")), nil
}

func TestPageSlideShow(t *testing.T) {
	slide := func(w, h int) []byte {
		var buf bytes.Buffer
		png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h)))
		return buf.Bytes()
	}
	t.Cleanup(func() { renderSlides = pdfslidesRender })
	renderSlides = func(_ context.Context, pdf []byte) ([][]byte, error) {
		if !strings.HasPrefix(string(pdf), "%PDF") {
			t.Errorf("rendered %q", pdf)
		}
		return [][]byte{slide(64, 36), slide(32, 64), slide(48, 48)}, nil
	}
	src := &deckPages{}
	m := newTree(context.Background(), TreeOptions{Source: src, Space: "DEV"}, plainRenderer)
	m.gfx = &graphics{caps: termimg.Caps{InPage: termimg.Blocks, Preview: termimg.Blocks}, cellW: 8, cellH: 16}
	m = drainTree(m, m.Init())
	if !strings.Contains(strings.Join(src.calls, ","), "download /download/deck.pdf") {
		t.Fatalf("the PDF was not downloaded by its escaped name: %v", src.calls)
	}
	if v := m.content.View(); !strings.Contains(v, "▣ slide 1/3: Intro deck.pdf") || !strings.Contains(v, "[ ]: previous/next") {
		t.Fatalf("first slide not shown:\n%s", v)
	}
	size := func(m treeModel) [2]int {
		w, h := m.images[m.shownID][0].img.Size()
		return [2]int{w, h}
	}
	key := func(m treeModel, k string) treeModel {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
		return next.(treeModel)
	}
	m = key(m, "]")
	if v := m.content.View(); !strings.Contains(v, "▣ slide 2/3") || size(m) != [2]int{32, 64} {
		t.Errorf("] must show slide 2:\n%s", v)
	}
	m = key(key(m, "["), "[") // back past the first: wraps to the last
	if v := m.content.View(); !strings.Contains(v, "▣ slide 3/3") || size(m) != [2]int{48, 48} {
		t.Errorf("[ must wrap to slide 3:\n%s", v)
	}

	// A page without decks only reports it.
	m2, _ := openImagePage(t, &graphics{caps: termimg.Caps{InPage: termimg.Blocks, Preview: termimg.Blocks}, cellW: 8, cellH: 16})
	if s := key(m2, "]").status; s != "No slides on this page" {
		t.Errorf("status %q", s)
	}
}

// widgetPages serves a live page embedding a Google Slides deck through
// the Widget Connector; atts are the page's attachments.
type widgetPages struct {
	imagePages
	atts []confluence.Attachment
}

func (f *widgetPages) GetPage(_ context.Context, id string, _ []string) (*confluence.Page, error) {
	return &confluence.Page{ID: id, Title: "Page " + id, Body: confluence.PageBody{Storage: &confluence.BodyContent{Value: `<p>Deck</p>` +
		`<ac:structured-macro ac:name="widget"><ac:parameter ac:name="url"><ri:url ri:value="https://docs.google.com/presentation/d/DECKID/edit" /></ac:parameter></ac:structured-macro>`}}}, nil
}

func (f *widgetPages) GetAttachments(context.Context, string) ([]confluence.Attachment, error) {
	return f.atts, nil
}

func (f *widgetPages) DownloadAttachment(_ context.Context, path string) (io.ReadCloser, error) {
	f.record("download " + path)
	return io.NopCloser(strings.NewReader("%PDF-1.4 attached")), nil
}

func TestPageEmbedsLive(t *testing.T) {
	file := filepath.Join(t.TempDir(), "Intro deck.pdf")
	if err := os.WriteFile(file, []byte("%PDF-1.4 local"), 0o644); err != nil {
		t.Fatal(err)
	}
	var rendered []string
	t.Cleanup(func() { renderSlides = pdfslidesRender })
	renderSlides = func(_ context.Context, pdf []byte) ([][]byte, error) {
		rendered = append(rendered, string(pdf))
		var buf bytes.Buffer
		png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 64, 36)))
		return [][]byte{buf.Bytes(), buf.Bytes()}, nil
	}
	open := func(src *widgetPages, list []embeds.Embed) treeModel {
		m := newTree(context.Background(), TreeOptions{Source: src, Space: "DEV", Embeds: list}, plainRenderer)
		m.gfx = &graphics{caps: termimg.Caps{InPage: termimg.Blocks, Preview: termimg.Blocks}, cellW: 8, cellH: 16}
		return drainTree(m, m.Init())
	}
	list := []embeds.Embed{{URL: "DECKID", File: file}}

	src := &widgetPages{}
	m := open(src, list)
	if v := m.content.View(); !strings.Contains(v, "▣ slide 1/2: Intro deck.pdf") {
		t.Fatalf("the local copy must show as slides in place of the widget:\n%s", v)
	}
	if len(rendered) != 1 || rendered[0] != "%PDF-1.4 local" {
		t.Errorf("rendered %q, want the local file", rendered)
	}
	if strings.Contains(strings.Join(src.calls, ","), "download") {
		t.Errorf("nothing must be downloaded: %v", src.calls)
	}

	// An attachment of the same name on the page wins, as in mirror export.
	rendered = nil
	src = &widgetPages{atts: []confluence.Attachment{{Title: "Intro deck.pdf", Links: map[string]string{"download": "/download/deck.pdf"}}}}
	open(src, list)
	if len(rendered) != 1 || rendered[0] != "%PDF-1.4 attached" {
		t.Errorf("rendered %q, want the attachment", rendered)
	}

	// Without a mapping the widget stays.
	rendered = nil
	if v := open(&widgetPages{}, nil).content.View(); strings.Contains(v, "slide") || len(rendered) != 0 {
		t.Errorf("an unmapped widget must not become slides:\n%s", v)
	}
}

var pdfslidesRender = renderSlides
