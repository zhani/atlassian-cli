// Package embeds maps documents that Confluence pages embed from other sites
// (a Widget Connector showing a Google Slides deck, say) to local copies, and
// rewrites page storage to show the copy in place of the embed. mirror export
// attaches the copies to the exported pages; the page browser reads them from
// disk when it shows a live page.
package embeds

import (
	"errors"
	"fmt"
	"html"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Embed maps a document a page embeds by URL to a local copy of it.
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

// Title is the attachment name the copy is shown under: its base name.
func (e *Embed) Title() string { return filepath.Base(e.File) }

// DefaultPath is ATLASSIAN_EMBEDS, or embeds.yaml in the user's config
// directory (~/.config/atlassian-cli on Linux).
func DefaultPath() (string, error) {
	if p := os.Getenv("ATLASSIAN_EMBEDS"); p != "" {
		return p, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "atlassian-cli", "embeds.yaml"), nil
}

// LoadDefault reads the file at DefaultPath; a missing file is no error.
func LoadDefault() ([]Embed, error) {
	path, err := DefaultPath()
	if err != nil {
		return nil, nil
	}
	return Load(path, false)
}

// Load reads an embeds file: a YAML list of {url, file}. A missing file is
// an error only when required (named on the command line).
func Load(path string, required bool) ([]Embed, error) {
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

// key is the part of a URL an embed matches on.
func key(u string) string {
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

// Match returns the embed for an embedded URL, or nil.
func Match(embeds []Embed, u string) *Embed {
	u = key(u)
	for i := range embeds {
		if k := key(embeds[i].URL); k != "" && strings.Contains(u, k) {
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

// Use is an embed applied to a page: the attachment title it is shown under.
type Use struct {
	Embed *Embed
	Title string
}

// Apply replaces each macro whose url parameter matches an embed by the
// embed's local file, as an attachment named by its base name: a viewpdf
// macro for a PDF, an image for an image, else a link. It returns the new
// body, the embeds used (each once), and the URLs of Widget Connector macros
// nothing matched. Only innermost macros are looked at, so a panel holding a
// widget is kept and only the widget is replaced.
func Apply(body string, embeds []Embed) (string, []Use, []string) {
	var out strings.Builder
	var uses []Use
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
		e := Match(embeds, u)
		if e == nil {
			if name == "widget" {
				unmapped = append(unmapped, u)
			}
			continue
		}
		title := e.Title()
		if !used[e] {
			used[e] = true
			uses = append(uses, Use{Embed: e, Title: title})
		}
		out.WriteString(body[pos:start])
		out.WriteString(storage(title, u))
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

// storage is the storage format showing an attached copy of the document at
// url.
func storage(title, u string) string {
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
