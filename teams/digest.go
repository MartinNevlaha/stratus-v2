package teams

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	digestMaxTasks  = 8
	digestMaxRunes  = 1500
	digestLineRunes = 160
	digestIDRunes   = 40 // UUIDs are 36, so team_status lookups still work
	digestNameRunes = 32
)

var digestANSI = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

// lastTurnStart returns when the input of the member's most recent finished turn
// was handed over (that task's StartedAt), or the zero time when there is none.
// Only turns of the same workflow and phase as cur count: a member woken for
// other work gets the full brief and no digest. Using the start rather than the
// end of the previous turn keeps whatever peers did while that turn ran; the
// price is that the member's own decision from that turn can echo back as "new".
//
// Auto-acknowledged messages never started and are ignored. Attached members get
// StartedAt when their native turn begins (attachments.go), so they follow the
// same rule.
func lastTurnStart(tasks []Task, memberID string, cur *Task) time.Time {
	var start, finished time.Time
	for i := range tasks {
		t := &tasks[i]
		if t.To != memberID || t.ID == cur.ID || t.StartedAt == nil || t.FinishedAt == nil ||
			t.WorkflowID != cur.WorkflowID || t.WorkflowPhase != cur.WorkflowPhase {
			continue
		}
		if t.FinishedAt.After(finished) {
			finished, start = *t.FinishedAt, *t.StartedAt
		}
	}
	return start
}

// inputIDs returns the IDs that are already part of cur's input: cur, the tasks
// batched into it and the source tasks their results answer.
func inputIDs(tasks []Task, cur *Task) map[string]bool {
	ids := map[string]bool{cur.ID: true, cur.ParentTaskID: true}
	for i := range tasks {
		if tasks[i].BatchedInto == cur.ID {
			ids[tasks[i].ID], ids[tasks[i].ParentTaskID] = true, true
		}
	}
	return ids
}

// changeDigest lists, oldest first, up to digestMaxTasks teammate tasks created
// or finished after since, one sanitized line each, within digestMaxRunes. When
// the cap is hit the oldest lines are dropped and counted in a final "+N more"
// line; a line is never cut. It skips the member's own tasks and outgoing
// delegations, automatic result hand-backs (their source task is listed) and
// any ID in inInput (already part of this turn's input). It returns "" when
// nothing changed.
func changeDigest(tasks []Task, memberID string, inInput map[string]bool, since time.Time) string {
	type change struct {
		at   time.Time
		line string
	}
	var changes []change
	for i := range tasks {
		t := &tasks[i]
		if t.To == memberID || t.From == memberID || t.Kind == "result" || inInput[t.ID] {
			continue
		}
		at := t.CreatedAt
		if t.FinishedAt != nil && t.FinishedAt.After(at) {
			at = *t.FinishedAt
		}
		if !at.After(since) {
			continue
		}
		excerpt := t.Result
		if t.State == "needs_user" && t.Error != "" {
			excerpt = t.Error
		}
		if excerpt == "" {
			excerpt = t.Error
		}
		if excerpt == "" {
			excerpt = t.Text
		}
		changes = append(changes, change{at, fmt.Sprintf("- %s %s->%s %s: %s", cutRunes(digestLine(t.ID), digestIDRunes), cutRunes(digestLine(t.From), digestNameRunes), cutRunes(digestLine(t.To), digestNameRunes), digestLine(t.State), clipRunes(digestLine(excerpt), digestLineRunes))})
	}
	if len(changes) == 0 {
		return ""
	}
	sort.SliceStable(changes, func(i, j int) bool { return changes[i].at.Before(changes[j].at) })
	n := len(changes)
	for k := min(n, digestMaxTasks); ; k-- {
		lines := make([]string, 0, k+1)
		for _, c := range changes[n-k:] {
			lines = append(lines, c.line)
		}
		if k < n {
			lines = append(lines, fmt.Sprintf("+%d more", n-k))
		}
		if out := strings.Join(lines, "\n"); k == 1 || utf8.RuneCountInString(out) <= digestMaxRunes {
			return out
		}
	}
}

// digestLine turns stored text into a single safe line: ANSI escapes, control
// and zero-width/format runes are dropped and whitespace runs collapse to one
// space, so peer-written text cannot forge section headers.
func digestLine(s string) string {
	s = digestANSI.ReplaceAllString(s, "")
	s = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsSpace(r):
			return ' '
		case unicode.IsControl(r), unicode.Is(unicode.Cf, r):
			return -1
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

// clipRunes truncates s to at most n runes, marking the cut with an ellipsis.
func clipRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

// cutRunes keeps at most n runes of s, without a marker.
func cutRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}
