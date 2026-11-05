package api

import (
	"net/http"
	"strconv"

	"github.com/MartinNevlaha/stratus-v2/db"
)

func (s *Server) handleSaveEvent(w http.ResponseWriter, r *http.Request) {
	var in db.SaveEventInput
	if err := decodeBody(r, &in); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	id, err := s.db.SaveEvent(in)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.hub.BroadcastJSON("event_saved", map[string]any{"id": id})
	json200(w, map[string]any{"id": id})
}

// compactSnippetRunes is the longest text excerpt a compact search result carries.
const compactSnippetRunes = 200

// compactEvent is the index form of an event: enough to pick which IDs to fetch in full.
type compactEvent struct {
	ID         int64    `json:"id"`
	Ts         string   `json:"ts"`
	Scope      string   `json:"scope"`
	Type       string   `json:"type"`
	Title      string   `json:"title"`
	Snippet    string   `json:"snippet"`
	Tags       []string `json:"tags"`
	Importance float64  `json:"importance"`
	Project    *string  `json:"project,omitempty"`
}

func compactEvents(events []db.Event) []compactEvent {
	out := make([]compactEvent, len(events))
	for i, e := range events {
		snippet := e.Text
		if r := []rune(snippet); len(r) > compactSnippetRunes {
			snippet = string(r[:compactSnippetRunes]) + "…"
		}
		out[i] = compactEvent{
			ID: e.ID, Ts: e.Ts, Scope: e.Scope, Type: e.Type, Title: e.Title,
			Snippet: snippet, Tags: e.Tags, Importance: e.Importance, Project: e.Project,
		}
	}
	return out
}

func (s *Server) handleSearchEvents(w http.ResponseWriter, r *http.Request) {
	in := db.SearchEventsInput{
		Query:     queryStr(r, "q"),
		Type:      queryStr(r, "type"),
		Scope:     queryStr(r, "scope"),
		Project:   queryStr(r, "project"),
		DateStart: queryStr(r, "date_start"),
		DateEnd:   queryStr(r, "date_end"),
		Limit:     queryInt(r, "limit", 20),
		Offset:    queryInt(r, "offset", 0),
	}
	events, err := s.db.SearchEvents(in)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if events == nil {
		events = []db.Event{}
	}
	if queryStr(r, "compact") == "true" {
		json200(w, map[string]any{"results": compactEvents(events), "count": len(events)})
		return
	}
	json200(w, map[string]any{"results": events, "count": len(events)})
}

func (s *Server) handleTimeline(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	before := queryInt(r, "before", 10)
	after := queryInt(r, "after", 10)
	events, err := s.db.GetTimeline(id, before, after)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if events == nil {
		events = []db.Event{}
	}
	json200(w, map[string]any{"events": events, "anchor_id": id})
}

func (s *Server) handleBatchEvents(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []int64 `json:"ids"`
	}
	if err := decodeBody(r, &body); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	events, err := s.db.GetEventsByIDs(body.IDs)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if events == nil {
		events = []db.Event{}
	}
	json200(w, map[string]any{"results": events})
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ContentSessionID string  `json:"content_session_id"`
		Project          string  `json:"project"`
		InitialPrompt    *string `json:"initial_prompt"`
	}
	if err := decodeBody(r, &body); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if body.ContentSessionID == "" {
		jsonErr(w, http.StatusBadRequest, "content_session_id is required")
		return
	}
	sess, err := s.db.SaveSession(body.ContentSessionID, body.Project, body.InitialPrompt)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	json200(w, sess)
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	limit := queryInt(r, "limit", 50)
	sessions, err := s.db.ListSessions(limit)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if sessions == nil {
		sessions = []db.Session{}
	}
	json200(w, map[string]any{"sessions": sessions})
}
