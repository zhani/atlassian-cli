package main

import (
	"encoding/json"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Endpoints for issue operations (assign, links, worklogs, fields,
// attachments, development info) and Confluence attachments.

var linkTypes = []map[string]any{
	{"id": "1", "name": "Blocks", "outward": "blocks", "inward": "is blocked by"},
	{"id": "2", "name": "Relates", "outward": "relates to", "inward": "relates to"},
	{"id": "3", "name": "Implements", "outward": "implements", "inward": "is implemented by"},
	{"id": "4", "name": "Values", "outward": "gives values to", "inward": "gets values from"},
}

func (s *store) registerOperations(mux *http.ServeMux) {
	mux.HandleFunc("GET /rest/api/2/user/search", s.searchUsers)
	mux.HandleFunc("PUT /rest/api/2/issue/{key}/assignee", s.assign)
	mux.HandleFunc("DELETE /rest/api/2/issue/{key}", s.deleteIssue)
	mux.HandleFunc("GET /rest/api/2/issueLinkType", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"issueLinkTypes": linkTypes})
	})
	mux.HandleFunc("POST /rest/api/2/issueLink", s.createLink)
	mux.HandleFunc("DELETE /rest/api/2/issueLink/{id}", s.deleteLink)
	mux.HandleFunc("POST /rest/api/2/issue/{key}/remotelink", s.logBody("remote link"))
	mux.HandleFunc("POST /rest/api/2/issue/{key}/worklog", s.addWorklog)
	mux.HandleFunc("GET /rest/api/2/field", s.listFields)
	mux.HandleFunc("POST /rest/api/2/issue/{key}/attachments", s.uploadAttachment)
	mux.HandleFunc("DELETE /rest/api/2/attachment/{id}", s.deleteAttachment)
	mux.HandleFunc("GET /rest/dev-status/latest/issue/summary", s.devSummary)
	mux.HandleFunc("GET /rest/dev-status/latest/issue/detail", s.devDetail)
	mux.HandleFunc("POST /rest/api/content/{id}/child/attachment", s.uploadPageAttachment)
	mux.HandleFunc("GET /rest/api/content/{id}/child/attachment", s.listPageAttachments)
	mux.HandleFunc("POST /rest/api/content/{id}/child/attachment/{att}/data", s.updatePageAttachment)
	mux.HandleFunc("DELETE /rest/api/content/{id}", s.deleteContent)
}

func (s *store) logBody(what string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		log.Printf("%s on %s: %s", what, r.PathValue("key"), body)
		writeJSON(w, http.StatusCreated, map[string]any{"id": 1})
	}
}

// users are derived from the people in the data; usernames are made up
// from the display names.
func (s *store) users() []map[string]any {
	seen := map[string]bool{"Fake User": true}
	users := []map[string]any{{"name": "fake", "displayName": "Fake User", "active": true}}
	for _, key := range s.order {
		f := mapOf(s.issues[key], "fields")
		for _, role := range []string{"assignee", "reporter"} {
			dn, _ := mapOf(f, role)["displayName"].(string)
			if dn == "" || seen[dn] {
				continue
			}
			seen[dn] = true
			users = append(users, map[string]any{"name": username(dn), "displayName": dn, "active": true})
		}
	}
	return users
}

func username(displayName string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(displayName) {
		if r >= 'a' && r <= 'z' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (s *store) searchUsers(w http.ResponseWriter, r *http.Request) {
	q := strings.ToLower(r.URL.Query().Get("username") + r.URL.Query().Get("query"))
	var out []map[string]any
	for _, u := range s.users() {
		if strings.Contains(strings.ToLower(u["name"].(string)+" "+u["displayName"].(string)), q) {
			out = append(out, u)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *store) assign(w http.ResponseWriter, r *http.Request) {
	is, ok := s.issues[r.PathValue("key")]
	if !ok {
		notFound(w, "Issue "+r.PathValue("key"))
		return
	}
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	f := mapOf(is, "fields")
	name, _ := body["name"].(string)
	if name == "" || name == "-1" {
		f["assignee"] = nil
		w.WriteHeader(http.StatusNoContent)
		return
	}
	for _, u := range s.users() {
		if u["name"] == name {
			f["assignee"] = u
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}
	writeJSON(w, http.StatusBadRequest, map[string]any{"errors": map[string]string{"assignee": "User '" + name + "' does not exist."}})
}

func (s *store) deleteIssue(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	is, ok := s.issues[key]
	if !ok {
		notFound(w, "Issue "+key)
		return
	}
	if subs, _ := mapOf(is, "fields")["subtasks"].([]any); len(subs) > 0 && r.URL.Query().Get("deleteSubtasks") != "true" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"errorMessages": []string{"The issue has subtasks; use deleteSubtasks=true"}})
		return
	}
	delete(s.issues, key)
	for i, k := range s.order {
		if k == key {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// createLink stores the link on both issues, as Jira reports it: the inward
// issue sees an outwardIssue entry and vice versa.
func (s *store) createLink(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Type    struct{ Name string } `json:"type"`
		Inward  struct{ Key string }  `json:"inwardIssue"`
		Outward struct{ Key string }  `json:"outwardIssue"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"errorMessages": []string{err.Error()}})
		return
	}
	var lt map[string]any
	for _, t := range linkTypes {
		if t["name"] == body.Type.Name {
			lt = t
		}
	}
	from, okFrom := s.issues[body.Inward.Key]
	to, okTo := s.issues[body.Outward.Key]
	if lt == nil || !okFrom || !okTo {
		writeJSON(w, http.StatusBadRequest, map[string]any{"errorMessages": []string{"invalid link"}})
		return
	}
	s.nextLinkID++
	id := strconv.Itoa(s.nextLinkID)
	ref := func(is map[string]any) map[string]any {
		f := mapOf(is, "fields")
		return map[string]any{"key": is["key"], "fields": map[string]any{"summary": f["summary"], "status": f["status"]}}
	}
	addLink(from, map[string]any{"id": id, "type": lt, "outwardIssue": ref(to)})
	addLink(to, map[string]any{"id": id, "type": lt, "inwardIssue": ref(from)})
	log.Printf("link %s: %s %s %s", id, body.Inward.Key, lt["outward"], body.Outward.Key)
	w.WriteHeader(http.StatusCreated)
}

func addLink(is map[string]any, link map[string]any) {
	f := mapOf(is, "fields")
	links, _ := f["issuelinks"].([]any)
	f["issuelinks"] = append(links, link)
}

func (s *store) deleteLink(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	found := false
	for _, key := range s.order {
		f := mapOf(s.issues[key], "fields")
		links, _ := f["issuelinks"].([]any)
		kept := links[:0:0]
		for _, l := range links {
			if m, _ := l.(map[string]any); m["id"] == id {
				found = true
				continue
			}
			kept = append(kept, l)
		}
		f["issuelinks"] = kept
	}
	if !found {
		notFound(w, "Link "+id)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *store) addWorklog(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	log.Printf("worklog on %s: %v", r.PathValue("key"), body)
	body["id"] = "1"
	writeJSON(w, http.StatusCreated, body)
}

// listFields returns the system fields the CLI uses plus every custom field
// found in the data, named "Custom <id>".
func (s *store) listFields(w http.ResponseWriter, _ *http.Request) {
	fields := []map[string]any{
		{"id": "summary", "name": "Summary", "custom": false, "schema": map[string]any{"type": "string"}},
		{"id": "status", "name": "Status", "custom": false, "schema": map[string]any{"type": "status"}},
		{"id": "assignee", "name": "Assignee", "custom": false, "schema": map[string]any{"type": "user"}},
		{"id": "priority", "name": "Priority", "custom": false, "schema": map[string]any{"type": "priority"}},
		{"id": "labels", "name": "Labels", "custom": false, "schema": map[string]any{"type": "array", "items": "string"}},
		{"id": "fixVersions", "name": "Fix Version/s", "custom": false, "schema": map[string]any{"type": "array", "items": "version"}},
	}
	seen := map[string]bool{}
	for _, key := range s.order {
		for id := range mapOf(s.issues[key], "fields") {
			if strings.HasPrefix(id, "customfield_") && !seen[id] {
				seen[id] = true
			}
		}
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		fields = append(fields, map[string]any{"id": id, "name": s.fieldName(id), "custom": true, "schema": map[string]any{"type": "any"}})
	}
	writeJSON(w, http.StatusOK, fields)
}

// saveUpload stores the multipart "file" in a temporary directory and
// registers it as an attachment.
func (s *store) saveUpload(r *http.Request, owner string) (attachment, error) {
	file, hdr, err := r.FormFile("file")
	if err != nil {
		return attachment{}, err
	}
	defer file.Close()
	dir, err := os.MkdirTemp("", "devserver-upload-")
	if err != nil {
		return attachment{}, err
	}
	path := filepath.Join(dir, filepath.Base(hdr.Filename))
	out, err := os.Create(path)
	if err != nil {
		return attachment{}, err
	}
	size, err := io.Copy(out, file)
	out.Close()
	if err != nil {
		return attachment{}, err
	}
	a := attachment{
		ID: strconv.Itoa(10000 + len(s.attachments)), Key: owner, Filename: filepath.Base(hdr.Filename),
		Path: path, Size: size, MimeType: mime.TypeByExtension(filepath.Ext(hdr.Filename)),
	}
	if a.MimeType == "" {
		a.MimeType = "application/octet-stream"
	}
	s.attachments[a.ID] = a
	log.Printf("uploaded %s (%d bytes) to %s", a.Filename, size, owner)
	return a, nil
}

func (s *store) uploadAttachment(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	is, ok := s.issues[key]
	if !ok {
		notFound(w, "Issue "+key)
		return
	}
	if r.Header.Get("X-Atlassian-Token") != "no-check" {
		writeJSON(w, http.StatusForbidden, map[string]any{"errorMessages": []string{"XSRF check failed"}})
		return
	}
	a, err := s.saveUpload(r, key)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"errorMessages": []string{err.Error()}})
		return
	}
	f := mapOf(is, "fields")
	atts, _ := f["attachment"].([]any)
	f["attachment"] = append(atts, s.attachmentJSON(a))
	writeJSON(w, http.StatusOK, []any{s.attachmentJSON(a)})
}

func (s *store) deleteAttachment(w http.ResponseWriter, r *http.Request) {
	a, ok := s.attachments[r.PathValue("id")]
	if !ok {
		notFound(w, "Attachment "+r.PathValue("id"))
		return
	}
	delete(s.attachments, a.ID)
	if is, ok := s.issues[a.Key]; ok {
		f := mapOf(is, "fields")
		atts, _ := f["attachment"].([]any)
		kept := atts[:0:0]
		for _, x := range atts {
			if m, _ := x.(map[string]any); m["id"] != a.ID {
				kept = append(kept, x)
			}
		}
		f["attachment"] = kept
	}
	w.WriteHeader(http.StatusNoContent)
}

// devSummary reports one Bitbucket Server pull request and branch for every
// issue, so the development panel can be tried out.
func (s *store) devSummary(w http.ResponseWriter, r *http.Request) {
	counts := map[string]any{"byInstanceType": map[string]any{"stash": map[string]any{"count": 1, "name": "Bitbucket Server"}}}
	writeJSON(w, http.StatusOK, map[string]any{"summary": map[string]any{"pullrequest": counts, "branch": counts}})
}

func (s *store) devDetail(w http.ResponseWriter, r *http.Request) {
	key := ""
	for k, is := range s.issues {
		if is["id"] == r.URL.Query().Get("issueId") {
			key = k
		}
	}
	branch := "feature/" + key
	writeJSON(w, http.StatusOK, map[string]any{"detail": []any{map[string]any{
		"pullRequests": []any{map[string]any{
			"id": "#1", "name": key + ": demo change", "status": "OPEN", "url": s.baseURL + "/pr/1",
			"author": map[string]any{"name": "Fake User"}, "source": map[string]any{"branch": branch},
			"destination": map[string]any{"branch": "main"}, "repositoryName": "demo",
		}},
		"branches": []any{map[string]any{"name": branch, "url": s.baseURL + "/branch", "repository": map[string]any{"name": "demo"}}},
	}}})
}

func (s *store) pageAttachmentJSON(a attachment) map[string]any {
	return map[string]any{
		"id": a.ID, "type": "attachment", "status": "current", "title": a.Filename,
		"mediaType": a.MimeType, "fileSize": a.Size,
		"_links": map[string]any{"download": "/secure/attachment/" + a.ID + "/" + a.Filename},
	}
}

func (s *store) listPageAttachments(w http.ResponseWriter, r *http.Request) {
	var out []any
	for _, a := range s.sortedAttachments() {
		if a.Key == "page:"+r.PathValue("id") {
			out = append(out, s.pageAttachmentJSON(a))
		}
	}
	paged(w, r, out)
}

func (s *store) sortedAttachments() []attachment {
	out := make([]attachment, 0, len(s.attachments))
	for _, a := range s.attachments {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (s *store) uploadPageAttachment(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := s.pages[id]; !ok {
		notFound(w, "Page "+id)
		return
	}
	a, err := s.saveUpload(r, "page:"+id)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": []any{s.pageAttachmentJSON(a)}, "size": 1})
}

// updatePageAttachment stores a new version of an attachment.
func (s *store) updatePageAttachment(w http.ResponseWriter, r *http.Request) {
	old, ok := s.attachments[r.PathValue("att")]
	if !ok {
		notFound(w, "Attachment "+r.PathValue("att"))
		return
	}
	a, err := s.saveUpload(r, old.Key)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"message": err.Error()})
		return
	}
	delete(s.attachments, a.ID)
	a.ID = old.ID
	s.attachments[a.ID] = a
	writeJSON(w, http.StatusOK, s.pageAttachmentJSON(a))
}

// deleteContent deletes a page attachment (or a page).
func (s *store) deleteContent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if a, ok := s.attachments[id]; ok && strings.HasPrefix(a.Key, "page:") {
		delete(s.attachments, id)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if _, ok := s.pages[id]; ok {
		delete(s.pages, id)
		for i, p := range s.pageOrder {
			if p == id {
				s.pageOrder = append(s.pageOrder[:i], s.pageOrder[i+1:]...)
				break
			}
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	notFound(w, "Content "+id)
}
