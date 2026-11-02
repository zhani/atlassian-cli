package tui

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/zhani/atlassian-cli/internal/confluence"
	"github.com/zhani/atlassian-cli/internal/httpclient"
	"github.com/zhani/atlassian-cli/internal/termimg"
)

// ImageSource is implemented by page sources that can download images
// (*confluence.Client); without it pages show image captions only.
type ImageSource interface {
	GetAttachments(ctx context.Context, pageID string) ([]confluence.Attachment, error)
	DownloadAttachment(ctx context.Context, downloadPath string) (io.ReadCloser, error)
	BaseURL() string
}

// maxImageBytes caps the size of a downloaded image.
const maxImageBytes = 25 << 20

// maxPageImages caps how many images of a page are downloaded.
const maxPageImages = 40

// graphics holds what the page browser needs to draw images.
type graphics struct {
	caps         termimg.Caps
	out          *termimg.Output // nil: escape sequences cannot be sent (tests)
	cellW, cellH int
}

// kitty reports whether images are drawn in place with kitty placeholders.
func (g *graphics) kitty() bool { return g != nil && g.out != nil && g.caps.InPage == termimg.Kitty }

type pageImage struct {
	name, src string

	img     *termimg.Image
	err     error
	loaded  bool
	kittyID uint32
	placed  [2]int // cols, rows of the kitty placement

	blocks     []string // cached half-block rendering
	blocksSize [2]int
}

type (
	attachmentsMsg struct {
		pageID string
		atts   []confluence.Attachment
		err    error
	}
	imageLoadedMsg struct {
		pageID string
		idx    int
		img    *termimg.Image
		err    error
	}
)

var markdownImage = regexp.MustCompile(`!\[([^\]]*)\]\(([^)\s]*)\)`)

// imageMarker stands for image n in markdown sent through glamour and is
// replaced by the image (or its caption) afterwards.
var imageMarker = regexp.MustCompile(`@@img(\d+)@@`)

// extractImages replaces markdown images by markers on lines of their own
// (or by a text caption inside tables) and returns the images in order.
func extractImages(md string) (string, []*pageImage) {
	var imgs []*pageImage
	lines := strings.Split(md, "\n")
	for i, line := range lines {
		inTable := strings.HasPrefix(strings.TrimSpace(line), "|")
		lines[i] = markdownImage.ReplaceAllStringFunc(line, func(s string) string {
			m := markdownImage.FindStringSubmatch(s)
			name := m[2]
			if u, err := url.Parse(m[2]); err == nil && u.Path != "" {
				name = u.Path[strings.LastIndex(u.Path, "/")+1:]
			}
			if m[1] != "" {
				name = m[1]
			}
			if name == "" {
				name = "image"
			}
			imgs = append(imgs, &pageImage{name: name, src: m[2]})
			n := len(imgs) - 1
			if inTable {
				return fmt.Sprintf("[image %d: %s]", n+1, escapeTableText(name))
			}
			return fmt.Sprintf("\n\n@@img%d@@\n\n", n)
		})
	}
	return strings.Join(lines, "\n"), imgs
}

func escapeTableText(s string) string { return strings.ReplaceAll(s, "|", "\\|") }

// isAttachmentRef reports whether an image source names an attachment of
// the page (a bare filename) rather than a URL.
func isAttachmentRef(src string) bool {
	return src != "" && !strings.Contains(src, "/") && !strings.Contains(src, ":")
}

// loadImages starts downloading the images of a page: attachments need the
// page's attachment list first.
func (m treeModel) loadImages(pageID string) tea.Cmd {
	src, ok := m.opts.Source.(ImageSource)
	imgs := m.images[pageID]
	if !ok || len(imgs) == 0 || m.gfx == nil || m.gfx.caps.InPage == termimg.Off && m.gfx.caps.Preview == termimg.Off {
		return nil
	}
	for _, im := range imgs {
		if isAttachmentRef(im.src) {
			ctx := m.ctx
			return func() tea.Msg {
				atts, err := src.GetAttachments(ctx, pageID)
				return attachmentsMsg{pageID: pageID, atts: atts, err: err}
			}
		}
	}
	return m.fetchImages(pageID, nil)
}

// fetchImages downloads and decodes each image of a page in the background.
func (m treeModel) fetchImages(pageID string, atts []confluence.Attachment) tea.Cmd {
	src := m.opts.Source.(ImageSource)
	byName := map[string]string{}
	for _, a := range atts {
		byName[a.Title] = a.Links["download"]
	}
	var cmds []tea.Cmd
	for i, im := range m.images[pageID] {
		if i >= maxPageImages {
			break
		}
		ctx, idx, ref := m.ctx, i, im.src
		cmds = append(cmds, func() tea.Msg {
			data, err := downloadImage(ctx, src, ref, byName)
			if err != nil {
				return imageLoadedMsg{pageID: pageID, idx: idx, err: err}
			}
			img, err := termimg.Decode(data)
			return imageLoadedMsg{pageID: pageID, idx: idx, img: img, err: err}
		})
	}
	return tea.Batch(cmds...)
}

// downloadImage fetches an attachment by filename or an image URL: URLs on
// the Confluence site go through the authenticated client.
func downloadImage(ctx context.Context, src ImageSource, ref string, attachments map[string]string) ([]byte, error) {
	var body io.ReadCloser
	var err error
	base := strings.TrimRight(src.BaseURL(), "/")
	switch {
	case isAttachmentRef(ref):
		link, ok := attachments[ref]
		if !ok || link == "" {
			return nil, fmt.Errorf("attachment not found")
		}
		body, err = src.DownloadAttachment(ctx, link)
	case strings.HasPrefix(ref, "/"):
		body, err = src.DownloadAttachment(ctx, ref)
	case base != "" && strings.HasPrefix(ref, base+"/"):
		body, err = src.DownloadAttachment(ctx, strings.TrimPrefix(ref, base))
	case strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://"):
		body, err = fetchURL(ctx, ref)
	default:
		return nil, fmt.Errorf("unsupported image source")
	}
	if err != nil {
		return nil, err
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, maxImageBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxImageBytes {
		return nil, fmt.Errorf("image larger than %d MB", maxImageBytes>>20)
	}
	return data, nil
}

func fetchURL(ctx context.Context, u string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	// Same transport as the API clients: proxy settings and ATLASSIAN_CA_FILE.
	resp, err := (&http.Client{Timeout: 20 * time.Second, Transport: httpclient.Transport()}).Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		resp.Body.Close()
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return resp.Body, nil
}

// imageBox is the cell box an image gets in the page view.
func (m treeModel) imageBox(im *pageImage) (cols, rows int) {
	return imageBox(m.gfx, im, m.content.Width, m.content.Height)
}

// imageBox is the cell box an image gets in a text view of viewW×viewH
// cells (page or issue view).
func imageBox(g *graphics, im *pageImage, viewW, viewH int) (cols, rows int) {
	w, h := im.img.Size()
	cellW, cellH := 8, 16
	if g != nil {
		cellW, cellH = g.cellW, g.cellH
	}
	maxCols := max(4, viewW-6)
	maxRows := max(4, viewH-3)
	if g.kitty() {
		maxCols, maxRows = min(maxCols, termimg.MaxKittyCells), min(maxRows, termimg.MaxKittyCells)
		return termimg.Fit(w, h, maxCols, maxRows, cellW, cellH)
	}
	// Half blocks have two pixels per cell, so they need more cells than
	// the image's own size for any detail: allow 2×2 source pixels per block
	// pixel, keeping the cell aspect ratio.
	return termimg.Fit(w, h, maxCols, maxRows, 2, max(2, 2*cellH/max(1, cellW)))
}

// placeImages replaces the image markers of rendered page text by the
// images (or captions) and returns the line of each image.
func (m treeModel) placeImages(rendered string, imgs []*pageImage) (string, []int) {
	return placeImages(m.gfx, rendered, imgs, m.content.Width, m.content.Height)
}

// placeImages replaces the image markers of rendered text by the images (or
// captions) for a view of viewW×viewH cells; it returns each image's line.
func placeImages(g *graphics, rendered string, imgs []*pageImage, viewW, viewH int) (string, []int) {
	if len(imgs) == 0 {
		return rendered, nil
	}
	lines := strings.Split(rendered, "\n")
	out := make([]string, 0, len(lines))
	where := make([]int, len(imgs))
	for i := range where {
		where[i] = -1
	}
	for _, line := range lines {
		mm := imageMarker.FindStringSubmatch(line)
		if mm == nil {
			out = append(out, line)
			continue
		}
		n, _ := strconv.Atoi(mm[1])
		if n >= len(imgs) {
			out = append(out, line)
			continue
		}
		im := imgs[n]
		where[n] = len(out)
		out = append(out, imageCaption(g, n, im))
		out = append(out, imageLines(g, im, viewW, viewH)...)
	}
	return strings.Join(out, "\n"), where
}

func imageCaption(g *graphics, n int, im *pageImage) string {
	caption := fmt.Sprintf("  ▣ image %d: %s", n+1, im.name)
	inPage := g != nil && g.caps.InPage != termimg.Off
	switch {
	case g == nil || g.caps.InPage == termimg.Off && g.caps.Preview == termimg.Off:
	case n >= maxPageImages:
		caption += " (not loaded)"
	case im.err != nil:
		caption += " (" + im.err.Error() + ")"
	case !im.loaded:
		caption += " (loading…)"
	case !inPage:
		caption += " (press i to view)"
	}
	return styleDim.Render(caption)
}

// imageLines draws a loaded image for a view of viewW×viewH cells.
func imageLines(g *graphics, im *pageImage, viewW, viewH int) []string {
	if im.img == nil || g == nil || g.caps.InPage == termimg.Off {
		return nil
	}
	cols, rows := imageBox(g, im, viewW, viewH)
	if cols <= 0 || rows <= 0 {
		return nil
	}
	var lines []string
	if g.kitty() && im.kittyID != 0 {
		if im.placed != [2]int{cols, rows} {
			g.out.WriteSeqs(g.caps.Wrap(termimg.KittyPlace(im.kittyID, 1, cols, rows), false)) //nolint:errcheck
			im.placed = [2]int{cols, rows}
		}
		lines = termimg.KittyPlaceholders(im.kittyID, cols, rows)
	} else {
		if im.blocksSize != [2]int{cols, rows} {
			im.blocks, im.blocksSize = termimg.HalfBlocks(im.img, cols, rows), [2]int{cols, rows}
		}
		lines = im.blocks
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = "  " + l
	}
	return out
}

// transmitKitty sends a loaded image to kitty in the background.
func (m treeModel) transmitKitty(pageID string, idx int, im *pageImage) tea.Cmd {
	return kittyTransmitCmd(m.gfx, im, m.content.Width, m.content.Height, func(id uint32, cols, rows int, err error) tea.Msg {
		return kittyReadyMsg{pageID: pageID, idx: idx, id: id, cols: cols, rows: rows, err: err}
	})
}

// kittyTransmitCmd sends a loaded image to kitty in the background; done
// builds the message reporting its ID and placement size.
func kittyTransmitCmd(g *graphics, im *pageImage, viewW, viewH int, done func(id uint32, cols, rows int, err error) tea.Msg) tea.Cmd {
	if !g.kitty() || im.img == nil {
		return nil
	}
	id := termimg.NewKittyID()
	cols, rows := imageBox(g, im, viewW, viewH)
	return func() tea.Msg {
		var seqs []string
		for _, s := range termimg.KittyTransmit(id, im.img.PNG, cols, rows) {
			seqs = append(seqs, g.caps.Wrap(s, false))
		}
		return done(id, cols, rows, g.out.WriteSeqs(seqs...))
	}
}

type kittyReadyMsg struct {
	pageID     string
	idx        int
	id         uint32
	cols, rows int
	err        error
}

// previewImages opens the full-screen preview at the first loaded image at
// or below the top of the page view.
func (m treeModel) previewImages() (treeModel, tea.Cmd) {
	imgs := m.images[m.shownID]
	if len(imgs) == 0 {
		m.setStatus("No images on this page", false)
		return m, nil
	}
	if m.gfx == nil || m.gfx.caps.Preview == termimg.Off {
		m.setStatus("Images are turned off (ATLASSIAN_IMAGES)", false)
		return m, nil
	}
	p := &termimg.Preview{Caps: m.gfx.caps}
	top, start := m.content.YOffset, -1
	for i, im := range imgs {
		if im.img == nil {
			continue
		}
		if start < 0 && i < len(m.imageAt) && m.imageAt[i] >= top {
			start = len(p.Items)
		}
		p.Items = append(p.Items, termimg.PreviewItem{Title: im.name, Image: im.img})
	}
	p.Index = max(0, start)
	if len(p.Items) == 0 {
		m.setStatus("Images are still loading", false)
		return m, nil
	}
	return m, tea.Exec(p, func(err error) tea.Msg { return previewDoneMsg{err: err} })
}

type previewDoneMsg struct{ err error }
