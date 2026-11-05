package teams

import (
	"context"
	"fmt"
	"log"
	"time"
)

// designTaskIDPrefix is reserved for the request IDs the design review automation creates.
const designTaskIDPrefix = "dr-"

const (
	maxDesignReviewRounds = 10
	maxDesignWaivers      = 200
)

// validateDesignReview checks the optional design review option of a new team and
// returns the configuration to store (nil when the option is absent or disabled).
func validateDesignReview(cfg *DesignReviewConfig, tm *Team, attachedMemberID string) (*DesignReviewConfig, error) {
	if cfg == nil {
		return nil, nil
	}
	// Nonsense is rejected even when the option is off, never silently dropped.
	if cfg.MaxRounds < 0 || cfg.MaxRounds > maxDesignReviewRounds {
		return nil, fmt.Errorf("design review max_rounds must be between 1 and %d (0 uses %d)", maxDesignReviewRounds, DefaultDesignReviewRounds)
	}
	if cfg.ReviewerMemberID == "" {
		if cfg.Enabled {
			return nil, fmt.Errorf("design review requires reviewer_member_id")
		}
		return nil, nil
	}
	reviewer := findMember(tm, cfg.ReviewerMemberID)
	if reviewer == nil {
		return nil, fmt.Errorf("design review reviewer %q is not a member of the team", cfg.ReviewerMemberID)
	}
	if CanonicalRole(reviewer.Role) == "lead" {
		return nil, fmt.Errorf("design review reviewer cannot be the team lead, who owns the workflow")
	}
	if reviewer.Attachment != nil || reviewer.ID == attachedMemberID {
		return nil, fmt.Errorf("design review reviewer cannot be an attached native chat")
	}
	if !cfg.Enabled {
		return nil, nil
	}
	out := *cfg
	if out.MaxRounds == 0 {
		out.MaxRounds = DefaultDesignReviewRounds
	}
	return &out, nil
}

// SetPurposeValidator verifies that a finished purpose task (for example a design
// review) still describes the current documents; the callback replaces the whole-tree
// revision check for such tasks. It runs outside the manager lock and must be bounded
// and read-only.
func (m *Manager) SetPurposeValidator(f func(Team, Task) bool) {
	m.mu.Lock()
	m.purposeValidator = f
	m.mu.Unlock()
}

// SetTaskFinishedHook registers a callback for purpose tasks (see PurposeDesignReview)
// that reached a terminal state. It runs once per finished turn on the goroutine that
// ran the task, after the manager lock was released, so it may call manager methods.
// A panic is logged and does not affect the manager.
func (m *Manager) SetTaskFinishedHook(f func(Team, Task)) {
	m.mu.Lock()
	m.finishedHook = f
	m.mu.Unlock()
}

func (m *Manager) fireFinished(snapshot *Team, ids []string) {
	if snapshot == nil {
		return
	}
	m.mu.Lock()
	hook := m.finishedHook
	m.mu.Unlock()
	if hook == nil {
		return
	}
	for _, id := range ids {
		task := findTask(snapshot, id)
		if task == nil || (task.State != "complete" && task.State != "needs_user" && task.State != "cancelled") {
			continue
		}
		safeFinishedHook(hook, *snapshot, *task)
	}
}

func safeFinishedHook(f func(Team, Task), tm Team, task Task) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("teams: task finished hook panicked (task %s): %v", task.ID, r)
		}
	}()
	f(tm, task)
}

func safeValidate(f func(Team, Task) bool, tm Team, task Task) (ok bool) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("teams: purpose validator panicked (task %s): %v", task.ID, r)
			ok = false
		}
	}()
	return f(tm, task)
}

// EnqueueSystem submits work on behalf of the team lead from server automation. Unlike
// Enqueue it does not require the lead to hold a running assignment, and it is the only
// way to set a task Purpose. The sender must be the team's lead.
func (m *Manager) EnqueueSystem(ctx context.Context, id string, r TaskRequest, purpose string) (*Task, error) {
	if purpose != PurposeDesignReview && purpose != PurposeDesignRework {
		return nil, fmt.Errorf("unknown task purpose %q", purpose)
	}
	r.systemRequest = true
	r.Purpose = purpose
	return m.Enqueue(ctx, id, r)
}

// RecordDesignReviewWaiver stores the user's decision to accept the documents with the
// given hash without a passing review. Recording the same workflow and hash again is a no-op.
func (m *Manager) RecordDesignReviewWaiver(teamID string, w DesignReviewWaiver) error {
	if w.WorkflowID == "" || w.DocHash == "" {
		return fmt.Errorf("waiver requires a workflow and a document hash")
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
	for _, existing := range rec.team.DesignReviewWaivers {
		if existing.WorkflowID == w.WorkflowID && existing.DocHash == w.DocHash {
			return nil
		}
	}
	if w.At.IsZero() {
		w.At = time.Now().UTC()
	}
	before := rec.team.DesignReviewWaivers
	waivers := append([]DesignReviewWaiver(nil), before...)
	waivers = append(waivers, w)
	if len(waivers) > maxDesignWaivers {
		waivers = waivers[len(waivers)-maxDesignWaivers:]
	}
	rec.team.DesignReviewWaivers = waivers
	if err := m.saveLocked(rec); err != nil {
		rec.team.DesignReviewWaivers = before
		return fmt.Errorf("persist design review waiver: %w", err)
	}
	m.signalLocked()
	return nil
}
