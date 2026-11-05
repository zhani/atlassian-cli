package mirrorsync

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Embed maps a document a page embeds by URL (a Widget Connector showing a
// Google Slides deck, say) to a local copy of it. The export attaches the
// file to the page and shows it in place of the embed, so the mirror has
// the content even though the embedded site is out of reach offline.
type Embed struct {
	// URL is the embedded URL or any part of it, such as a Google document
	// ID. Query, fragment and a trailing /edit, /view, /preview, /pub or
	// /embed are ignored, so any link to the document will do.
	URL string `yaml:"url"`
	// File is the local copy: a PDF is shown as a slide show (the viewpdf
	// macro), an image as an image, anything else as a link to it. "~/" and
	// paths relative to the embeds file are resolved.
	File string `yaml:"file"`
}

// DefaultEmbedsPath is ATLASSIAN_EMBEDS, or embeds.yaml in the user's config
// directory (~/.config/atlassian-cli on Linux).
func DefaultEmbedsPath() (string, error) {
	if p := os.Getenv("ATLASSIAN_EMBEDS"); p != "" {
		return p, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "atlassian-cli", "embeds.yaml"), nil
}

// LoadEmbeds reads an embeds file: a YAML list of {url, file}. A missing
// file is an error only when required (named on the command line).
func LoadEmbeds(path string, required bool) ([]Embed, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) && !required {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var embeds []Embed
	if err := yaml.Unmarshal(data, &embeds); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	home, _ := os.UserHomeDir()
	for i := range embeds {
		e := &embeds[i]
		if e.URL == "" || e.File == "" {
			return nil, fmt.Errorf("%s: entry %d needs both url and file", path, i+1)
		}
		if strings.HasPrefix(e.File, "~/") && home != "" {
			e.File = filepath.Join(home, e.File[2:])
		}
		if !filepath.IsAbs(e.File) {
			e.File = filepath.Join(filepath.Dir(path), e.File)
		}
	}
	return embeds, nil
}

// embedKey is the part of a URL an embed matches on.
func embedKey(u string) string {
	u = strings.TrimSpace(u)
	if i := strings.IndexAny(u, "?#"); i >= 0 {
		u = u[:i]
	}
	u = strings.TrimRight(u, "/")
	for _, s := range []string{"/edit", "/view", "/preview", "/pub", "/embed"} {
		u = strings.TrimSuffix(u, s)
	}
	return u
}

// match returns the embed for an embedded URL, or nil.
func matchEmbed(embeds []Embed, u string) *Embed {
	u = embedKey(u)
	for i := range embeds {
		if k := embedKey(embeds[i].URL); k != "" && strings.Contains(u, k) {
			return &embeds[i]
		}
	}
	return nil
}

var (
	macroStartRe = regexp.MustCompile(`<ac:structured-macro\b[^>]*>`)
	macroNameRe  = regexp.MustCompile(`\bac:name="([^"]*)"`)
	urlParamRe   = regexp.MustCompile(`(?s)<ac:parameter\s+ac:name="url"\s*>(.*?)</ac:parameter>`)
	riURLValueRe = regexp.MustCompile(`ri:value="([^"]*)"`)
)

const macroEnd = "</ac:structured-macro>"

// embedUse is an embed applied to a page: the attachment title it is shown
// under.
type embedUse struct {
	embed *Embed
	title string
}

// applyEmbeds replaces each macro whose url parameter matches an embed by
// the embed's local file, attached under its base name: a viewpdf macro for
// a PDF, an image for an image, else a link. It returns the new body, the
// embeds used (each once), and the URLs of Widget Connector macros nothing
// matched. Only innermost macros are looked at, so a panel holding a widget
// is kept and only the widget is replaced.
func applyEmbeds(body string, embeds []Embed) (string, []embedUse, []string) {
	var out strings.Builder
	var uses []embedUse
	var unmapped []string
	used := map[*Embed]bool{}
	pos := 0
	for _, loc := range macroStartRe.FindAllStringIndex(body, -1) {
		start, tagEnd := loc[0], loc[1]
		if start < pos || strings.HasSuffix(body[start:tagEnd], "/>") {
			continue
		}
		end := strings.Index(body[tagEnd:], macroEnd)
		if end < 0 {
			break
		}
		end += tagEnd + len(macroEnd)
		if macroStartRe.MatchString(body[tagEnd : end-len(macroEnd)]) {
			continue // not innermost: the macros inside come next
		}
		name := ""
		if m := macroNameRe.FindStringSubmatch(body[start:tagEnd]); m != nil {
			name = m[1]
		}
		u := macroURL(body[start:end])
		if u == "" {
			continue
		}
		e := matchEmbed(embeds, u)
		if e == nil {
			if name == "widget" {
				unmapped = append(unmapped, u)
			}
			continue
		}
		title := filepath.Base(e.File)
		if !used[e] {
			used[e] = true
			uses = append(uses, embedUse{embed: e, title: title})
		}
		out.WriteString(body[pos:start])
		out.WriteString(embedStorage(title, u))
		pos = end
	}
	if pos == 0 {
		return body, uses, unmapped
	}
	out.WriteString(body[pos:])
	return out.String(), uses, unmapped
}

// macroURL is the url parameter of a macro: an ri:url or plain text.
func macroURL(macro string) string {
	m := urlParamRe.FindStringSubmatch(macro)
	if m == nil {
		return ""
	}
	if v := riURLValueRe.FindStringSubmatch(m[1]); v != nil {
		return html.UnescapeString(v[1])
	}
	return html.UnescapeString(strings.TrimSpace(m[1]))
}

// embedStorage is the storage format showing an attached copy of the
// document at url.
func embedStorage(title, u string) string {
	att := `<ri:attachment ri:filename="` + html.EscapeString(title) + `" />`
	switch strings.ToLower(filepath.Ext(title)) {
	case ".pdf":
		// Confluence's own PDF macro; embedded-url records what it replaces.
		return `<ac:structured-macro ac:name="viewpdf"><ac:parameter ac:name="name">` + att +
			`</ac:parameter><ac:parameter ac:name="embedded-url">` + html.EscapeString(u) +
			`</ac:parameter></ac:structured-macro>`
	case ".png", ".jpg", ".jpeg", ".gif":
		return `<ac:image>` + att + `</ac:image>`
	default:
		return `<ac:link>` + att + `<ac:plain-text-link-body><![CDATA[` + title + `]]></ac:plain-text-link-body></ac:link>`
	}
}

// embedAttachment is the attachment entry for an embedded file of a page,
// in the shape Confluence lists attachments.
func embedAttachment(pageID, id, title string, size int64) map[string]any {
	mt := map[string]string{".pdf": "application/pdf", ".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif"}[strings.ToLower(filepath.Ext(title))]
	if mt == "" {
		mt = "application/octet-stream"
	}
	return map[string]any{
		"id": id, "type": "attachment", "status": "current", "title": title,
		"mediaType": mt, "fileSize": size,
		"extensions": map[string]any{"mediaType": mt, "fileSize": size, "comment": "Local copy of an embedded document (mirror export)"},
		"_links":     map[string]any{"download": "/download/attachments/" + pageID + "/" + url.PathEscape(title)},
	}
}

// embedID is the attachment ID of an embedded file: by content, so
// re-exports keep it.
func embedID(data []byte) string {
	sum := sha256.Sum256(data)
	return "embed-" + hex.EncodeToString(sum[:8])
}

// attachEmbeds rewrites a page's body with the embeds and adds their files
// to its attachments (stored under dir/files). An attachment of the same
// title already on the page is left as it is and shown instead.
func (e *Exporter) attachEmbeds(id, title, dir string, p map[string]any, atts []map[string]any) []map[string]any {
	storage := obj(obj(p, "body"), "storage")
	body, _ := storage["value"].(string)
	if body == "" {
		return atts
	}
	nb, uses, unmapped := applyEmbeds(body, e.opts.Embeds)
	for _, u := range unmapped {
		e.warn("page %s (%s) embeds %s: no local copy mapped (add it to the embeds file, --embeds)", id, title, u)
	}
	if len(uses) == 0 {
		return atts
	}
	have := map[string]bool{}
	for _, a := range atts {
		have[fmt.Sprint(a["title"])] = true
	}
	for _, u := range uses {
		if have[u.title] {
			continue
		}
		data, err := os.ReadFile(u.embed.File)
		if err != nil {
			e.warn("page %s (%s): embedded copy %v", id, title, err)
			return atts // keep the page as it is rather than point at a missing file
		}
		aid := embedID(data)
		if err := os.MkdirAll(filepath.Join(dir, "files"), 0o755); err != nil {
			e.warn("page %s: %v", id, err)
			return atts
		}
		if err := os.WriteFile(filepath.Join(dir, "files", aid), data, 0o644); err != nil {
			e.warn("page %s: %v", id, err)
			return atts
		}
		atts = append(atts, embedAttachment(id, aid, u.title, int64(len(data))))
		have[u.title] = true
		e.stats.attachments++
		e.logf("page %s (%s): embedded %s attached", id, title, u.title)
	}
	storage["value"] = nb
	return atts
}
