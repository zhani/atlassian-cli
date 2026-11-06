package mirrorsync

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/zhani/atlassian-cli/internal/embeds"
)

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
	nb, uses, unmapped := embeds.Apply(body, e.opts.Embeds)
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
		if have[u.Title] {
			continue
		}
		data, err := os.ReadFile(u.Embed.File)
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
		atts = append(atts, embedAttachment(id, aid, u.Title, int64(len(data))))
		have[u.Title] = true
		e.stats.attachments++
		e.logf("page %s (%s): embedded %s attached", id, title, u.Title)
	}
	storage["value"] = nb
	return atts
}
