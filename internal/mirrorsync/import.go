package mirrorsync

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/zhani/atlassian-cli/internal/bookmarks"
	"github.com/zhani/atlassian-cli/internal/mirror"
	"github.com/zhani/atlassian-cli/internal/xref"
	"go.yaml.in/yaml/v3"
)

// writeTarGz packs dir into a .tar.gz at out (written to a temp file first).
func writeTarGz(dir, out string) error {
	tmp := out + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil || rel == "." {
			return err
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(rel)
		if info.IsDir() {
			hdr.Name += "/"
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		src, err := os.Open(path)
		if err != nil {
			return err
		}
		defer src.Close()
		_, err = io.Copy(tw, src)
		return err
	})
	for _, c := range []io.Closer{tw, gz, f} {
		if cerr := c.Close(); err == nil {
			err = cerr
		}
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, out)
}

// extractTarGz unpacks an archive into dir, refusing paths that leave it
// and anything but regular files and directories.
func extractTarGz(archive, dir string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("%s: not a .tar.gz mirror archive: %w", archive, err)
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		name := filepath.FromSlash(strings.TrimPrefix(hdr.Name, "./"))
		if name == "" || name == "." {
			continue
		}
		if !filepath.IsLocal(name) {
			return fmt.Errorf("archive entry %q escapes the mirror", hdr.Name)
		}
		target := filepath.Join(dir, name)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			if err := out.Close(); err != nil {
				return err
			}
		default:
			return fmt.Errorf("archive entry %q is not a regular file", hdr.Name)
		}
	}
}

// ImportOptions control Import.
type ImportOptions struct {
	Prune     bool             // remove items missing from a full snapshot
	Bookmarks *bookmarks.Store // union-merge the archive's bookmarks into this store (nil: skip)
	Log       io.Writer
}

// ImportResult summarizes an import.
type ImportResult struct {
	Dir        string           `json:"dir"`
	Full       bool             `json:"full"`
	Contents   []string         `json:"contents"` // the sides the archive covers
	ExportedAt string           `json:"exported_at"`
	New        int              `json:"new"`
	Updated    int              `json:"updated"`
	Kept       int              `json:"kept"`
	Pruned     []string         `json:"pruned"`
	Bookmarks  int              `json:"bookmarks_added"`
	Manifest   *mirror.Manifest `json:"manifest"`
}

// unitGlobs are the items an import replaces as a whole (relative to the
// mirror root), per side. Everything else is shared metadata merged separately.
var unitGlobs = map[string][]string{
	mirror.SideJira:       {"jira/issues/*", "jira/boards/*.json"},
	mirror.SideConfluence: {"confluence/pages/*", "confluence/spaces/*.json"},
}

// Import upserts an archive into dir: every issue, page, board and space in
// it replaces its old copy entirely; everything else is kept, and local/ is
// never touched. With Prune (full snapshots only) items missing from the
// archive are removed, only on the sides the archive covers (a Jira-only
// snapshot never prunes Confluence pages).
func Import(archive, dir string, opts ImportOptions) (*ImportResult, error) {
	logf := func(format string, args ...any) {
		if opts.Log != nil {
			fmt.Fprintf(opts.Log, format+"\n", args...)
		}
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	parent := filepath.Dir(dir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return nil, err
	}
	// Staged next to the mirror so items move in with a rename.
	stage, err := os.MkdirTemp(parent, ".mirror-import-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)
	if err := extractTarGz(archive, stage); err != nil {
		return nil, err
	}
	in, err := mirror.ReadManifest(stage)
	if err != nil {
		return nil, fmt.Errorf("%s is not a mirror archive (no %s)", archive, mirror.ManifestFile)
	}
	if in.Version != 1 {
		return nil, fmt.Errorf("unsupported mirror archive version %d", in.Version)
	}
	full := in.Since == ""
	if opts.Prune && !full {
		return nil, fmt.Errorf("--prune needs a full snapshot; this archive only has changes since %s", in.Since)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	old, _ := mirror.ReadManifest(dir)

	res := &ImportResult{Dir: dir, Full: full, ExportedAt: in.ExportedAt, Pruned: []string{}}
	for _, side := range mirror.Sides {
		if in.Covers(side) {
			res.Contents = append(res.Contents, side)
		}
	}
	trash := filepath.Join(dir, ".trash-"+fmt.Sprint(time.Now().UnixNano()))
	defer os.RemoveAll(trash)

	incoming := map[string]bool{}
	for _, g := range allGlobs() {
		matches, _ := filepath.Glob(filepath.Join(stage, filepath.FromSlash(g)))
		for _, src := range matches {
			rel, _ := filepath.Rel(stage, src)
			incoming[rel] = true
			dst := filepath.Join(dir, rel)
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return nil, err
			}
			existed := false
			if _, err := os.Lstat(dst); err == nil {
				existed = true
				// Move the old copy aside first, so dst is never half-written.
				aside := filepath.Join(trash, rel)
				if err := os.MkdirAll(filepath.Dir(aside), 0o755); err != nil {
					return nil, err
				}
				if err := os.Rename(dst, aside); err != nil {
					return nil, err
				}
			}
			if err := os.Rename(src, dst); err != nil {
				return nil, err
			}
			if existed {
				res.Updated++
			} else {
				res.New++
			}
		}
	}

	var stale []string
	var covered []string
	for _, side := range mirror.Sides {
		if in.Covers(side) {
			covered = append(covered, unitGlobs[side]...)
		}
	}
	for _, g := range covered {
		matches, _ := filepath.Glob(filepath.Join(dir, filepath.FromSlash(g)))
		for _, p := range matches {
			rel, _ := filepath.Rel(dir, p)
			if !incoming[rel] && rel != filepath.FromSlash("jira/boards/bookmarked-sprints.json") {
				stale = append(stale, rel)
			}
		}
	}
	sort.Strings(stale)
	if opts.Prune && len(stale) > 0 {
		var linked int
		stale, linked = keepLinked(dir, stale, firstNonEmpty(in.JiraURL, oldURL(old, true)), firstNonEmpty(in.ConfluenceURL, oldURL(old, false)))
		if linked > 0 {
			logf("keeping %d item(s) missing from the snapshot that mirrored items still link to", linked)
		}
	}
	if opts.Prune {
		if len(stale) > 0 {
			logf("pruning %d item(s) not in the snapshot:", len(stale))
		}
		for _, rel := range stale {
			logf("  - %s", filepath.ToSlash(rel))
			aside := filepath.Join(trash, rel)
			if err := os.MkdirAll(filepath.Dir(aside), 0o755); err != nil {
				return nil, err
			}
			if err := os.Rename(filepath.Join(dir, rel), aside); err != nil {
				return nil, err
			}
			res.Pruned = append(res.Pruned, filepath.ToSlash(rel))
		}
	} else {
		res.Kept = len(stale)
	}

	if err := mergeMeta(stage, dir); err != nil {
		return nil, err
	}

	if data, err := os.ReadFile(filepath.Join(stage, "bookmarks.yaml")); err == nil {
		if err := replaceFile(filepath.Join(dir, "bookmarks.yaml"), data); err != nil {
			return nil, err
		}
		if opts.Bookmarks != nil {
			n, err := mergeBookmarks(data, opts.Bookmarks)
			if err != nil {
				return nil, fmt.Errorf("bookmarks: %w", err)
			}
			res.Bookmarks = n
		}
	}

	m := *mirror.MergeImport(old, in, time.Now().UTC().Format(time.RFC3339))
	countMirror(dir, &m)
	if err := writeManifestAtomic(dir, &m); err != nil {
		return nil, err
	}
	res.ExportedAt, res.Manifest = m.ExportedAt, &m
	return res, nil
}

func allGlobs() []string {
	var out []string
	for _, side := range mirror.Sides {
		out = append(out, unitGlobs[side]...)
	}
	return out
}

func replaceFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".part"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func writeManifestAtomic(dir string, m *mirror.Manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return replaceFile(filepath.Join(dir, mirror.ManifestFile), append(data, '\n'))
}

// mergeMeta merges the shared Jira metadata: field names are unioned, the
// rest replaced by the newer copy.
func mergeMeta(stage, dir string) error {
	var names map[string]string
	if readJSONFile(filepath.Join(stage, "jira", "names.json"), &names) == nil {
		var cur map[string]string
		_ = readJSONFile(filepath.Join(dir, "jira", "names.json"), &cur)
		if cur == nil {
			cur = map[string]string{}
		}
		for k, v := range names {
			cur[k] = v
		}
		data, err := json.MarshalIndent(cur, "", " ")
		if err != nil {
			return err
		}
		if err := replaceFile(filepath.Join(dir, "jira", "names.json"), data); err != nil {
			return err
		}
	}
	for _, f := range []string{"jira/myself.json", "jira/fields.json", "jira/linktypes.json"} {
		data, err := os.ReadFile(filepath.Join(stage, filepath.FromSlash(f)))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if err := replaceFile(filepath.Join(dir, filepath.FromSlash(f)), data); err != nil {
			return err
		}
	}
	return nil
}

// mergeBookmarks adds the archive's bookmarks the store lacks; existing
// ones (and their names) stay as they are. It returns how many were added.
func mergeBookmarks(data []byte, store *bookmarks.Store) (int, error) {
	var f struct {
		Bookmarks []bookmarks.Bookmark `yaml:"bookmarks"`
	}
	if err := yaml.Unmarshal(data, &f); err != nil {
		return 0, err
	}
	n := 0
	for _, b := range f.Bookmarks {
		if b.Kind == bookmarks.Query && strings.TrimSpace(b.Name) == "" {
			b.Name = b.JQL
		}
		has, err := store.Has(b)
		if err != nil {
			return n, err
		}
		if has || b.Validate() != nil {
			continue
		}
		if err := store.Add(b); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// countMirror fills the manifest's counts from the directory (local/ included).
func countMirror(dir string, m *mirror.Manifest) {
	m.Issues, m.Pages, m.Attachments, m.Bytes = 0, 0, 0, 0
	for _, root := range []string{dir, filepath.Join(dir, "local")} {
		issues, _ := filepath.Glob(filepath.Join(root, "jira", "issues", "*", "issue.json"))
		pages, _ := filepath.Glob(filepath.Join(root, "confluence", "pages", "*", "page.json"))
		ja, _ := filepath.Glob(filepath.Join(root, "jira", "issues", "*", "attachments", "*"))
		ca, _ := filepath.Glob(filepath.Join(root, "confluence", "pages", "*", "files", "*"))
		xa, _ := filepath.Glob(filepath.Join(root, "jira", "issues", "*", "xray", "files", "*"))
		m.Issues += len(issues)
		m.Pages += len(pages)
		m.Attachments += len(ja) + len(ca) + len(xa)
	}
	m.Bytes = dirSize(dir)
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func oldURL(m *mirror.Manifest, jira bool) string {
	switch {
	case m == nil:
		return ""
	case jira:
		return m.JiraURL
	default:
		return m.ConfluenceURL
	}
}

// keepLinked drops from stale the issues and pages that a mirrored item
// (one that stays) links to: a side's full snapshot doesn't carry what the
// other side's export brought in by reference, and pruning those would
// break the links between the two. It returns the rest and how many it
// kept.
func keepLinked(dir string, stale []string, jiraURL, confURL string) ([]string, int) {
	staleSet := map[string]bool{}
	for _, rel := range stale {
		staleSet[filepath.ToSlash(rel)] = true
	}
	issueRefs, pageIDs, pageTitles := map[string]bool{}, map[string]bool{}, map[string]bool{}
	if confURL != "" {
		dirs, _ := filepath.Glob(filepath.Join(dir, "jira", "issues", "*"))
		for _, d := range dirs {
			rel, _ := filepath.Rel(dir, d)
			if staleSet[filepath.ToSlash(rel)] {
				continue
			}
			var text strings.Builder
			for _, name := range []string{"issue.json", "remotelinks.json"} {
				var v any
				if readJSONFile(filepath.Join(d, name), &v) == nil {
					text.WriteString(xref.Strings(v))
				}
			}
			for _, r := range xref.PageRefs(text.String(), confURL) {
				if r.ID != "" {
					pageIDs[r.ID] = true
				} else {
					pageTitles[r.Space+"/"+strings.ToLower(r.Title)] = true
				}
			}
		}
	}
	if jiraURL != "" {
		dirs, _ := filepath.Glob(filepath.Join(dir, "confluence", "pages", "*"))
		for _, d := range dirs {
			rel, _ := filepath.Rel(dir, d)
			if staleSet[filepath.ToSlash(rel)] {
				continue
			}
			var p map[string]any
			if readJSONFile(filepath.Join(d, "page.json"), &p) != nil {
				continue
			}
			body, _ := obj(obj(p, "body"), "storage")["value"].(string)
			for _, r := range xref.IssueRefs(body, jiraURL) {
				issueRefs[r.Key] = true
			}
		}
	}
	var out []string
	kept := 0
	for _, rel := range stale {
		parts := strings.Split(filepath.ToSlash(rel), "/")
		linked := false
		if len(parts) == 3 && parts[0] == "jira" && parts[1] == "issues" {
			linked = issueRefs[parts[2]]
		}
		if len(parts) == 3 && parts[0] == "confluence" && parts[1] == "pages" {
			linked = pageIDs[parts[2]]
			if !linked && len(pageTitles) > 0 {
				var p map[string]any
				if readJSONFile(filepath.Join(dir, rel, "page.json"), &p) == nil {
					linked = pageTitles[fmt.Sprint(obj(p, "space")["key"])+"/"+strings.ToLower(fmt.Sprint(p["title"]))]
				}
			}
		}
		if linked {
			kept++
		} else {
			out = append(out, rel)
		}
	}
	return out, kept
}
