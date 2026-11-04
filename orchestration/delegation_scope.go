package orchestration

import "strings"

// RecordDelegationScoped binds an actual assignment to the dispatch-time owner
// and review scope. Validation and persistence share the coordinator lock.
func (c *Coordinator) RecordDelegationScoped(id, role, session, phase string, task *int, generation int64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	state, err := c.Get(id)
	if err != nil {
		return err
	}
	sameTask := state.CurrentTask == nil && task == nil || state.CurrentTask != nil && task != nil && *state.CurrentTask == *task
	if session == "" || state.SessionID != session || string(state.Phase) != phase || state.ReviewGeneration != generation || !sameTask || state.Aborted || state.Phase == PhaseComplete || !strings.HasPrefix(role, "delivery-") {
		return ErrOwnershipConflict
	}
	for _, old := range state.Delegated[phase] {
		if old == role {
			return nil
		}
	}
	if state.Delegated == nil {
		state.Delegated = map[string][]string{}
	}
	state.Delegated[phase] = append(state.Delegated[phase], role)
	return c.save(state)
}
