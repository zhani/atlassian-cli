package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
)

// A demo Scrum board with three sprints. Sprint and epic membership is kept
// in memory and seeded from the issue statuses, so the explorers have
// something to show; it does not come from the data.

type fakeSprint struct {
	ID                      int
	Name, State, Start, End string
}

var fakeSprints = []fakeSprint{
	{1, "Sprint 1", "closed", "2026-09-01T08:00:00.000Z", "2026-09-14T16:00:00.000Z"},
	{2, "Sprint 2", "active", "2026-09-15T08:00:00.000Z", "2026-09-28T16:00:00.000Z"},
	{3, "Sprint 3", "future", "", ""},
}

func (s *store) seedAgile() {
	s.sprintOf = map[string]int{}
	s.epicOf = map[string]string{}
	epics := map[string]string{} // project -> first epic
	for _, key := range s.order {
		f := mapOf(s.issues[key], "fields")
		if nameOf(f, "issuetype") == "Epic" {
			project, _, _ := strings.Cut(key, "-")
			if epics[project] == "" {
				epics[project] = key
			}
		}
	}
	for _, key := range s.order {
		f := mapOf(s.issues[key], "fields")
		switch nameOf(f, "status") {
		case "In Progress", "In Review":
			s.sprintOf[key] = 2
		case "To be corrected":
			s.sprintOf[key] = 1
		case "Open":
			s.sprintOf[key] = 3
		}
		project, _, _ := strings.Cut(key, "-")
		if epic := epics[project]; epic != "" && epic != key && nameOf(f, "issuetype") != "Epic" {
			s.epicOf[key] = epic
		}
	}
}

func (s *store) registerAgile(mux *http.ServeMux) {
	mux.HandleFunc("GET /rest/agile/1.0/board", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"values": []any{map[string]any{"id": 1, "name": "Demo board", "type": "scrum"}}, "isLast": true})
	})
	mux.HandleFunc("GET /rest/agile/1.0/board/{id}/sprint", s.listSprints)
	mux.HandleFunc("POST /rest/agile/1.0/sprint/{id}/issue", s.moveToSprint)
	mux.HandleFunc("POST /rest/agile/1.0/epic/{key}/issue", s.moveToEpic)
	mux.HandleFunc("GET /rest/api/space/{key}/content/page", s.rootPages)
}

func (s *store) listSprints(w http.ResponseWriter, r *http.Request) {
	states := r.URL.Query().Get("state")
	var out []any
	for _, sp := range fakeSprints {
		if states != "" && !strings.Contains(states, sp.State) {
			continue
		}
		out = append(out, map[string]any{"id": sp.ID, "name": sp.Name, "state": sp.State, "startDate": sp.Start, "endDate": sp.End})
	}
	start := intParam(r, "startAt", 0)
	end := min(len(out), start+intParam(r, "maxResults", 50))
	start = min(start, end)
	writeJSON(w, http.StatusOK, map[string]any{"values": out[start:end], "isLast": end >= len(out)})
}

func readIssueKeys(r *http.Request) ([]string, error) {
	var body struct {
		Issues []string `json:"issues"`
	}
	err := json.NewDecoder(r.Body).Decode(&body)
	return body.Issues, err
}

func (s *store) moveToSprint(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	keys, derr := readIssueKeys(r)
	if err != nil || derr != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"errorMessages": []string{"invalid request"}})
		return
	}
	for _, k := range keys {
		s.sprintOf[k] = id
	}
	log.Printf("sprint %d += %v", id, keys)
	w.WriteHeader(http.StatusNoContent)
}

func (s *store) moveToEpic(w http.ResponseWriter, r *http.Request) {
	keys, err := readIssueKeys(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"errorMessages": []string{err.Error()}})
		return
	}
	epic := r.PathValue("key")
	for _, k := range keys {
		if epic == "none" {
			delete(s.epicOf, k)
		} else {
			s.epicOf[k] = epic
		}
	}
	log.Printf("epic %s += %v", epic, keys)
	w.WriteHeader(http.StatusNoContent)
}

func (s *store) rootPages(w http.ResponseWriter, r *http.Request) {
	var out []any
	for _, id := range s.pageOrder {
		if p := s.pages[id]; p.Space == r.PathValue("key") && p.Parent == "" {
			out = append(out, s.pageJSON(p, false))
		}
	}
	paged(w, r, out)
}
