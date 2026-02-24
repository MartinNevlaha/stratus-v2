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
