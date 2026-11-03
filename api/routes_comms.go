package api

import (
	"errors"
	"log"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/MartinNevlaha/stratus-v2/db"
)

// Comms: messages and a shared task board between Claude Code, OpenCode and Codex sessions.

const (
	defaultWaitSeconds = 50
	maxWaitSeconds     = 600
	waitPollInterval   = time.Second
)

// commsError maps comms sentinel errors to HTTP statuses. Recipient errors list the
// active handles so the calling agent can correct its address.
func (s *Server) commsError(w http.ResponseWriter, op string, err error) {
	switch {
	case errors.Is(err, db.ErrUnknownRecipient), errors.Is(err, db.ErrNoRecipients):
		handles := []string{}
		if peers, perr := s.db.ListPeers(true); perr == nil {
			for _, p := range peers {
				handles = append(handles, p.Handle+" ("+p.Runtime+")")
			}
		}
		// The MCP proxy keeps only "error", so the active handles go into the text too.
		msg := err.Error() + "; active peers: none"
		if len(handles) > 0 {
			msg = err.Error() + "; active peers: " + strings.Join(handles, ", ")
		}
		jsonErrCode(w, http.StatusNotFound, "UNKNOWN_RECIPIENT", msg, map[string]any{
			"active_peers": handles,
			"hint":         "address a handle, a runtime (claude-code, opencode, codex), '*' or 'user'",
		})
	case errors.Is(err, db.ErrPeerNotFound), errors.Is(err, db.ErrTaskNotFound):
		jsonErr(w, http.StatusNotFound, err.Error())
	case errors.Is(err, db.ErrHandleTaken), errors.Is(err, db.ErrTaskTaken):
		jsonErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, db.ErrInvalidHandle), errors.Is(err, db.ErrInvalidKind), errors.Is(err, db.ErrEmptyBody),
		errors.Is(err, db.ErrInvalidTaskStatus), errors.Is(err, db.ErrEmptyTitle), errors.Is(err, db.ErrTooLong):
		jsonErr(w, http.StatusBadRequest, err.Error())
	default:
		log.Printf("comms %s: %v", op, err)
		jsonErr(w, http.StatusInternalServerError, op+" failed")
	}
}

func (s *Server) handleRegisterPeer(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SessionKey string `json:"session_key"`
		Runtime    string `json:"runtime"`
		Project    string `json:"project"`
	}
	if err := decodeBody(r, &body); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if strings.TrimSpace(body.SessionKey) == "" || body.SessionKey == db.UserKey {
		jsonErr(w, http.StatusBadRequest, "session_key is required")
		return
	}
	if body.Runtime != "" && db.NormalizeRuntime(body.Runtime) == "" {
		jsonErr(w, http.StatusBadRequest, "runtime must be claude-code, opencode or codex")
		return
	}
	p, err := s.db.UpsertPeer(body.SessionKey, body.Runtime, body.Project)
	if err != nil {
		s.commsError(w, "register peer", err)
		return
	}
	s.hub.BroadcastJSON("peer_updated", p)
	json200(w, p)
}

func (s *Server) handleListPeers(w http.ResponseWriter, r *http.Request) {
	peers, err := s.db.ListPeers(r.URL.Query().Get("active") == "1")
	if err != nil {
		s.commsError(w, "list peers", err)
		return
	}
	json200(w, map[string]any{"peers": peers})
}

func (s *Server) handleEndPeer(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if err := s.db.EndPeer(key); err != nil {
		s.commsError(w, "end peer", err)
		return
	}
	if p, err := s.db.GetPeer(key); err == nil {
		s.hub.BroadcastJSON("peer_updated", p)
	}
	json200(w, map[string]any{"ok": true})
}

func (s *Server) handleRenamePeer(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Handle string `json:"handle"`
	}
	if err := decodeBody(r, &body); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	p, err := s.db.RenamePeer(r.PathValue("key"), body.Handle)
	if err != nil {
		s.commsError(w, "rename peer", err)
		return
	}
	s.hub.BroadcastJSON("peer_updated", p)
	json200(w, p)
}

func (s *Server) handleSendMessage(w http.ResponseWriter, r *http.Request) {
	var body struct {
		From   string `json:"from"`
		To     string `json:"to"`
		Kind   string `json:"kind"`
		Body   string `json:"body"`
		TaskID *int64 `json:"task_id"`
	}
	if err := decodeBody(r, &body); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if strings.TrimSpace(body.From) == "" || strings.TrimSpace(body.To) == "" {
		jsonErr(w, http.StatusBadRequest, "from and to are required")
		return
	}
	m, recipients, err := s.db.SendMessage(body.From, body.To, body.Kind, body.Body, body.TaskID)
	if err != nil {
		s.commsError(w, "send message", err)
		return
	}
	s.hub.BroadcastJSON("message_sent", m)
	out := map[string]any{"message": m, "recipients": len(recipients)}
	if len(recipients) == 1 {
		if p, err := s.db.GetPeer(recipients[0]); err == nil && p.EndedAt != nil {
			out["note"] = "recipient session " + p.Handle + " has ended; the message waits in its inbox in case the session resumes"
		}
	}
	json200(w, out)
}

func (s *Server) handleListMessages(w http.ResponseWriter, r *http.Request) {
	msgs, err := s.db.ListMessages(r.URL.Query().Get("peer"), queryInt(r, "limit", 200))
	if err != nil {
		s.commsError(w, "list messages", err)
		return
	}
	json200(w, map[string]any{"messages": msgs})
}

// inboxFilter reads from/kind and rejects filters that could never match, so a typo
// does not make a wait block for its whole timeout.
func (s *Server) inboxFilter(w http.ResponseWriter, r *http.Request) (db.InboxFilter, bool) {
	f := db.InboxFilter{From: r.URL.Query().Get("from"), Kind: r.URL.Query().Get("kind")}
	if err := s.db.ValidateInboxFilter(f); err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return f, false
	}
	return f, true
}

// handleClaimInbox returns and marks delivered the session's unread messages.
// register=1 (used by hooks) registers a missing or ended session first.
func (s *Server) handleClaimInbox(w http.ResponseWriter, r *http.Request) {
	filter, ok := s.inboxFilter(w, r)
	if !ok {
		return
	}
	msgs, err := s.db.ClaimInbox(r.Context(), r.PathValue("key"), filter, queryInt(r, "limit", 50), r.URL.Query().Get("register") == "1")
	if err != nil {
		s.commsError(w, "claim inbox", err)
		return
	}
	json200(w, map[string]any{"messages": msgs})
}

// handleWaitInbox long-polls the inbox: it returns as soon as a matching message
// arrives, or {timed_out: true} after timeout_s seconds. A client that disconnects
// stops the wait before anything more is claimed.
func (s *Server) handleWaitInbox(w http.ResponseWriter, r *http.Request) {
	timeout := defaultWaitSeconds
	if v := r.URL.Query().Get("timeout_s"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxWaitSeconds {
			jsonErr(w, http.StatusBadRequest, "timeout_s must be an integer between 1 and 600")
			return
		}
		timeout = n
	}
	filter, ok := s.inboxFilter(w, r)
	if !ok {
		return
	}
	key := r.PathValue("key")
	deadline := time.NewTimer(time.Duration(timeout) * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(waitPollInterval)
	defer tick.Stop()
	for {
		msgs, err := s.db.ClaimInbox(r.Context(), key, filter, 50, false)
		if r.Context().Err() != nil {
			return
		}
		if err != nil {
			s.commsError(w, "wait inbox", err)
			return
		}
		if len(msgs) > 0 {
			json200(w, map[string]any{"messages": msgs, "timed_out": false})
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-deadline.C:
			json200(w, map[string]any{"messages": []db.Message{}, "timed_out": true})
			return
		case <-tick.C:
		}
	}
}

func (s *Server) handleCreateSharedTask(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CreatedBy   string `json:"created_by"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Assignee    string `json:"assignee"`
	}
	if err := decodeBody(r, &body); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if strings.TrimSpace(body.CreatedBy) == "" {
		jsonErr(w, http.StatusBadRequest, "created_by is required")
		return
	}
	t, err := s.db.CreateSharedTask(body.CreatedBy, body.Title, body.Description, body.Assignee)
	if err != nil {
		s.commsError(w, "create shared task", err)
		return
	}
	s.hub.BroadcastJSON("shared_task_updated", t)
	if body.Assignee != "" {
		s.hub.BroadcastJSON("message_sent", map[string]any{"task_id": t.ID})
	}
	json200(w, t)
}

func (s *Server) handleListSharedTasks(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	if status != "" && !slices.Contains(db.SharedTaskStatuses, status) {
		jsonErr(w, http.StatusBadRequest, "status must be one of "+strings.Join(db.SharedTaskStatuses, ", "))
		return
	}
	tasks, err := s.db.ListSharedTasks(status)
	if err != nil {
		s.commsError(w, "list shared tasks", err)
		return
	}
	json200(w, map[string]any{"tasks": tasks})
}

func (s *Server) handleUpdateSharedTask(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid task id")
		return
	}
	var body struct {
		Actor    string  `json:"actor"`
		Status   *string `json:"status"`
		Result   *string `json:"result"`
		Assignee *string `json:"assignee"`
	}
	if err := decodeBody(r, &body); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if strings.TrimSpace(body.Actor) == "" {
		jsonErr(w, http.StatusBadRequest, "actor is required")
		return
	}
	t, err := s.db.UpdateSharedTask(id, body.Actor, db.SharedTaskUpdate{Status: body.Status, Result: body.Result, Assignee: body.Assignee})
	if err != nil {
		s.commsError(w, "update shared task", err)
		return
	}
	s.hub.BroadcastJSON("shared_task_updated", t)
	s.hub.BroadcastJSON("message_sent", map[string]any{"task_id": t.ID})
	json200(w, t)
}
