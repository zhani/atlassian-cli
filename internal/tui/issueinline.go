package tui

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/zhani/atlassian-cli/internal/jira"
	"github.com/zhani/atlassian-cli/internal/termimg"
)

// Images drawn in place in the issue detail view, as the Confluence page
// browser does (pageimages.go): Xray evidence under each step, step
// attachments, image attachments and images in the description. The issue
// markdown embeds them, extractImages turns them into markers, and
// placeImages draws them (kitty placeholders or half blocks) after glamour.

// issueInline is the prepared detail markdown of one loaded issue and its
// images. A new one replaces it when the issue is loaded again, so results
// for the old one can be told apart (by pointer) and dropped.
type issueInline struct {
	issue *jira.Issue
	md    string // markdown with image markers
	imgs  []*pageImage
}

type (
	inlineImageMsg struct {
		key string
		in  *issueInline
		idx int
		img *termimg.Image
		err error
	}
	inlineKittyMsg struct {
		key        string
		in         *issueInline
		idx        int
		id         uint32
		cols, rows int
		err        error
	}
)

// newGraphics detects what the terminal can draw. It must run before the
// TUI reads input (it queries the terminal); on failure there are captions
// only.
func newGraphics() *graphics {
	caps, err := termimg.Detect()
	if err != nil {
		caps = termimg.Caps{}
	}
	cellW, cellH := termimg.CellSize()
	return &graphics{caps: caps, out: termimg.NewOutput(os.Stdout), cellW: cellW, cellH: cellH}
}

// inlineImages reports whether the detail view draws images in place.
func (m model) inlineImages() bool { return m.gfx != nil && m.gfx.caps.InPage != termimg.Off }

// prepareInline returns the prepared markdown and images of an issue,
// building them when the issue is new or was loaded again.
func (m *model) prepareInline(issue *jira.Issue) *issueInline {
	if in := m.inline[issue.Key]; in != nil && in.issue == issue {
		return in
	}
	m.dropInline(issue.Key)
	md, imgs := extractImages(issueMarkdownWith(issue, m.opts.DetailFields, true))
	in := &issueInline{issue: issue, md: md, imgs: imgs}
	m.inline[issue.Key] = in
	return in
}

// dropInline forgets an issue's images, deleting them from kitty.
func (m *model) dropInline(key string) {
	in := m.inline[key]
	if in == nil {
		return
	}
	if m.gfx.kitty() {
		for _, im := range in.imgs {
			if im.kittyID != 0 {
				m.gfx.out.WriteSeqs(m.gfx.caps.Wrap(termimg.KittyDelete(im.kittyID), false)) //nolint:errcheck
			}
		}
	}
	delete(m.inline, key)
}

// clearInline drops every issue's images (refresh, exit).
func (m *model) clearInline() {
	for key := range m.inline {
		m.dropInline(key)
	}
}

// loadInline downloads and decodes the images of a loaded issue in the
// background, one command per image.
func (m *model) loadInline(key string) tea.Cmd {
	issue := m.cache[key]
	dl, ok := m.opts.Source.(attachmentSource)
	if !m.inlineImages() || issue == nil || !ok {
		return nil
	}
	in := m.prepareInline(issue)
	base := siteBase(m.opts.Source)
	var cmds []tea.Cmd
	for i, im := range in.imgs {
		if i >= maxPageImages || im.loaded {
			continue
		}
		ctx, idx, src := m.ctx, i, im.src
		cmds = append(cmds, func() tea.Msg {
			img, err := fetchInline(ctx, dl, base, src)
			return inlineImageMsg{key: key, in: in, idx: idx, img: img, err: err}
		})
	}
	return tea.Batch(cmds...)
}

// siteBase is the Jira site's base URL, from the source's issue URLs.
func siteBase(src Source) string {
	return strings.TrimSuffix(src.IssueURL("X-1"), "/browse/X-1")
}

// fetchInline downloads an image: site URLs (relative, or under the site's
// base) through the authenticated client, other URLs without credentials so
// no token goes to another host.
func fetchInline(ctx context.Context, dl attachmentSource, base, src string) (*termimg.Image, error) {
	onSite := strings.HasPrefix(src, "/") || base != "" && strings.HasPrefix(src, base+"/")
	if onSite {
		return fetchImage(ctx, dl, src)
	}
	if !strings.HasPrefix(src, "http://") && !strings.HasPrefix(src, "https://") {
		return nil, errUnsupportedImage
	}
	return fetchImage(ctx, plainFetcher{}, src)
}

type plainFetcher struct{}

func (plainFetcher) DownloadAttachment(ctx context.Context, u string) (io.ReadCloser, error) {
	return fetchURL(ctx, u)
}

var errUnsupportedImage = errors.New("unsupported image source")

// handleInlineImage stores a downloaded image, redraws the view if it shows
// the issue, and sends the image to kitty.
func (m model) handleInlineImage(msg inlineImageMsg) (tea.Model, tea.Cmd) {
	if m.inline[msg.key] != msg.in || msg.idx >= len(msg.in.imgs) {
		return m, nil // the issue was reloaded meanwhile
	}
	im := msg.in.imgs[msg.idx]
	im.img, im.err, im.loaded = msg.img, msg.err, true
	m.redrawIfShown(msg.key)
	key, in, idx := msg.key, msg.in, msg.idx
	return m, kittyTransmitCmd(m.gfx, im, m.detail.Width, m.detail.Height, func(id uint32, cols, rows int, err error) tea.Msg {
		return inlineKittyMsg{key: key, in: in, idx: idx, id: id, cols: cols, rows: rows, err: err}
	})
}

func (m model) handleInlineKitty(msg inlineKittyMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		return m, nil
	}
	if m.inline[msg.key] != msg.in || msg.idx >= len(msg.in.imgs) {
		if m.gfx.kitty() { // stale: don't leave the image in kitty
			m.gfx.out.WriteSeqs(m.gfx.caps.Wrap(termimg.KittyDelete(msg.id), false)) //nolint:errcheck
		}
		return m, nil
	}
	im := msg.in.imgs[msg.idx]
	im.kittyID, im.placed = msg.id, [2]int{msg.cols, msg.rows}
	m.redrawIfShown(msg.key)
	return m, nil
}

func (m *model) redrawIfShown(key string) {
	if m.mode == modeDetail && m.detailKey == key {
		m.refreshDetailContent()
	}
}

// markdownImageOf is a markdown image the image extractor accepts: no
// whitespace or parentheses in the URL, no bracket in the name.
func markdownImageOf(name, u string) string {
	u = strings.NewReplacer(" ", "%20", "(", "%28", ")", "%29").Replace(u)
	name = strings.NewReplacer("[", "(", "]", ")").Replace(name)
	return "![" + name + "](" + u + ")"
}
