package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/MartinNevlaha/stratus-v2/projectidentity"
	"github.com/MartinNevlaha/stratus-v2/teams"
	"github.com/google/uuid"
)

func (s *Server) registerTeamAttachmentRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/team-attachments/join", s.handleTeamAttachmentJoin)
	mux.HandleFunc("GET /api/team-attachments/next", s.handleTeamAttachmentNext)
	mux.HandleFunc("POST /api/team-attachments/observe", s.handleTeamAttachmentObserve)
	mux.HandleFunc("POST /api/team-attachments/ack", s.handleTeamAttachmentAck)
	mux.HandleFunc("POST /api/team-attachments/result", s.handleTeamAttachmentResult)
	mux.HandleFunc("POST /api/team-attachments/fail", s.handleTeamAttachmentFail)
	mux.HandleFunc("POST /api/team-attachments/leave", s.handleTeamAttachmentLeave)
	mux.HandleFunc("POST /api/teams/{id}/attachments", s.handleTeamAttachmentAction)
	mux.HandleFunc("POST /api/team-request", s.handleTeamRequest)
}
func decodeAttachment(w http.ResponseWriter, r *http.Request, value any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(value); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return errors.New("only one JSON object is accepted")
	}
	return nil
}
func attachmentKey(teamID, memberID string) string { return teamID + "\x00" + memberID }

// Bootstrap is a private connector-to-server hop. Native identity is verified by
// the installed connector before this local request; model arguments never reach
// this endpoint. Browser origins and already-managed capabilities cannot pair.
func (s *Server) handleTeamAttachmentJoin(w http.ResponseWriter, r *http.Request) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() || r.Header.Get("Origin") != "" || r.Header.Get("X-Stratus-Team-Token") != "" {
		jsonErr(w, 403, "Joining requires the verified local native connector")
		return
	}
	if !s.teamReady(w) {
		return
	}
	var req struct {
		TeamID      string        `json:"team_id,omitempty"`
		MemberID    string        `json:"member_id,omitempty"`
		Runtime     string        `json:"runtime"`
		HostID      string        `json:"host_id"`
		NativeID    string        `json:"native_id"`
		ProjectRoot string        `json:"project_root"`
		Profile     teams.Profile `json:"profile,omitempty"`
		CreateTeam  bool          `json:"create_team"`
	}
	if err := decodeAttachment(w, r, &req); err != nil {
		jsonErr(w, 400, "Invalid attachment identity")
		return
	}
	if req.Runtime != "codex" || req.HostID != "local" || strings.TrimSpace(req.NativeID) == "" || len(req.NativeID) > 512 {
		jsonErr(w, 403, "This server requires a verified local Codex chat connector")
		return
	}
	project, root, err := projectidentity.Resolve(req.ProjectRoot)
	if err != nil {
		s.teamError(w, err)
		return
	}
	serverProject, serverRoot, err := s.teamProject()
	if err != nil {
		s.teamError(w, err)
		return
	}
	if project != serverProject || root != serverRoot {
		jsonErr(w, 403, "The original chat belongs to another project; connect its project server")
		return
	}
	if req.Profile.Runtime != "" && req.Profile.Runtime != req.Runtime {
		jsonErr(w, 403, "Native profile runtime does not match its host")
		return
	}
	req.Profile.Runtime = req.Runtime
	s.teamAttachmentJoinMu.Lock()
	defer s.teamAttachmentJoinMu.Unlock()
	var team *teams.Team
	if req.TeamID != "" {
		team, err = s.scopedTeam(req.TeamID)
		if err != nil {
			s.teamError(w, err)
			return
		}
	} else {
		list, e := s.teams.List(project)
		if e != nil {
			s.teamError(w, e)
			return
		}
		for i := range list {
			if list[i].State != "stopped" {
				if team != nil {
					jsonErr(w, 409, "More than one team exists for this project; select a team before joining")
					return
				}
				team = &list[i]
			}
		}
	}
	if team == nil && !req.CreateTeam {
		json200(w, map[string]any{"status": "needs_team", "project_id": project, "project_root": root, "instruction": "No active team exists for this project. Join with create_team=true to create one with this chat as lead and the configured role workers."})
		return
	}
	memberID := strings.TrimSpace(req.MemberID)
	if team != nil {
		for _, m := range team.Members {
			if a := m.Attachment; a != nil && a.Identity.Runtime == req.Runtime && a.Identity.HostID == req.HostID && a.Identity.NativeID == req.NativeID {
				if memberID != "" && memberID != m.ID {
					s.teamError(w, teams.ErrConflict)
					return
				}
				memberID = m.ID
			}
		}
		for _, m := range team.Members {
			if m.ID == memberID && (m.Attachment == nil || m.Attachment.Identity.NativeID != req.NativeID || m.Attachment.Identity.Runtime != req.Runtime || m.Attachment.Identity.HostID != req.HostID) {
				s.teamError(w, teams.ErrForbidden)
				return
			}
		}
	}
	if memberID == "" {
		memberID = "attached-" + uuid.NewString()
	}
	if len(memberID) > 200 || memberID == "user" {
		jsonErr(w, 400, "Invalid attachment member")
		return
	}
	binding := teams.AttachmentBinding{Identity: teams.AttachmentIdentity{Runtime: req.Runtime, HostID: req.HostID, NativeID: req.NativeID, ProjectID: project, ProjectRoot: root, Profile: req.Profile}, Capabilities: teams.AttachmentCapabilities{Queue: true, ObserveUserTurns: true, CorrelateResults: true, Wake: true}, Role: "member"}
	broker := newTeamAttachmentBroker(req.NativeID, req.Profile, 90*time.Second, nil)
	if team == nil {
		members := []teams.MemberConfig{{ID: memberID, Role: "lead", Profile: req.Profile}}
		members = append(members, teamAttachmentWorkers(root)...)
		binding.Role = "lead"
		team, err = s.teams.CreateAttached(r.Context(), teams.CreateRequest{ProjectID: project, ProjectRoot: root, Members: members, Limits: teams.Limits{MaxConcurrent: 2, MaxAutoRounds: 4, MaxTurns: 40}}, memberID, binding, broker)
	} else {
		err = s.teams.Attach(r.Context(), team.ID, memberID, binding, broker)
	}
	if err != nil {
		broker.Close()
		s.teamError(w, err)
		return
	}
	team, err = s.teams.Get(team.ID)
	if err != nil {
		broker.Close()
		s.teamError(w, err)
		return
	}
	var member *teams.Member
	for i := range team.Members {
		if team.Members[i].ID == memberID {
			member = &team.Members[i]
			break
		}
	}
	if member == nil || member.Attachment == nil {
		broker.Close()
		s.teamError(w, teams.ErrConflict)
		return
	}
	teamID, generation := team.ID, member.Attachment.Generation
	broker.configure(generation, func() {
		_ = s.teams.ObserveNativeTurn(teamID, memberID, generation, teams.NativeTurn{State: "disconnected"})
	})
	s.teamAttachmentsMu.Lock()
	if s.teamAttachments == nil {
		s.teamAttachments = map[string]*teamAttachmentBroker{}
	}
	s.teamAttachments[attachmentKey(teamID, memberID)] = broker
	s.teamAttachmentsMu.Unlock()
	w.Header().Set("Cache-Control", "no-store")
	json200(w, map[string]any{"team_id": team.ID, "member_id": member.ID, "generation": generation, "token": member.Token, "native_session_id": member.NativeID})
}
func (s *Server) attachmentIdentity(r *http.Request, generation uint64) (*teams.Team, *teams.Member, *teamAttachmentBroker, error) {
	team, member, err := s.teamIdentity(r)
	if err != nil {
		return nil, nil, nil, err
	}
	project, _, err := s.teamProject()
	if err != nil {
		return nil, nil, nil, err
	}
	if team.ProjectID != project || member.Attachment == nil || member.Attachment.State != "connected" || member.Attachment.Generation != generation || generation == 0 {
		return nil, nil, nil, teams.ErrConflict
	}
	s.teamAttachmentsMu.Lock()
	broker := s.teamAttachments[attachmentKey(team.ID, member.ID)]
	s.teamAttachmentsMu.Unlock()
	if broker == nil || !broker.live(generation) {
		return nil, nil, nil, teams.ErrConflict
	}
	return team, member, broker, nil
}
func (s *Server) handleTeamAttachmentNext(w http.ResponseWriter, r *http.Request) {
	generation, err := strconv.ParseUint(r.URL.Query().Get("generation"), 10, 64)
	if err != nil {
		jsonErr(w, 400, "Connection generation is required")
		return
	}
	_, _, broker, err := s.attachmentIdentity(r, generation)
	if err != nil {
		s.teamError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	input, err := broker.next(ctx)
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		s.teamError(w, err)
		return
	}
	json200(w, map[string]any{"input": input})
}
func (s *Server) handleTeamAttachmentObserve(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Generation uint64           `json:"generation"`
		Turn       teams.NativeTurn `json:"turn"`
	}
	if err := decodeAttachment(w, r, &req); err != nil {
		jsonErr(w, 400, "Invalid native observation")
		return
	}
	team, member, broker, err := s.attachmentIdentity(r, req.Generation)
	if err != nil {
		s.teamError(w, err)
		return
	}
	turn := req.Turn
	if turn.Result != nil {
		jsonErr(w, 400, "Native observations cannot report team results")
		return
	}
	// Before ACK, a native team turn is busy-state evidence only. It must block
	// scheduling without granting either a user assignment or team-task authority.
	if turn.Origin == "team" && !broker.acknowledged(turn.InputID, turn.ID) {
		if turn.InputID != "" {
			s.teamError(w, teams.ErrConflict)
			return
		}
		turn.Origin = "unknown"
	}
	if err = s.teams.ObserveNativeTurn(team.ID, member.ID, req.Generation, turn); err != nil {
		s.teamError(w, err)
		return
	}
	broker.observe(turn)
	json200(w, map[string]bool{"ok": true})
}

type attachmentReceiptRequest struct {
	Generation   uint64 `json:"generation"`
	TaskID       string `json:"task_id"`
	NativeTurnID string `json:"native_turn_id"`
	Text         string `json:"text,omitempty"`
	Error        string `json:"error,omitempty"`
}

func (s *Server) handleTeamAttachmentAck(w http.ResponseWriter, r *http.Request) {
	var req attachmentReceiptRequest
	if err := decodeAttachment(w, r, &req); err != nil {
		jsonErr(w, 400, "Invalid input acknowledgement")
		return
	}
	team, member, broker, err := s.attachmentIdentity(r, req.Generation)
	if err != nil {
		s.teamError(w, err)
		return
	}
	err = broker.ack(req.TaskID, req.NativeTurnID, func() error {
		return s.teams.ObserveNativeTurn(team.ID, member.ID, req.Generation, teams.NativeTurn{ID: req.NativeTurnID, InputID: req.TaskID, Origin: "team", State: "running"})
	})
	if err != nil {
		s.teamError(w, err)
		return
	}
	json200(w, map[string]bool{"ok": true})
}
func (s *Server) handleTeamAttachmentResult(w http.ResponseWriter, r *http.Request) {
	var req attachmentReceiptRequest
	if err := decodeAttachment(w, r, &req); err != nil {
		jsonErr(w, 400, "Invalid input result")
		return
	}
	_, _, broker, err := s.attachmentIdentity(r, req.Generation)
	if err != nil {
		s.teamError(w, err)
		return
	}
	if err = broker.complete(req.TaskID, req.NativeTurnID, req.Text, req.Error); err != nil {
		s.teamError(w, err)
		return
	}
	json200(w, map[string]bool{"ok": true})
}
func (s *Server) handleTeamAttachmentFail(w http.ResponseWriter, r *http.Request) {
	var req attachmentReceiptRequest
	if err := decodeAttachment(w, r, &req); err != nil {
		jsonErr(w, 400, "Invalid delivery failure")
		return
	}
	_, _, broker, err := s.attachmentIdentity(r, req.Generation)
	if err != nil {
		s.teamError(w, err)
		return
	}
	if err = broker.fail(req.TaskID, req.Error); err != nil {
		s.teamError(w, err)
		return
	}
	json200(w, map[string]bool{"ok": true})
}
func (s *Server) handleTeamAttachmentLeave(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Generation uint64 `json:"generation"`
	}
	if err := decodeAttachment(w, r, &req); err != nil {
		jsonErr(w, 400, "Invalid detach request")
		return
	}
	team, member, _, err := s.attachmentIdentity(r, req.Generation)
	if err != nil {
		s.teamError(w, err)
		return
	}
	if err = s.teams.Detach(r.Context(), team.ID, member.ID); err != nil {
		s.teamError(w, err)
		return
	}
	json200(w, map[string]bool{"ok": true})
}
func (s *Server) handleTeamAttachmentAction(w http.ResponseWriter, r *http.Request) {
	if !s.teamReady(w) {
		return
	}
	if r.Header.Get("X-Stratus-Team-Token") != "" {
		s.teamError(w, teams.ErrForbidden)
		return
	}
	team, err := s.scopedTeam(r.PathValue("id"))
	if err != nil {
		s.teamError(w, err)
		return
	}
	var req struct {
		Action   string `json:"action"`
		MemberID string `json:"member_id"`
	}
	if err := decodeAttachment(w, r, &req); err != nil {
		jsonErr(w, 400, "Invalid attachment action")
		return
	}
	switch req.Action {
	case "detach":
		err = s.teams.Detach(r.Context(), team.ID, req.MemberID)
	case "lead":
		err = s.teams.SetLead(r.Context(), team.ID, req.MemberID)
	default:
		err = fmt.Errorf("unknown attachment action")
	}
	if err != nil {
		s.teamError(w, err)
		return
	}
	team, err = s.teams.Get(team.ID)
	if err != nil {
		s.teamError(w, err)
		return
	}
	json200(w, publicTeam(*team))
}
func (s *Server) handleTeamRequest(w http.ResponseWriter, r *http.Request) {
	team, member, err := s.teamIdentity(r)
	if err != nil {
		s.teamError(w, err)
		return
	}
	var req struct {
		ID   string `json:"id,omitempty"`
		Text string `json:"text"`
	}
	if err := decodeAttachment(w, r, &req); err != nil {
		jsonErr(w, 400, "Invalid team request")
		return
	}
	task, err := s.teams.RequestFromAttached(r.Context(), team.ID, member.ID, req.ID, req.Text)
	if err != nil {
		s.teamError(w, err)
		return
	}
	json200(w, task)
}

// Personal workflow authority requires a currently observed native human turn;
// an attached worker assignment alone never grants the lead's permissions.
func attachedHumanScope(team *teams.Team, member *teams.Member) bool {
	if member.Attachment == nil || member.Attachment.State != "connected" || member.NativeTurn == nil || member.NativeTurn.Origin != "user" || member.NativeTurn.State != "running" {
		return false
	}
	for _, task := range team.Tasks {
		if task.ID == member.ActiveTaskID && task.To == member.ID && task.Origin == "external" && task.State == "running" && task.NativeTurnID == member.NativeTurn.ID {
			return true
		}
	}
	return false
}

// Creation uses the same project role templates as the dashboard. Explicit
// project fields also override the corresponding complex-workflow defaults.
func teamAttachmentWorkers(root string) []teams.MemberConfig {
	profiles, _ := teamProfiles(root)
	var config struct {
		ManagedTeam struct {
			Profiles map[string]map[string]teams.Profile `json:"profiles"`
		} `json:"managed_team"`
	}
	if raw, err := os.ReadFile(filepath.Join(root, ".stratus.json")); err == nil {
		_ = json.Unmarshal(raw, &config)
	}
	workers := make([]teams.MemberConfig, 0, 5)
	for _, role := range []string{"implementation", "design", "review", "diagnosis", "mechanical"} {
		profile := profiles["codex"][role]
		complex := teams.DefaultProfile("codex", role, "spec-complex", "complex")
		override := config.ManagedTeam.Profiles["codex"][role]
		if override.Model != "" {
			complex.Model = override.Model
		}
		if override.Effort != "" {
			complex.Effort = override.Effort
		}
		workers = append(workers, teams.MemberConfig{ID: role, Role: role, Profile: profile, ComplexProfile: &complex})
	}
	return workers
}
