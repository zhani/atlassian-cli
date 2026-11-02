package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/zhani/atlassian-cli/internal/jira"
	"github.com/zhani/atlassian-cli/internal/termimg"
	"github.com/zhani/atlassian-cli/internal/xray"
)

// issueImagesKey opens the images of the issue in the detail view: Xray
// evidence (the screenshots a tester attached to a step), step attachments,
// then image attachments.
var issueImagesKey = key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "view images"))

// attachmentSource is implemented by sources that can download attachment
// and evidence files (*jira.Client, also through an offline mirror).
type attachmentSource interface {
	DownloadAttachment(ctx context.Context, contentURL string) (io.ReadCloser, error)
}

type issueImage struct{ title, url string }

type issueImagesMsg struct {
	key    string
	items  []termimg.PreviewItem
	failed int
	err    error
}

// imageExts are the formats termimg can decode.
var imageExts = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true}

func isImage(name, mime string) bool {
	return imageExts[strings.ToLower(filepath.Ext(name))] ||
		mime == "image/png" || mime == "image/jpeg" || mime == "image/gif"
}

// issueImageRefs lists an issue's images in viewing order: the evidence of
// its Xray test runs step by step, run-level evidence, the manual steps'
// attachments, then the issue's image attachments. Each URL once.
func issueImageRefs(issue *jira.Issue) []issueImage {
	var out []issueImage
	seen := map[string]bool{}
	add := func(title, u, name, mime string) {
		if u != "" && !seen[u] && isImage(name, mime) {
			seen[u] = true
			out = append(out, issueImage{title, u})
		}
	}
	for _, run := range issue.XrayRuns {
		steps, _ := run["steps"].([]any)
		for i, s := range steps {
			sm, _ := s.(map[string]any)
			idx := i + 1
			if v, ok := sm["index"].(float64); ok && v > 0 {
				idx = int(v)
			}
			status := strings.ToUpper(fmt.Sprint(sm["status"]))
			for _, f := range xray.Files(sm["evidences"]) {
				add(fmt.Sprintf("Step %d (%s) evidence: %s", idx, status, f.Name), f.URL, f.Name, "")
			}
		}
		for _, f := range xray.Files(run["evidences"]) {
			add("Test run evidence: "+f.Name, f.URL, f.Name, "")
		}
	}
	for _, st := range xray.ManualSteps(issue.Fields.Custom) {
		for _, f := range st.Files {
			add(fmt.Sprintf("Step %d attachment: %s", st.Index, f.Name), f.URL, f.Name, "")
		}
	}
	for _, a := range issue.Fields.Attachment {
		add("Attachment: "+a.Filename, a.Content, a.Filename, a.MimeType)
	}
	return out
}

// previewIssueImages opens the full-screen preview of the detail view's
// issue, downloading its images first (once per issue).
func (m model) previewIssueImages() (tea.Model, tea.Cmd) {
	if m.gfx == nil || m.gfx.caps.Preview == termimg.Off {
		m.setStatus("This terminal shows no images (see ATLASSIAN_IMAGES)", false)
		return m, nil
	}
	issue := m.cache[m.detailKey]
	if issue == nil {
		m.setStatus("Still loading "+m.detailKey, false)
		return m, nil
	}
	if items := m.imgCache[issue.Key]; len(items) > 0 {
		return m, m.openPreview(items)
	}
	refs := issueImageRefs(issue)
	if len(refs) == 0 {
		m.setStatus("No images on "+issue.Key, false)
		return m, nil
	}
	m.setStatus(fmt.Sprintf("Loading %d image(s)…", len(refs)), false)
	return m, tea.Batch(m.startLoading(), m.loadIssueImages(issue.Key, refs))
}

func (m model) loadIssueImages(issueKey string, refs []issueImage) tea.Cmd {
	ctx, src := m.ctx, m.opts.Source
	return func() tea.Msg {
		dl, ok := src.(attachmentSource)
		if !ok {
			return issueImagesMsg{key: issueKey, err: errors.New("images can't be downloaded here")}
		}
		msg := issueImagesMsg{key: issueKey}
		for _, r := range refs {
			img, err := fetchImage(ctx, dl, r.url)
			if err != nil {
				msg.failed++
				continue
			}
			msg.items = append(msg.items, termimg.PreviewItem{Title: r.title, Image: img})
		}
		return msg
	}
}

func fetchImage(ctx context.Context, dl attachmentSource, u string) (*termimg.Image, error) {
	body, err := dl.DownloadAttachment(ctx, u)
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
	return termimg.Decode(data)
}

func (m model) openPreview(items []termimg.PreviewItem) tea.Cmd {
	p := &termimg.Preview{Caps: m.gfx.caps, Items: items}
	return tea.Exec(p, func(err error) tea.Msg { return previewDoneMsg{err: err} })
}

// handleIssueImages shows the downloaded images, if the issue is still the
// one in the detail view.
func (m model) handleIssueImages(msg issueImagesMsg) (tea.Model, tea.Cmd) {
	m.loading = false
	switch {
	case msg.err != nil:
		m.setStatus(msg.err.Error(), true)
		return m, nil
	case len(msg.items) == 0:
		m.setStatus(fmt.Sprintf("Could not load the %d image(s) of %s", msg.failed, msg.key), true)
		return m, nil
	}
	m.imgCache[msg.key] = msg.items
	if msg.failed > 0 {
		m.setStatus(fmt.Sprintf("%d image(s) could not be loaded", msg.failed), true)
	}
	if m.mode != modeDetail || m.detailKey != msg.key {
		return m, nil
	}
	return m, m.openPreview(msg.items)
}
