package teams

import (
	"fmt"
	"strings"
)

// The request root is assigned only by RequestFromAttached and inherited through
// durable parent links. Ordinary team results retain their one-way semantics.
// Separate requests, and separate logical continuations within one request,
// must not share a native response merely because their workflow IDs are empty.
func sameRequestContinuation(tm *Team, a, b *Task) bool {
	if a.RequestTaskID != b.RequestTaskID {
		return false
	}
	if a.RequestTaskID == "" {
		return true
	}
	if a.Kind != "result" || b.Kind != "result" {
		return false
	}
	left, right := findTask(tm, a.ParentTaskID), findTask(tm, b.ParentTaskID)
	return left != nil && right != nil && left.ParentTaskID == right.ParentTaskID
}

// A finished native turn can still own unfinished logical work. A child counts
// as consumed only after its return turn (including further delegation) ends.
func requestChildrenState(tm *Team, parent *Task) (pending bool, failure string) {
	var failures []string
	for i := range tm.Tasks {
		child := &tm.Tasks[i]
		if child.ParentTaskID != parent.ID || child.Kind == "result" || child.RequestTaskID != parent.RequestTaskID {
			continue
		}
		switch child.State {
		case "queued", "starting", "running", "waiting":
			pending = true
			continue
		case "needs_user", "cancelled":
			failures = append(failures, fmt.Sprintf("Task %s: %s", child.ID, child.Error))
		}
		returned := findTask(tm, "result:"+child.ID)
		if returned == nil {
			// Acknowledgements are consumed without another native wakeup. A
			// held child that was never admitted has no return to consume.
			if child.State == "complete" && !(child.Kind == "message" && isAcknowledgement(child.Text)) {
				pending = true
			}
			continue
		}
		switch returned.State {
		case "queued", "starting", "running", "waiting":
			pending = true
		case "needs_user", "cancelled":
			failures = append(failures, fmt.Sprintf("Task %s: %s", returned.ID, returned.Error))
		}
	}
	return pending, strings.Join(failures, "; ")
}

func (m *Manager) finishRequestTaskLocked(tm *Team, taskID, returnText string) {
	task := findTask(tm, taskID)
	if task == nil || task.RequestTaskID == "" {
		return
	}
	if task.State == "complete" {
		pending, failure := requestChildrenState(tm, task)
		if pending {
			task.State = "waiting"
			task.FinishedAt = nil
			return
		}
		if failure != "" {
			task.State = "needs_user"
			task.Error = failure
			returnText = "Task " + task.ID + " needs user attention: " + failure + "\n\n" + returnText
		}
	}
	if task.State != "complete" && task.State != "needs_user" {
		return
	}
	if task.Kind != "result" {
		if task.ReturnTo != "" {
			// queueReturnLocked may grow the task slice; reacquire pointers after it.
			m.queueReturnLocked(tm, task, returnText)
			if returned := findTask(tm, "result:"+taskID); returned != nil && returned.State == "needs_user" {
				m.finishRequestTaskLocked(tm, returned.ID, returned.Error)
			}
		}
		return
	}
	child := findTask(tm, task.ParentTaskID)
	if child == nil {
		return
	}
	parent := findTask(tm, child.ParentTaskID)
	if parent == nil || parent.State != "waiting" || parent.To != task.To || parent.RequestTaskID != task.RequestTaskID {
		return
	}
	if pending, _ := requestChildrenState(tm, parent); pending && task.State == "complete" {
		return
	}
	// A failed continuation cannot finish its siblings while the native member
	// is held for inspection. Propagate attention immediately, never a success.
	parent.State = task.State
	parent.Result = task.Result
	parent.Error = task.Error
	parent.FinishedAt = task.FinishedAt
	m.finishRequestTaskLocked(tm, parent.ID, returnText)
}
