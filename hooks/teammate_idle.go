package hooks

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// teammateIdleNudgeMarker identifies a nudge this hook already delivered. It is
// echoed back inside the teammate's transcript as an injected message, so finding
// it is how the hook avoids nudging the same assignment twice (and looping).
const teammateIdleNudgeMarker = "stratus: you went idle without reporting to the coordinator."

const teammateIdleNudgeReason = teammateIdleNudgeMarker + " Plain text you write as a named " +
	"teammate is discarded — it never reaches the coordinator, and finishing silently is " +
	"indistinguishable from crashing. Call SendMessage now with to: \"team-lead\" (fall back to " +
	"to: \"main\" if that is rejected) and put the report in the message body: what you actually " +
	"ran and verified, stated separately from what you assumed, plus anything that blocked or " +
	"failed. Send it even if the answer is \"done, nothing to report\"."

// maxTeammateNudges caps how many nudges one transcript may ever receive. The
// per-assignment guard alone is not enough: every inbound message counts as a new
// assignment, so a lead that merely acknowledges a report resets it and the hook
// nudges an agent that already reported — repeatedly. A teammate that ignored two
// nudges will not be moved by a third, and the coordinator sees the idle event
// anyway.
const maxTeammateNudges = 2

// workTools are the tools whose use means the teammate produced something the
// coordinator is waiting on. Read-only lookups (Read/Grep/Glob) are excluded: a
// teammate that only re-read files to answer a stale echo owes nobody a report.
var workTools = map[string]bool{
	"Edit":         true,
	"Write":        true,
	"MultiEdit":    true,
	"NotebookEdit": true,
	"Bash":         true,
}

// TeammateIdle fires when a CC Agent Teams teammate goes idle.
//
// The failure it exists to catch: the teammate finishes an assignment and writes
// its report as plain assistant text instead of calling SendMessage. That text is
// discarded, so the coordinator waits for a report that will never arrive while
// the teammate waits for work — a deadlock that only a human poke has been
// breaking, sometimes hours later. When the transcript shows real work since the
// last assignment and no SendMessage, the teammate is redirected to report.
//
// FAIL-OPEN by design: unlike the PreToolUse governance guards, a check that
// cannot read its input must never wedge a live agent.
func TeammateIdle(event HookEvent) Decision {
	if event.TeammateName == "" || event.TranscriptPath == "" {
		return Decision{Continue: true}
	}

	// transcript_path is the COORDINATOR's — the hook runs in the lead's process.
	// Judging that file judges the lead, not the teammate that went idle.
	transcript, ok := teammateTranscript(event)
	if !ok {
		return Decision{Continue: true}
	}

	scan, err := scanIdleTranscript(transcript)
	if err != nil {
		return Decision{Continue: true}
	}

	// Told twice already — a third time is spam, and spam masks the idle events
	// of teammates that really are stuck.
	if scan.nudges >= maxTeammateNudges {
		return Decision{Continue: true}
	}

	// Reported, owed nothing, or already told once for this assignment.
	if scan.sentMessage || scan.workActions == 0 || scan.nudgedSinceAssignment {
		return Decision{Continue: true}
	}

	return Decision{Nudge: true, Reason: teammateIdleNudgeReason}
}

// teammateTranscript resolves the JSONL transcript of the teammate this event
// fired for.
//
// Claude Code documents the TeammateIdle payload as "JSON with teammate_name and
// team_name"; the transcript_path it carries is the common session field, which
// for a hook running in the coordinator's process is the COORDINATOR's transcript.
// Reading it made every verdict a statement about the lead: measured over 269 real
// idle points on 2026-08-19 the hook agreed with the lead's state 269/269 — it
// nudged teammates that had already reported (up to 14 in a row, each answered with
// a duplicate report) and stayed silent for the ones that had finished in plain text.
//
// Teammate transcripts live beside the session file, one directory per session:
//
//	<dir>/<session>.jsonl                                     coordinator
//	<dir>/<session>/subagents/agent-a<name>-<hash>.jsonl       teammate
//	<dir>/<session>/subagents/agent-a<name>-<hash>.meta.json   {"name": "<name>", …}
//
// meta.json carries the authoritative name; the filename is the fallback for
// transcripts written without one. Returns false when nothing matches, which is
// the fail-open path — silence is better than judging a stranger's transcript.
func teammateTranscript(event HookEvent) (string, bool) {
	path := event.TranscriptPath
	// Already an agent transcript (or a future Claude Code that passes the right
	// file): take it as given.
	if strings.Contains(filepath.ToSlash(filepath.Dir(path)), "/subagents") {
		return path, true
	}

	dir := filepath.Join(
		filepath.Dir(path),
		strings.TrimSuffix(filepath.Base(path), ".jsonl"),
		"subagents",
	)

	if metas, err := filepath.Glob(filepath.Join(dir, "*.meta.json")); err == nil {
		for _, meta := range metas {
			raw, err := os.ReadFile(meta)
			if err != nil {
				continue
			}
			var parsed struct {
				Name string `json:"name"`
			}
			if json.Unmarshal(raw, &parsed) != nil || parsed.Name != event.TeammateName {
				continue
			}
			transcript := strings.TrimSuffix(meta, ".meta.json") + ".jsonl"
			if _, err := os.Stat(transcript); err == nil {
				return transcript, true
			}
		}
	}

	// No sidecar: fall back to the filename, newest first. Both spellings are
	// tried because the "a" is an id prefix, not part of the name.
	for _, pattern := range []string{"agent-a" + event.TeammateName + "-*.jsonl", "agent-" + event.TeammateName + "-*.jsonl"} {
		matches, err := filepath.Glob(filepath.Join(dir, pattern))
		if err != nil || len(matches) == 0 {
			continue
		}
		sort.Slice(matches, func(i, j int) bool {
			a, errA := os.Stat(matches[i])
			b, errB := os.Stat(matches[j])
			if errA != nil || errB != nil {
				return matches[i] < matches[j]
			}
			return a.ModTime().After(b.ModTime())
		})
		return matches[0], true
	}

	return "", false
}

// idleScan summarises the transcript for the nudge decision. workActions and
// nudgedSinceAssignment cover only the trailing segment (since the last inbound
// message); nudges and sentMessage deliberately survive that reset.
type idleScan struct {
	nudges                int
	sentMessage           bool
	nudgedSinceAssignment bool
	workActions           int
}

// scanIdleTranscript walks the whole transcript. Work is counted per assignment,
// so a teammate that ignored one nudge still gets a fresh one for the next task —
// but the total nudge count and the fact that the teammate reported at all are
// kept across assignments, because an inbound message is just as likely to be an
// acknowledgement of the last report as it is a new task.
func scanIdleTranscript(path string) (idleScan, error) {
	file, err := os.Open(path)
	if err != nil {
		return idleScan{}, err
	}
	defer file.Close()

	var scan idleScan
	scanner := bufio.NewScanner(file)
	// Transcript lines carry whole tool results and can be far longer than the
	// 64KB default; without this a big line ends the scan early and the hook
	// would silently judge a truncated segment.
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	for scanner.Scan() {
		var entry struct {
			Type    string `json:"type"`
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(scanner.Bytes(), &entry) != nil {
			continue
		}

		switch entry.Type {
		case "user":
			// Only a string payload is an inbound message; tool results arrive as
			// a content array and must not be treated as a new assignment.
			var text string
			if json.Unmarshal(entry.Message.Content, &text) != nil {
				continue
			}
			if strings.Contains(text, teammateIdleNudgeMarker) {
				scan.nudges++
				scan.nudgedSinceAssignment = true
				// A nudge asks for a report the teammate has not sent yet, so any
				// earlier report no longer excuses it.
				scan.sentMessage = false
				continue
			}
			if strings.Contains(text, "<teammate-message") {
				// New assignment: only the per-segment counters restart.
				scan.nudgedSinceAssignment = false
				scan.workActions = 0
			}
		case "assistant":
			var blocks []struct {
				Type string `json:"type"`
				Name string `json:"name"`
			}
			if json.Unmarshal(entry.Message.Content, &blocks) != nil {
				continue
			}
			for _, b := range blocks {
				if b.Type != "tool_use" {
					continue
				}
				if b.Name == "SendMessage" {
					scan.sentMessage = true
				}
				if workTools[b.Name] {
					scan.workActions++
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return idleScan{}, err
	}
	return scan, nil
}
