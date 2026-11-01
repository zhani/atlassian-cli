package tui

import (
	"github.com/charmbracelet/bubbles/key"
	"github.com/putcho01/atlassian-cli/internal/bookmarks"
)

// Bookmarker adds and removes bookmarks; *bookmarks.Store implements it.
// Views without one have no bookmark keys.
type Bookmarker interface {
	Toggle(b bookmarks.Bookmark) (added bool, err error)
}

var bookmarkKeys = struct {
	toggle, saveQuery key.Binding
}{
	toggle:    key.NewBinding(key.WithKeys("B"), key.WithHelp("B", "bookmark")),
	saveQuery: key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "bookmark query")),
}

// toggleBookmark bookmarks b, or removes its bookmark, and describes the
// outcome for the status line.
func toggleBookmark(bm Bookmarker, b bookmarks.Bookmark) (status string, isErr bool) {
	added, err := bm.Toggle(b)
	switch {
	case err != nil:
		return "Bookmark failed: " + err.Error(), true
	case added:
		return "Bookmarked " + b.Label(), false
	}
	return "Removed bookmark " + b.Label(), false
}

// issueBookmark is the bookmark of an issue shown in the browser.
func (m model) issueBookmark(issueKey string) bookmarks.Bookmark {
	b := bookmarks.Bookmark{Kind: bookmarks.Issue, Key: issueKey, URL: m.opts.Source.IssueURL(issueKey)}
	if is, ok := m.cache[issueKey]; ok {
		b.Title = is.Fields.Summary
	}
	for _, is := range m.issues {
		if is.Key == issueKey {
			b.Title = is.Fields.Summary
		}
	}
	return b
}

// queryBookmark is the bookmark of the browser's query, named after its
// caption.
func (m model) queryBookmark() bookmarks.Bookmark {
	name := m.opts.Caption
	if name == "" {
		name = m.opts.JQL
	}
	return bookmarks.Bookmark{Kind: bookmarks.Query, Name: name, JQL: m.opts.JQL}
}
