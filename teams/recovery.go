package teams

import (
	"context"
	"fmt"
	"time"
)

// RecoverMember is an explicit dashboard-user decision after inspecting uncertain
// work. It closes the owned transport, retains native identity and old receipts,
// and permits new queued work. It never resubmits a needs_user task.
func (m *Manager) RecoverMember(ctx context.Context, teamID, memberID string) error {
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
	if member.Attachment != nil {
		m.mu.Unlock()
		return m.Detach(ctx, teamID, memberID)
	}
	slot := m.slots[slotKey(teamID, memberID)]
	uncertainApproval := member.ApprovalResponseID != ""
	if member.State != "needs_user" || (member.Approval != nil && !uncertainApproval) || rec.team.State == "stopped" || (slot != nil && slot.busy && !uncertainApproval) {
		m.mu.Unlock()
		return ErrConflict
	}
	member.State = "recovering"
	if err := m.saveLocked(rec); err != nil {
		member.State = "needs_user"
		m.mu.Unlock()
		return err
	}
	var session Session
	if slot != nil {
		slot.cancel()
		session = slot.session
		slot.session = nil
	}
	m.signalLocked()
	m.mu.Unlock()
	var closeErr error
	if session != nil {
		session.Interrupt(ctx)
		closeErr = session.Close()
	}
	// A failed approval reply may leave Submit waiting for the native process.
	// Wait for its cancellation receipt before making the member dispatchable.
	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for slot != nil {
		m.mu.Lock()
		busy := slot.busy
		m.mu.Unlock()
		if !busy {
			break
		}
		select {
		case <-waitCtx.Done():
			closeErr = fmt.Errorf("previous native turn has not stopped: %w", waitCtx.Err())
		case <-time.After(5 * time.Millisecond):
			continue
		}
		break
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	member = findMember(rec.team, memberID)
	if m.closed {
		return ErrClosed
	}
	if rec.team.State == "stopped" {
		return ErrConflict
	}
	if closeErr != nil {
		member.State = "needs_user"
		member.Error = "Cannot close previous owned transport: " + closeErr.Error()
		m.saveOrPauseLocked(rec)
		m.signalLocked()
		return fmt.Errorf("recover member: %w", closeErr)
	}
	delete(m.slots, slotKey(teamID, memberID))
	member.State = "ready"
	member.Error = ""
	member.ApprovalResponseID = ""
	member.PendingApprovals = nil
	member.ActiveTaskID = ""
	if err := m.saveLocked(rec); err != nil {
		member.State = "needs_user"
		rec.team.State = "paused"
		return err
	}
	m.signalLocked()
	return nil
}
