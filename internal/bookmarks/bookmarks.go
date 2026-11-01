// Package bookmarks stores bookmarks to Jira issues, saved JQL queries,
// features, epics, sprints and Confluence pages in a local YAML file.
package bookmarks

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// Kind is what a bookmark points to.
type Kind string

const (
	Issue   Kind = "issue"
	Query   Kind = "query"
	Feature Kind = "feature"
	Epic    Kind = "epic"
	Sprint  Kind = "sprint"
	Page    Kind = "page"
)

// Kinds lists the bookmark kinds in display order.
var Kinds = []Kind{Issue, Query, Feature, Epic, Sprint, Page}

// ParseKind accepts a kind name (also plural, and "jql" for queries).
func ParseKind(s string) (Kind, error) {
	s = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(s)), "s")
	if s == "jql" || s == "search" || s == "filter" {
		return Query, nil
	}
	for _, k := range Kinds {
		if string(k) == s {
			return k, nil
		}
	}
	return "", fmt.Errorf("unknown bookmark kind %q (use issue, query, feature, epic, sprint or page)", s)
}

// Bookmark is one saved item.
type Bookmark struct {
	Name  string `yaml:"name,omitempty" json:"name,omitempty"`   // label shown in lists
	Kind  Kind   `yaml:"kind" json:"kind"`                       // what it points to
	Key   string `yaml:"key,omitempty" json:"key,omitempty"`     // issue key, sprint or page ID
	JQL   string `yaml:"jql,omitempty" json:"jql,omitempty"`     // query bookmarks
	Title string `yaml:"title,omitempty" json:"title,omitempty"` // summary, page title or sprint name when added
	Space string `yaml:"space,omitempty" json:"space,omitempty"` // space of a page
	URL   string `yaml:"url,omitempty" json:"url,omitempty"`     // web page
	Added string `yaml:"added,omitempty" json:"added,omitempty"` // RFC 3339
}

// ID identifies the bookmarked item: two bookmarks with the same ID point
// to the same thing.
func (b Bookmark) ID() string {
	if b.Kind == Query {
		return string(Query) + ":" + strings.Join(strings.Fields(b.JQL), " ")
	}
	return string(b.Kind) + ":" + strings.ToUpper(b.Key)
}

// Label is the bookmark's display name, falling back to the key or JQL.
func (b Bookmark) Label() string {
	switch {
	case b.Name != "":
		return b.Name
	case b.Title != "" && b.Key != "":
		return b.Key + " " + b.Title
	case b.Key != "":
		return b.Key
	}
	return b.JQL
}

// DisplayName is the name for lists that show the key separately: the
// name, else the title, else the key or JQL.
func (b Bookmark) DisplayName() string {
	switch {
	case b.Name != "":
		return b.Name
	case b.Title != "":
		return b.Title
	}
	return b.Target()
}

// Target is what the bookmark opens: the key, ID or JQL.
func (b Bookmark) Target() string {
	if b.Kind == Query {
		return b.JQL
	}
	return b.Key
}

// Validate checks that a bookmark has what its kind needs.
func (b Bookmark) Validate() error {
	if _, err := ParseKind(string(b.Kind)); err != nil {
		return err
	}
	switch {
	case b.Kind == Query && strings.TrimSpace(b.JQL) == "":
		return errors.New("a query bookmark needs JQL")
	case b.Kind == Query && strings.TrimSpace(b.Name) == "":
		return errors.New("a query bookmark needs a name")
	case b.Kind != Query && strings.TrimSpace(b.Key) == "":
		return fmt.Errorf("a %s bookmark needs a key or ID", b.Kind)
	case (b.Kind == Sprint || b.Kind == Page) && !isNumber(b.Key):
		return fmt.Errorf("%s ID must be a number, got %q", b.Kind, b.Key)
	}
	return nil
}

func isNumber(s string) bool {
	_, err := strconv.Atoi(s)
	return err == nil
}

// Store is the bookmark file.
type Store struct {
	Path string
}

// DefaultPath is ATLASSIAN_BOOKMARKS, or bookmarks.yaml in the user's
// config directory (~/.config/atlassian-cli on Linux).
func DefaultPath() (string, error) {
	if p := os.Getenv("ATLASSIAN_BOOKMARKS"); p != "" {
		return p, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("cannot find the config directory (set ATLASSIAN_BOOKMARKS): %w", err)
	}
	return filepath.Join(dir, "atlassian-cli", "bookmarks.yaml"), nil
}

// Open returns the store at DefaultPath.
func Open() (*Store, error) {
	p, err := DefaultPath()
	if err != nil {
		return nil, err
	}
	return &Store{Path: p}, nil
}

type file struct {
	Bookmarks []Bookmark `yaml:"bookmarks"`
}

// List reads all bookmarks; a missing file has none.
func (s *Store) List() ([]Bookmark, error) {
	data, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var f file
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("%s: %w", s.Path, err)
	}
	return f.Bookmarks, nil
}

const header = `# atlassian-cli bookmarks. Edit freely; kinds: issue, query, feature, epic,
# sprint, page. "key" is an issue key or a sprint/page ID, "jql" the query.
`

func (s *Store) save(list []Bookmark) error {
	var buf strings.Builder
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(file{Bookmarks: list}); err != nil {
		return err
	}
	data := buf.String()
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.Path), ".bookmarks-*.yaml")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(header + data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.Path)
}

// Add saves a bookmark; one to the same item replaces the old one (keeping
// its position).
func (s *Store) Add(b Bookmark) error {
	if err := b.Validate(); err != nil {
		return err
	}
	if b.Kind != Query {
		b.Key = strings.TrimSpace(b.Key)
		if b.Kind != Sprint && b.Kind != Page {
			b.Key = strings.ToUpper(b.Key)
		}
	}
	b.Name, b.Title = strings.TrimSpace(b.Name), strings.TrimSpace(b.Title)
	if b.Added == "" {
		b.Added = time.Now().UTC().Format(time.RFC3339)
	}
	list, err := s.List()
	if err != nil {
		return err
	}
	for i := range list {
		if list[i].ID() == b.ID() {
			list[i] = b
			return s.save(list)
		}
	}
	return s.save(append(list, b))
}

// Has reports whether the item of b is bookmarked.
func (s *Store) Has(b Bookmark) (bool, error) {
	list, err := s.List()
	if err != nil {
		return false, err
	}
	for _, x := range list {
		if x.ID() == b.ID() {
			return true, nil
		}
	}
	return false, nil
}

// Toggle adds the bookmark, or removes it when its item is bookmarked
// already. added reports which happened.
func (s *Store) Toggle(b Bookmark) (added bool, err error) {
	list, err := s.List()
	if err != nil {
		return false, err
	}
	for i, x := range list {
		if x.ID() == b.ID() {
			return false, s.save(append(list[:i:i], list[i+1:]...))
		}
	}
	return true, s.Add(b)
}

// Find resolves a reference: a 1-based position in the list, a name, a key
// or ID (case-insensitive), or the JQL of a query.
func (s *Store) Find(ref string) (Bookmark, int, error) {
	list, err := s.List()
	if err != nil {
		return Bookmark{}, -1, err
	}
	i, err := find(list, ref)
	if err != nil {
		return Bookmark{}, -1, err
	}
	return list[i], i, nil
}

func find(list []Bookmark, ref string) (int, error) {
	ref = strings.TrimSpace(ref)
	if n, err := strconv.Atoi(ref); err == nil && n >= 1 && n <= len(list) {
		// Numbers are positions, unless they match a sprint or page ID.
		if i := matching(list, ref); len(i) == 0 {
			return n - 1, nil
		}
	}
	idx := matching(list, ref)
	switch len(idx) {
	case 0:
		return -1, fmt.Errorf("no bookmark %q (see `atlassian-cli bookmark list`)", ref)
	case 1:
		return idx[0], nil
	}
	names := make([]string, len(idx))
	for j, i := range idx {
		names[j] = fmt.Sprintf("%d (%s %s)", i+1, list[i].Kind, list[i].Label())
	}
	return -1, fmt.Errorf("%q matches several bookmarks, use a number: %s", ref, strings.Join(names, ", "))
}

func matching(list []Bookmark, ref string) []int {
	var idx []int
	for i, b := range list {
		if strings.EqualFold(b.Name, ref) || (b.Key != "" && strings.EqualFold(b.Key, ref)) ||
			(b.Kind == Query && strings.Join(strings.Fields(b.JQL), " ") == strings.Join(strings.Fields(ref), " ")) {
			idx = append(idx, i)
		}
	}
	return idx
}

// Remove deletes the bookmarks matching refs (see Find).
func (s *Store) Remove(refs ...string) ([]Bookmark, error) {
	list, err := s.List()
	if err != nil {
		return nil, err
	}
	drop := map[int]bool{}
	for _, ref := range refs {
		i, err := find(list, ref)
		if err != nil {
			return nil, err
		}
		drop[i] = true
	}
	var kept, removed []Bookmark
	for i, b := range list {
		if drop[i] {
			removed = append(removed, b)
		} else {
			kept = append(kept, b)
		}
	}
	return removed, s.save(kept)
}

// Rename changes the name of the bookmark matching ref.
func (s *Store) Rename(ref, name string) (Bookmark, error) {
	list, err := s.List()
	if err != nil {
		return Bookmark{}, err
	}
	i, err := find(list, ref)
	if err != nil {
		return Bookmark{}, err
	}
	if list[i].Kind == Query && strings.TrimSpace(name) == "" {
		return Bookmark{}, errors.New("a query bookmark needs a name")
	}
	list[i].Name = strings.TrimSpace(name)
	return list[i], s.save(list)
}

// Delete removes the bookmark of b's item.
func (s *Store) Delete(b Bookmark) error {
	list, err := s.List()
	if err != nil {
		return err
	}
	for i, x := range list {
		if x.ID() == b.ID() {
			return s.save(append(list[:i:i], list[i+1:]...))
		}
	}
	return fmt.Errorf("%s is not bookmarked", b.Label())
}

// SetName renames the bookmark of b's item.
func (s *Store) SetName(b Bookmark, name string) error {
	list, err := s.List()
	if err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	if b.Kind == Query && name == "" {
		return errors.New("a query bookmark needs a name")
	}
	for i, x := range list {
		if x.ID() == b.ID() {
			list[i].Name = name
			return s.save(list)
		}
	}
	return fmt.Errorf("%s is not bookmarked", b.Label())
}

// Raw returns the file's content, or a template when there is no file.
func (s *Store) Raw() (string, error) {
	data, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return header + "bookmarks: []\n", nil
	}
	return string(data), err
}

// SaveRaw replaces the file with hand-edited content after checking that it
// parses and every bookmark is valid; the text is kept as written.
func (s *Store) SaveRaw(text string) error {
	var f file
	if err := yaml.Unmarshal([]byte(text), &f); err != nil {
		return err
	}
	for i, b := range f.Bookmarks {
		if err := b.Validate(); err != nil {
			return fmt.Errorf("bookmark %d (%s): %w", i+1, b.Label(), err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(s.Path, []byte(text), 0o600)
}
