package teams

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

// AttachmentIdentity must come from an authenticated native host, never from
// model-supplied chat IDs. Profile is the native profile, including unknowns.
type AttachmentIdentity struct {
	Runtime     string  `json:"runtime"`
	HostID      string  `json:"host_id"`
	NativeID    string  `json:"native_id"`
	ProjectID   string  `json:"project_id"`
	ProjectRoot string  `json:"project_root"`
	Profile     Profile `json:"profile"`
}
type AttachmentCapabilities struct {
	Queue            bool `json:"queue"`
	ObserveUserTurns bool `json:"observe_user_turns"`
	CorrelateResults bool `json:"correlate_results"`
	Wake             bool `json:"wake"`
}
type AttachmentBinding struct {
	ID           string                 `json:"id"`
	Identity     AttachmentIdentity     `json:"identity"`
	Generation   uint64                 `json:"generation"`
	Capabilities AttachmentCapabilities `json:"capabilities"`
	State        string                 `json:"state"`
	ExpiresAt    *time.Time             `json:"expires_at,omitempty"`
	Role         string                 `json:"role,omitempty"`
}

// NativeTurn is a host observation. Team input admission is only confirmed by
// a running turn carrying the exact submitted InputID. User turns have no InputID.
type NativeTurn struct {
	ID      string  `json:"id,omitempty"`
	InputID string  `json:"input_id,omitempty"`
	Origin  string  `json:"origin,omitempty"`
	State   string  `json:"state"`
	Result  *Result `json:"result,omitempty"`
}

func validateAttachment(memberID string, binding AttachmentBinding, session Session) (AttachmentBinding, error) {
	if session == nil || memberID == "" || memberID == "user" {
		return binding, fmt.Errorf("%w: member and verified session required", ErrForbidden)
	}
	identity := binding.Identity
	if identity.Runtime == "" || identity.HostID == "" || identity.NativeID == "" || identity.ProjectID == "" || !filepath.IsAbs(identity.ProjectRoot) {
		return binding, fmt.Errorf("%w: verified native identity and project required", ErrForbidden)
	}
	if identity.Profile.Runtime == "" {
		identity.Profile.Runtime = identity.Runtime
	}
	if identity.Profile.Runtime != identity.Runtime {
		return binding, fmt.Errorf("%w: native profile runtime mismatch", ErrForbidden)
	}
	caps := binding.Capabilities
	if !caps.Queue || !caps.ObserveUserTurns || !caps.CorrelateResults || !caps.Wake {
		return binding, fmt.Errorf("%w: native queue, user-turn observation, result correlation and wake capabilities required", ErrForbidden)
	}
	if binding.ExpiresAt != nil && !time.Now().Before(*binding.ExpiresAt) {
		return binding, fmt.Errorf("%w: connection expired", ErrConflict)
	}
	binding.Identity = identity
	if binding.ID == "" {
		binding.ID = uuid.NewString()
	}
	if binding.ExpiresAt != nil {
		expires := *binding.ExpiresAt
		binding.ExpiresAt = &expires
	}
	return binding, nil
}

// CreateAttached atomically creates a team and its already-verified native lead.
// Only owned members are preflighted; attached model/effort unknowns remain unknown.
func (m *Manager) CreateAttached(ctx context.Context, request CreateRequest, memberID string, binding AttachmentBinding, session Session) (*Team, error) {
	normalized, err := validateAttachment(memberID, binding, session)
	if err != nil {
		return nil, err
	}
	if normalized.Identity.ProjectID != request.ProjectID || filepath.Clean(normalized.Identity.ProjectRoot) != filepath.Clean(request.ProjectRoot) {
		return nil, ErrConflict
	}
	return m.create(ctx, request, memberID, &normalized, session)
}

// Attach installs a verified transport. Its Close and context cancellation must
// detach our transport without closing or interrupting the user's native chat.
// A zero generation requests the next generation; Get returns the assigned value.
func (m *Manager) Attach(ctx context.Context, teamID, memberID string, binding AttachmentBinding, session Session) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	binding, err := validateAttachment(memberID, binding, session)
	if err != nil {
		return err
	}
	identity := binding.Identity
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return ErrClosed
	}
	rec := m.teams[teamID]
	if rec == nil {
		m.mu.Unlock()
		return ErrNotFound
	}
	if rec.team.State == "stopped" || rec.team.ProjectID != identity.ProjectID || rec.team.ProjectRoot != filepath.Clean(identity.ProjectRoot) {
		m.mu.Unlock()
		return ErrConflict
	}
	for _, other := range m.teams {
		for i := range other.team.Members {
			v := &other.team.Members[i]
			a := v.Attachment
			if a == nil || a.State == "detached" || other.team.State == "stopped" || (other.team.ID == teamID && v.ID == memberID) {
				continue
			}
			if a.Identity.Runtime == identity.Runtime && a.Identity.HostID == identity.HostID && a.Identity.NativeID == identity.NativeID {
				m.mu.Unlock()
				return fmt.Errorf("%w: native chat already belongs to a team", ErrConflict)
			}
		}
	}
	member := findMember(rec.team, memberID)
	previousGeneration := uint64(0)
	oldSlot := m.slots[slotKey(teamID, memberID)]
	if oldSlot != nil && oldSlot.busy {
		m.mu.Unlock()
		return ErrConflict
	}
	if member != nil {
		if member.Attachment == nil && (member.NativeID != "" || oldSlot != nil || member.ActiveTaskID != "" || member.WorkflowID != "") {
			m.mu.Unlock()
			return ErrConflict
		}
		if member.Attachment != nil {
			oldIdentity := member.Attachment.Identity
			if oldIdentity.Runtime != identity.Runtime || oldIdentity.HostID != identity.HostID || oldIdentity.NativeID != identity.NativeID {
				m.mu.Unlock()
				return ErrConflict
			}
			previousGeneration = member.Attachment.Generation
		}
		for _, task := range rec.team.Tasks {
			if (task.To == memberID || task.From == memberID) && task.Origin != "external" && (task.State == "starting" || task.State == "running" || (member.Attachment == nil && task.State == "queued")) {
				m.mu.Unlock()
				return ErrConflict
			}
		}
	}
	if binding.Generation == 0 {
		binding.Generation = previousGeneration + 1
	}
	if binding.Generation <= previousGeneration {
		m.mu.Unlock()
		return ErrConflict
	}
	if binding.ID == "" {
		binding.ID = uuid.NewString()
	}
	binding.Identity = identity
	binding.State = "connected"
	before := cloneTeam(rec.team)
	if member == nil {
		if len(rec.team.Members) >= 128 {
			m.mu.Unlock()
			return fmt.Errorf("team worker capacity reached")
		}
		role := strings.TrimSpace(binding.Role)
		if role == "" {
			role = "member"
		}
		if CanonicalRole(role) == "lead" {
			for _, v := range rec.team.Members {
				if CanonicalRole(v.Role) == "lead" {
					m.mu.Unlock()
					return ErrConflict
				}
			}
		}
		rec.team.Members = append(rec.team.Members, Member{ID: memberID, Role: role, Configured: true})
		member = &rec.team.Members[len(rec.team.Members)-1]
	}
	m.endExternalTurnLocked(rec.team, member, "needs_user", "Connection was replaced; native history must be inspected.")
	member.Attachment = &binding
	member.NativeTurn = &NativeTurn{State: "unknown"}
	member.NativeID = identity.NativeID
	member.Profile = identity.Profile
	member.ProfileExplicit = true
	member.ObservedModel = identity.Profile.Model
	member.ObservedEffort = identity.Profile.Effort
	member.ComplexProfile = nil
	member.State = "waiting"
	member.Error = ""
	member.ActiveTaskID = ""
	member.Approval = nil
	member.PendingApprovals = nil
	member.ApprovalResponseID = ""
	member.Token = uuid.NewString()
	if err := m.saveLocked(rec); err != nil {
		rec.team = before
		m.mu.Unlock()
		return err
	}
	slotCtx, cancel := context.WithCancel(m.ctx)
	m.slots[slotKey(teamID, memberID)] = &runtimeSlot{ctx: slotCtx, cancel: cancel, session: session, attached: true, generation: binding.Generation}
	if oldSlot != nil {
		oldSlot.cancel()
	}
	m.signalLocked()
	m.mu.Unlock()
	if oldSlot != nil {
		m.closeSlot(ctx, oldSlot)
	}
	return nil
}

// SetLead changes the sole coordinator only at an unassigned workflow boundary.
func (m *Manager) SetLead(ctx context.Context, teamID, memberID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrClosed
	}
	rec := m.teams[teamID]
	if rec == nil {
		return ErrNotFound
	}
	member := findMember(rec.team, memberID)
	if member == nil {
		return ErrNotFound
	}
	if rec.team.State == "stopped" {
		return ErrConflict
	}
	for _, task := range rec.team.Tasks {
		if task.State == "queued" || task.State == "starting" || task.State == "running" || task.State == "waiting" {
			return ErrConflict
		}
	}
	for _, v := range rec.team.Members {
		if v.ActiveTaskID != "" || v.Approval != nil || (v.ID != memberID && v.WorkflowID != "") {
			return ErrConflict
		}
	}
	before := cloneTeam(rec.team)
	for i := range rec.team.Members {
		v := &rec.team.Members[i]
		if v.ID == memberID {
			v.Role = "lead"
		} else if CanonicalRole(v.Role) == "lead" {
			v.Role = "member"
		}
	}
	if err := m.saveLocked(rec); err != nil {
		rec.team = before
		return err
	}
	m.signalLocked()
	return nil
}

// Detach revokes authority and fences late observations without owning the host.
func (m *Manager) Detach(ctx context.Context, teamID, memberID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return ErrClosed
	}
	rec := m.teams[teamID]
	if rec == nil {
		m.mu.Unlock()
		return ErrNotFound
	}
	member := findMember(rec.team, memberID)
	if member == nil {
		m.mu.Unlock()
		return ErrNotFound
	}
	if member.Attachment == nil {
		m.mu.Unlock()
		return ErrConflict
	}
	before := cloneTeam(rec.team)
	m.revokeAttachmentLocked(rec.team, member, "detached", "Detached from native chat; queued work requires reconnection.")
	if err := m.saveLocked(rec); err != nil {
		rec.team = before
		m.mu.Unlock()
		return err
	}
	slot := m.slots[slotKey(teamID, memberID)]
	if slot != nil {
		slot.cancel()
		delete(m.slots, slotKey(teamID, memberID))
	}
	m.signalLocked()
	m.mu.Unlock()
	if slot != nil {
		m.closeSlot(ctx, slot)
	}
	return nil
}

func attachmentLive(member *Member) bool {
	a := member.Attachment
	return a != nil && a.State == "connected" && (a.ExpiresAt == nil || time.Now().Before(*a.ExpiresAt))
}
func attachmentIdle(member *Member) bool {
	return attachmentLive(member) && member.NativeTurn != nil && (member.NativeTurn.State == "idle" || member.NativeTurn.State == "complete" || member.NativeTurn.State == "failed" || member.NativeTurn.State == "cancelled")
}
func (m *Manager) activeAssignmentLocked(tm *Team, member *Member) *Task {
	task := findTask(tm, member.ActiveTaskID)
	if task == nil || task.State != "running" {
		return nil
	}
	if member.Attachment != nil {
		turn := member.NativeTurn
		if !attachmentLive(member) || turn == nil || turn.State != "running" || turn.ID == "" || (task.Origin == "external" && (turn.Origin != "user" || task.NativeTurnID != turn.ID)) || (task.Origin != "external" && (turn.Origin != "team" || turn.InputID != task.ID)) {
			return nil
		}
	}
	return task
}

// assignmentForRequestLocked accepts explicit team-task authority only after the
// current host acknowledged that input in the genuinely observed native turn.
func (m *Manager) assignmentForRequestLocked(tm *Team, member *Member, taskID string) *Task {
	if taskID == "" || taskID == member.ActiveTaskID {
		return m.activeAssignmentLocked(tm, member)
	}
	if member.Attachment == nil {
		return nil
	}
	turn := member.NativeTurn
	task := findTask(tm, taskID)
	if !attachmentLive(member) || turn == nil || turn.State != "running" || task == nil || task.To != member.ID || task.State != "running" || task.Origin == "external" || task.NativeTurnID == "" || task.NativeTurnID != turn.ID {
		return nil
	}
	return task
}

func (m *Manager) endExternalTurnLocked(tm *Team, member *Member, state, reason string) {
	if task := findTask(tm, member.ActiveTaskID); task != nil && task.Origin == "external" && task.State == "running" {
		now := time.Now().UTC()
		task.State = state
		task.Error = reason
		task.FinishedAt = &now
	}
}
func (m *Manager) revokeAttachmentLocked(tm *Team, member *Member, state, reason string) {
	m.endExternalTurnLocked(tm, member, "needs_user", reason)
	member.Attachment.Generation++
	member.Attachment.State = state
	member.Token = uuid.NewString()
	member.ActiveTaskID = ""
	member.NativeTurn = nil
	member.State = "needs_user"
	member.Error = reason
	member.Approval = nil
	member.PendingApprovals = nil
	member.ApprovalResponseID = ""
	for i := range tm.Tasks {
		task := &tm.Tasks[i]
		if task.To == member.ID && (task.State == "running" || task.State == "starting") {
			task.State = "needs_user"
			task.Error = "Native delivery outcome uncertain after disconnect; no automatic replay."
		}
	}
}

// ObserveNativeTurn is called only by the current verified connector. Generation
// fences disconnected transports; native IDs alone never grant team authority.
func (m *Manager) ObserveNativeTurn(teamID, memberID string, generation uint64, turn NativeTurn) error {
	m.mu.Lock()
	var closing *runtimeSlot
	defer func() {
		m.mu.Unlock()
		if closing != nil {
			m.closeSlot(context.Background(), closing)
		}
	}()
	if m.closed {
		return ErrClosed
	}
	rec := m.teams[teamID]
	if rec == nil {
		return ErrNotFound
	}
	tm := rec.team
	member := findMember(tm, memberID)
	if member == nil {
		return ErrNotFound
	}
	if !attachmentLive(member) || member.Attachment.Generation != generation || tm.State == "stopped" {
		return ErrConflict
	}
	slot := m.slots[slotKey(teamID, memberID)]
	if slot == nil || !slot.attached || slot.generation != generation {
		return ErrConflict
	}
	before := cloneTeam(tm)
	if turn.State == "disconnected" {
		m.revokeAttachmentLocked(tm, member, "disconnected", "Native connection disconnected; reconnect to resume team inputs.")
	} else if turn.State == "idle" {
		if member.NativeTurn != nil && member.NativeTurn.ID != "" && (member.NativeTurn.State == "running" || member.NativeTurn.State == "waiting" || member.NativeTurn.State == "needs_user") {
			return ErrConflict
		}
		member.NativeTurn = &turn
		if member.State != "needs_user" || member.Error == "" {
			member.State = "ready"
		}
	} else {
		if turn.Origin == "" {
			turn.Origin = "unknown"
		}
		if turn.ID == "" || len(turn.ID) > 512 || (turn.Origin != "user" && turn.Origin != "team" && turn.Origin != "unknown") {
			return ErrForbidden
		}
		terminal := turn.State == "complete" || turn.State == "failed" || turn.State == "cancelled"
		if !terminal && turn.State != "running" && turn.State != "waiting" && turn.State != "needs_user" {
			return ErrForbidden
		}
		current := member.NativeTurn
		if current != nil && current.ID != "" && current.ID != turn.ID && (current.State == "running" || current.State == "waiting" || current.State == "needs_user") {
			return ErrConflict
		}
		if turn.Origin == "unknown" {
			if turn.InputID != "" || turn.Result != nil {
				return ErrForbidden
			}
			if current != nil && current.ID == turn.ID && (current.Origin == "user" || current.Origin == "team") {
				// A compact host snapshot may omit the original input event. Keep
				// already-verified provenance for this exact native turn only.
				turn.Origin, turn.InputID = current.Origin, current.InputID
			} else {
				// Busy/terminal evidence alone has no delegation or workflow
				// authority and never creates a fictitious external user task.
				copy := turn
				member.NativeTurn = &copy
				member.ActiveTaskID = ""
				if member.State != "needs_user" || member.Error == "" {
					if terminal {
						member.State = "ready"
					} else if turn.State == "running" {
						member.State = "working"
					} else {
						member.State = turn.State
					}
				}
				if err := m.saveLocked(rec); err != nil {
					rec.team = before
					return err
				}
				m.signalLocked()
				return nil
			}
		}
		coalescedHuman := turn.Origin == "team" && current != nil && current.ID == turn.ID && current.Origin == "user" && nativeTurnActive(current)
		var task *Task
		if turn.Origin == "user" {
			if turn.InputID != "" {
				return ErrForbidden
			}
			id := fmt.Sprintf("external:%s:%d:%s", member.Attachment.ID, generation, turn.ID)
			task = findTask(tm, id)
			if task == nil {
				if terminal || len(tm.Tasks) >= 10000 {
					return ErrConflict
				}
				now := time.Now().UTC()
				tm.Tasks = append(tm.Tasks, Task{ID: id, Origin: "external", NativeTurnID: turn.ID, From: "user", To: member.ID, Role: member.Role, Kind: "external", State: "running", Profile: member.Profile, CreatedAt: now, StartedAt: &now})
				task = &tm.Tasks[len(tm.Tasks)-1]
			} else if task.State != "running" {
				if terminal && current != nil && current.ID == turn.ID {
					return nil
				}
				return ErrConflict
			}
		} else {
			completedNativeObservation := terminal && current != nil && current.ID == turn.ID && current.InputID == turn.InputID && current.Origin == "team"
			if turn.InputID == "" || ((!slot.busy || slot.taskID != turn.InputID) && !completedNativeObservation) {
				return ErrConflict
			}
			task = findTask(tm, turn.InputID)
			if task == nil || task.To != member.ID || task.Origin == "external" || ((task.State != "starting" && task.State != "running") && !completedNativeObservation) {
				return ErrConflict
			}
			if task.NativeTurnID != "" && task.NativeTurnID != turn.ID {
				return ErrConflict
			}
			if task.NativeTurnID == "" && terminal {
				return ErrConflict
			}
			if task.NativeTurnID == "" {
				if reason := budgetReason(tm); reason != "" {
					return fmt.Errorf("%w: %s", ErrConflict, reason)
				}
				now := time.Now().UTC()
				task.StartedAt = &now
				task.NativeTurnID = turn.ID
				setBatchState(tm, task.ID, "running")
				tm.Turns++
				if task.From != "user" {
					tm.AutoWakeups++
				}
			}
		}
		if coalescedHuman {
			// Native tool-output admission shares the human turn. Keep its authority
			// and workflow; explicit ParentTaskID selects the acknowledged input.
		} else if terminal {
			member.ActiveTaskID = ""
			if member.State != "needs_user" || member.Error == "" {
				member.State = "ready"
			}
			if turn.Origin == "user" {
				now := time.Now().UTC()
				task.State = turn.State
				task.FinishedAt = &now
				if turn.Result != nil {
					task.Result = turn.Result.Text
				}
			}
		} else {
			member.ActiveTaskID = task.ID
			member.WorkflowID = task.WorkflowID
			member.Revision = task.Revision
			// Manual authority and native activity remain observable while an
			// uncertain automatic delivery independently holds future dispatch.
			if member.State != "needs_user" || member.Error == "" {
				member.State = "working"
				if turn.State != "running" {
					member.State = turn.State
				}
			}
		}
		if !coalescedHuman {
			copy := turn
			copy.Result = nil
			member.NativeTurn = &copy
		}
	}
	if err := m.saveLocked(rec); err != nil {
		rec.team = before
		return err
	}
	if turn.State == "disconnected" {
		slot.cancel()
		closing = slot
	}
	m.signalLocked()
	return nil
}

// pendingTurns reserves the admission budget before an asynchronous native ACK.
// It is a reservation only: manual/native user turns never consume this budget.
func pendingTurns(tm *Team, exclude string) int {
	count := 0
	for _, task := range tm.Tasks {
		if task.ID != exclude && task.State == "starting" && task.BatchedInto == "" && task.Origin != "external" {
			count++
		}
	}
	return count
}

func (m *Manager) expireAttachments() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	var closing []*runtimeSlot
	for _, rec := range m.teams {
		changed := false
		for i := range rec.team.Members {
			member := &rec.team.Members[i]
			a := member.Attachment
			if a == nil || a.State != "connected" || a.ExpiresAt == nil || time.Now().Before(*a.ExpiresAt) {
				continue
			}
			m.revokeAttachmentLocked(rec.team, member, "expired", "Native connection expired; reconnect to resume team inputs.")
			if slot := m.slots[slotKey(rec.team.ID, member.ID)]; slot != nil {
				slot.cancel()
				closing = append(closing, slot)
			}
			changed = true
		}
		if changed {
			m.saveOrPauseLocked(rec)
			m.signalLocked()
		}
	}
	m.mu.Unlock()
	for _, slot := range closing {
		m.closeSlot(context.Background(), slot)
	}
}

func nativeTurnActive(turn *NativeTurn) bool {
	return turn != nil && (turn.State == "running" || turn.State == "waiting" || turn.State == "needs_user")
}

// RequestFromAttached forwards a genuine new human request through the sole lead
// and preserves its requester for the correlated result. It cannot transfer an
// existing requester-owned workflow or turn a team input into human authority.
func (m *Manager) RequestFromAttached(ctx context.Context, teamID, memberID, requestID, text string) (*Task, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, ErrClosed
	}
	rec := m.teams[teamID]
	if rec == nil {
		m.mu.Unlock()
		return nil, ErrNotFound
	}
	member := findMember(rec.team, memberID)
	if member == nil || member.Attachment == nil || CanonicalRole(member.Role) == "lead" {
		m.mu.Unlock()
		return nil, ErrForbidden
	}
	parent := m.activeAssignmentLocked(rec.team, member)
	if parent == nil || parent.Origin != "external" || member.NativeTurn.Origin != "user" {
		m.mu.Unlock()
		return nil, ErrForbidden
	}
	if parent.WorkflowID != "" {
		m.mu.Unlock()
		return nil, ErrConflict
	}
	var leadID string
	for _, candidate := range rec.team.Members {
		if CanonicalRole(candidate.Role) == "lead" {
			if leadID != "" {
				m.mu.Unlock()
				return nil, ErrConflict
			}
			leadID = candidate.ID
		}
	}
	if leadID == "" {
		m.mu.Unlock()
		return nil, ErrNotFound
	}
	parentID := parent.ID
	m.mu.Unlock()
	// Enqueue rechecks the captured parent under lock, so reconnects or a changed
	// native turn cannot race this validation into a new authority scope.
	return m.Enqueue(ctx, teamID, TaskRequest{ID: requestID, From: memberID, To: leadID, Role: "lead", Text: text, Kind: "task", ParentTaskID: parentID, attachedRequest: true})
}
