package orchestration

import (
	"errors"
	"fmt"
	"strings"
)

var ErrOwnershipConflict = errors.New("workflow runtime ownership conflict")
var ErrAmbiguousWorkflow = errors.New("AMBIGUOUS_WORKFLOW")

type WorkflowOwner struct{ Runtime, NativeSessionID string }

func SessionKey(o WorkflowOwner) (string, error) {
	if o.Runtime == "" {
		return o.NativeSessionID, nil
	}
	if o.Runtime != "codex" || strings.TrimSpace(o.NativeSessionID) == "" || strings.Contains(o.NativeSessionID, "${") {
		return "", fmt.Errorf("invalid runtime or native session identity")
	}
	return "codex:" + o.NativeSessionID, nil
}
func ResolveOwned(active []*WorkflowState, id string, o WorkflowOwner) (*WorkflowState, error) {
	key, err := SessionKey(o)
	if err != nil {
		return nil, err
	}
	var matches []*WorkflowState
	for _, w := range active {
		if id != "" && w.ID != id {
			continue
		}
		if w.Runtime == o.Runtime && w.SessionID == key && !w.Aborted && w.Phase != PhaseComplete {
			matches = append(matches, w)
		} else if id != "" {
			return nil, ErrOwnershipConflict
		}
	}
	if len(matches) > 1 {
		return nil, ErrAmbiguousWorkflow
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if id != "" {
		return nil, ErrWorkflowNotFound
	}
	return nil, nil
}
func (c *Coordinator) BindOwner(id string, o WorkflowOwner) (*WorkflowState, error) {
	key, e := SessionKey(o)
	if e != nil {
		return nil, e
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	w, e := c.Get(id)
	if e != nil {
		return nil, e
	}
	if w.Runtime != o.Runtime {
		return nil, ErrOwnershipConflict
	}
	w.SessionID = key
	w.RuntimeSessionID = o.NativeSessionID
	return w, c.save(w)
}
