// Package mirror serves an offline copy of Jira and Confluence data (made
// with `atlassian-cli mirror export` on a connected machine and unpacked
// with `mirror import`) over the REST endpoints the clients use, as an
// in-process http.RoundTripper. With a mirror active, the Jira and
// Confluence clients talk to JiraBase / ConfluenceBase, whose requests never
// leave the process; everything is read-only.
//
// Directory layout (the archive holds the same tree; every issue, page and
// board is a self-contained unit that an import replaces as a whole):
//
//	mirror.json                          Manifest
//	jira/myself.json                     the exporting user (currentUser())
//	jira/names.json                      field ID -> display name
//	jira/fields.json                     GET /rest/api/2/field
//	jira/linktypes.json                  GET /rest/api/2/issueLinkType
//	jira/boards/<id>.json                Board with its sprints
//	jira/issues/<KEY>/issue.json         issue with renderedFields and comments
//	jira/issues/<KEY>/attachments/<id>   attachment content
//	confluence/spaces/<KEY>.json         space
//	confluence/pages/<id>/page.json      page with body.storage, version, space, ancestors
//	confluence/pages/<id>/attachments.json  the page's attachment list
//	confluence/pages/<id>/files/<att id>    attachment content
//	confluence/pages/<id>/urls.json      site path -> file under urls/ (linked images)
//	confluence/pages/<id>/jira-queries.json  the page's issue lists (Jira issues
//	                                     macros) as Jira answered them: keys in order
//	bookmarks.yaml                       the exporter's bookmarks
//	local/                               the user's own additions, same layout;
//	                                     loaded over the mirrored data, never
//	                                     touched by import
package mirror

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Base URLs of the mirrored services. The .invalid TLD guarantees a request
// that somehow escapes the RoundTripper goes nowhere.
const (
	JiraBase       = "http://jira.mirror.invalid"
	ConfluenceBase = "http://confluence.mirror.invalid"
)

// Manifest is mirror.json.
type Manifest struct {
	Version       int      `json:"version"`
	ExportedAt    string   `json:"exported_at"`
	LastFullAt    string   `json:"last_full_at,omitempty"`
	Since         string   `json:"since"`
	ImportedAt    string   `json:"imported_at,omitempty"`
	JiraURL       string   `json:"jira_url"`
	ConfluenceURL string   `json:"confluence_url"`
	Cloud         bool     `json:"cloud"`
	Boards        []int    `json:"boards,omitempty"`
	Issues        int      `json:"issues"`
	Pages         int      `json:"pages"`
	Attachments   int      `json:"attachments"`
	Bytes         int64    `json:"bytes"`
	Settings      Settings `json:"settings"`
	// Contents names the sides (SideJira, SideConfluence) an archive covers;
	// empty means both (archives made before the sides could be split). In
	// a mirror directory it is the union of everything imported.
	Contents []string `json:"contents,omitempty"`
	// Per-side freshness in a mirror directory (import fills them in; Jira
	// changes often, Confluence rarely, so they are exported separately).
	Jira       *SideState `json:"jira,omitempty"`
	Confluence *SideState `json:"confluence,omitempty"`
}

// The sides of a mirror, exported together or separately (export --only).
const (
	SideJira       = "jira"
	SideConfluence = "confluence"
)

// Sides lists every side, in a fixed order.
var Sides = []string{SideJira, SideConfluence}

// SideState is how current one side of a mirror is.
type SideState struct {
	ExportedAt string `json:"exported_at"`
	LastFullAt string `json:"last_full_at,omitempty"`
	Since      string `json:"since,omitempty"`
}

// Covers reports whether the manifest's archive or mirror covers side.
func (m *Manifest) Covers(side string) bool {
	if len(m.Contents) == 0 {
		return true
	}
	for _, s := range m.Contents {
		if s == side {
			return true
		}
	}
	return false
}

// Side returns the state of one side (nil when it was never imported). A
// mirror from before per-side state has both sides as of its manifest.
func (m *Manifest) Side(side string) *SideState {
	if m.Jira == nil && m.Confluence == nil {
		if m.ExportedAt == "" || !m.Covers(side) {
			return nil
		}
		return &SideState{ExportedAt: m.ExportedAt, LastFullAt: m.LastFullAt, Since: m.Since}
	}
	if side == SideJira {
		return m.Jira
	}
	return m.Confluence
}

func (m *Manifest) setSide(side string, st *SideState) {
	if side == SideJira {
		m.Jira = st
	} else {
		m.Confluence = st
	}
}

// MergeImport returns the manifest of a mirror directory (old, nil when
// empty) after importing an archive with manifest in: only the sides the
// archive covers change, so a Jira-only archive keeps the Confluence URL,
// freshness and pages (and vice versa). The top-level ExportedAt is the
// newest side's, LastFullAt the oldest side's last full snapshot (empty
// while a side has none).
func MergeImport(old, in *Manifest, importedAt string) *Manifest {
	m := Manifest{}
	if old != nil {
		m = *old
		m.Jira, m.Confluence = old.Side(SideJira), old.Side(SideConfluence)
	}
	m.Version, m.ImportedAt, m.Since = in.Version, importedAt, in.Since
	full := in.Since == ""
	contents := map[string]bool{}
	if old != nil {
		for _, s := range Sides {
			if old.Covers(s) {
				contents[s] = true
			}
		}
	}
	for _, s := range Sides {
		if !in.Covers(s) {
			continue
		}
		contents[s] = true
		st := SideState{}
		if prev := m.Side(s); prev != nil {
			st = *prev
		}
		if in.ExportedAt > st.ExportedAt {
			st.ExportedAt = in.ExportedAt
		}
		st.Since = in.Since
		if full && in.ExportedAt > st.LastFullAt {
			st.LastFullAt = in.ExportedAt
		}
		m.setSide(s, &st)
		switch s {
		case SideJira:
			if in.JiraURL != "" {
				m.JiraURL, m.Cloud = in.JiraURL, in.Cloud
			}
			m.Settings = in.Settings
			if full || old == nil {
				m.Boards = in.Boards
			} else {
				m.Boards = unionInts(m.Boards, in.Boards)
			}
		case SideConfluence:
			if in.ConfluenceURL != "" {
				m.ConfluenceURL = in.ConfluenceURL
			}
		}
	}
	// The other side's URL too while the mirror has none: an archive carries
	// that side's items it references, and links are rewritten by it.
	if m.JiraURL == "" && in.JiraURL != "" {
		m.JiraURL, m.Cloud = in.JiraURL, in.Cloud
	}
	if m.ConfluenceURL == "" && in.ConfluenceURL != "" {
		m.ConfluenceURL = in.ConfluenceURL
	}
	m.Contents = nil
	for _, s := range Sides {
		if contents[s] {
			m.Contents = append(m.Contents, s)
		}
	}
	m.ExportedAt, m.LastFullAt = "", ""
	first := true
	for _, s := range m.Contents {
		st := m.Side(s)
		if st == nil {
			continue
		}
		if st.ExportedAt > m.ExportedAt {
			m.ExportedAt = st.ExportedAt
		}
		if first || st.LastFullAt < m.LastFullAt {
			m.LastFullAt = st.LastFullAt
		}
		first = false
	}
	return &m
}

func unionInts(a, b []int) []int {
	seen := map[int]bool{}
	var out []int
	for _, v := range append(append([]int{}, a...), b...) {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

// Settings are the exporter's JIRA_* explorer settings, used on the mirror
// side when the corresponding variable is unset there.
type Settings struct {
	DefaultProject   string   `json:"default_project,omitempty"`
	DefaultBoard     string   `json:"default_board,omitempty"`
	FeatureType      string   `json:"feature_type,omitempty"`
	FeatureLinkTypes []string `json:"feature_link_types,omitempty"`
	EpicLinkField    string   `json:"epic_link_field,omitempty"`
	DetailFields     []string `json:"detail_fields,omitempty"`
}

// ManifestFile is the manifest's name in a mirror directory or archive.
const ManifestFile = "mirror.json"

// DefaultDir is $ATLASSIAN_MIRROR, else ~/.local/share/atlassian-cli/mirror.
func DefaultDir() string {
	if d := os.Getenv("ATLASSIAN_MIRROR"); d != "" && !disabledValue(d) {
		return d
	}
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "atlassian-cli", "mirror")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", "atlassian-mirror")
	}
	return filepath.Join(home, ".local", "share", "atlassian-cli", "mirror")
}

func disabledValue(v string) bool {
	switch strings.ToLower(v) {
	case "off", "0", "false", "no":
		return true
	}
	return false
}

// Disabled turns mirror detection off for this process (mirror export must
// talk to the real servers).
var Disabled bool

// ActiveDir reports whether the mirror is to be used, and its directory:
// when ATLASSIAN_MIRROR names a directory, or when urlConfigured is false
// (no service URL set) and the default directory holds a mirror.
func ActiveDir(urlConfigured bool) (string, bool) {
	if Disabled {
		return "", false
	}
	if v := os.Getenv("ATLASSIAN_MIRROR"); v != "" {
		if disabledValue(v) {
			return "", false
		}
		return v, true
	}
	if urlConfigured {
		return "", false
	}
	dir := DefaultDir()
	if _, err := os.Stat(filepath.Join(dir, ManifestFile)); err == nil {
		return dir, true
	}
	return "", false
}

// ReadManifest reads dir/mirror.json.
func ReadManifest(dir string) (*Manifest, error) {
	var m Manifest
	if err := readJSON(filepath.Join(dir, ManifestFile), &m); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("no mirror in %s (run `atlassian-cli mirror import <archive>`)", dir)
		}
		return nil, err
	}
	return &m, nil
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// WriteManifest writes dir/mirror.json.
func WriteManifest(dir string, m *Manifest) error {
	return writeJSON(filepath.Join(dir, ManifestFile), m)
}

var (
	activeMu  sync.Mutex
	activeDir string
	servers   = map[string]*Server{}
)

// Activate makes dir the mirror served by Transport.
func Activate(dir string) {
	activeMu.Lock()
	activeDir = dir
	activeMu.Unlock()
}

// IsMirrorURL reports whether base is one of the mirror's base URLs.
func IsMirrorURL(base string) bool {
	base = strings.TrimRight(base, "/")
	return base == JiraBase || base == ConfluenceBase
}

// Transport returns the RoundTripper serving the active mirror. The data is
// loaded on the first request.
func Transport() http.RoundTripper {
	activeMu.Lock()
	dir := activeDir
	activeMu.Unlock()
	return roundTripper{dir: dir}
}

// ServerFor returns the (cached) server of a mirror directory.
func ServerFor(dir string) (*Server, error) {
	activeMu.Lock()
	defer activeMu.Unlock()
	if s, ok := servers[dir]; ok {
		return s, nil
	}
	s, err := Load(dir)
	if err != nil {
		return nil, err
	}
	servers[dir] = s
	return s, nil
}
