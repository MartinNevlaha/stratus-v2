package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// renderTranscript writes entries as a JSONL transcript at path.
func renderTranscript(t *testing.T, path string, entries ...map[string]any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create transcript dir: %v", err)
	}
	var b strings.Builder
	for _, e := range entries {
		line, err := json.Marshal(e)
		if err != nil {
			t.Fatalf("marshal transcript entry: %v", err)
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
}

// writeTranscript lays out a session the way Claude Code does — the coordinator's
// transcript at <dir>/<session>.jsonl and every teammate under
// <dir>/<session>/subagents/ — writes entries as teammate "prev-core"'s
// transcript, and returns the path the hook actually receives: the COORDINATOR's.
//
// The lead transcript is deliberately filled with work and no SendMessage, i.e.
// the exact state that used to make the hook nudge whoever went idle. Every test
// that expects no nudge therefore doubles as proof that the lead's activity is
// not what the hook judges.
func writeTranscript(t *testing.T, entries ...map[string]any) string {
	t.Helper()
	dir := t.TempDir()
	lead := filepath.Join(dir, "session-abc.jsonl")
	renderTranscript(t, lead,
		inbound("<teammate-message teammate_id=\"prev-core\">Report from a teammate</teammate-message>"),
		toolCall("Edit"),
		toolCall("Bash"),
		assistantText("Coordinator kept working and sent nothing."),
	)
	writeTeammateTranscript(t, dir, "session-abc", "prev-core", entries...)
	return lead
}

// writeTeammateTranscript creates one teammate transcript plus the meta.json
// sidecar Claude Code writes next to it.
func writeTeammateTranscript(t *testing.T, dir, session, name string, entries ...map[string]any) string {
	t.Helper()
	base := filepath.Join(dir, session, "subagents", "agent-a"+name+"-0123456789abcdef")
	renderTranscript(t, base+".jsonl", entries...)
	meta, err := json.Marshal(map[string]any{"name": name, "taskKind": "in_process_teammate", "teamName": "session-" + session})
	if err != nil {
		t.Fatalf("marshal meta: %v", err)
	}
	if err := os.WriteFile(base+".meta.json", meta, 0o644); err != nil {
		t.Fatalf("write meta: %v", err)
	}
	return base + ".jsonl"
}

func inbound(text string) map[string]any {
	return map[string]any{"type": "user", "message": map[string]any{"content": text}}
}

func toolCall(name string) map[string]any {
	return map[string]any{"type": "assistant", "message": map[string]any{
		"content": []any{map[string]any{"type": "tool_use", "name": name}},
	}}
}

func assistantText(text string) map[string]any {
	return map[string]any{"type": "assistant", "message": map[string]any{
		"content": []any{map[string]any{"type": "text", "text": text}},
	}}
}

func idleEvent(transcript string) HookEvent {
	return HookEvent{
		HookEventName:  "TeammateIdle",
		TeammateName:   "prev-core",
		TeamName:       "session-abc",
		TranscriptPath: transcript,
	}
}

func TestTeammateIdle_WorkedWithoutSendMessage_Nudges(t *testing.T) {
	transcript := writeTranscript(t,
		inbound("<teammate-message teammate_id=\"team-lead\">Do task T6</teammate-message>"),
		toolCall("Edit"),
		toolCall("Bash"),
		assistantText("All work for T6 is complete. Here is the summary."),
	)

	decision := TeammateIdle(idleEvent(transcript))

	if !decision.Nudge {
		t.Fatalf("expected a nudge when the teammate worked but never called SendMessage, got %#v", decision)
	}
	if !strings.Contains(decision.Reason, "SendMessage") {
		t.Fatalf("nudge reason must tell the agent to call SendMessage, got %q", decision.Reason)
	}
}

func TestTeammateIdle_ReportedViaSendMessage_Allows(t *testing.T) {
	transcript := writeTranscript(t,
		inbound("<teammate-message teammate_id=\"team-lead\">Do task T6</teammate-message>"),
		toolCall("Edit"),
		toolCall("SendMessage"),
		assistantText("Report sent."),
	)

	decision := TeammateIdle(idleEvent(transcript))

	if decision.Nudge || !decision.Continue {
		t.Fatalf("expected plain allow after the teammate reported, got %#v", decision)
	}
}

func TestTeammateIdle_NoWorkDone_Allows(t *testing.T) {
	transcript := writeTranscript(t,
		inbound("<teammate-message teammate_id=\"team-lead\">FYI, nothing for you</teammate-message>"),
		assistantText("Acknowledged — standing by."),
	)

	decision := TeammateIdle(idleEvent(transcript))

	if decision.Nudge {
		t.Fatalf("a pure acknowledgement owes no report, got %#v", decision)
	}
}

func TestTeammateIdle_AlreadyNudgedInSameSegment_DoesNotLoop(t *testing.T) {
	transcript := writeTranscript(t,
		inbound("<teammate-message teammate_id=\"team-lead\">Do task T6</teammate-message>"),
		toolCall("Edit"),
		inbound("TeammateIdle hook error: "+teammateIdleNudgeMarker+" call SendMessage"),
		assistantText("I still will not send it."),
	)

	decision := TeammateIdle(idleEvent(transcript))

	if decision.Nudge {
		t.Fatalf("must nudge at most once per assignment, got %#v", decision)
	}
}

// An ignored nudge still buys the next assignment a fresh one — the cap, not the
// assignment boundary, is what bounds the hook.
func TestTeammateIdle_NewAssignmentAfterIgnoredNudge_NudgesAgain(t *testing.T) {
	transcript := writeTranscript(t,
		inbound("<teammate-message teammate_id=\"team-lead\">Do task T6</teammate-message>"),
		toolCall("Edit"),
		inbound("TeammateIdle hook error: "+teammateIdleNudgeMarker+" call SendMessage"),
		assistantText("T6 is done, I will just say so here."),
		inbound("<teammate-message teammate_id=\"team-lead\">Now do T9</teammate-message>"),
		toolCall("Write"),
		assistantText("T9 done."),
	)

	decision := TeammateIdle(idleEvent(transcript))

	if !decision.Nudge {
		t.Fatalf("a fresh assignment after an ignored nudge gets a fresh nudge, got %#v", decision)
	}
}

// The loop this hook used to produce: a teammate reports, the lead acknowledges,
// the teammate runs one verification command and goes idle. The acknowledgement
// is indistinguishable from a new assignment, so before the fix it wiped the
// record of the report and the teammate was nudged to report what it had already
// reported — forever.
func TestTeammateIdle_AckAfterReport_DoesNotNudge(t *testing.T) {
	transcript := writeTranscript(t,
		inbound("<teammate-message teammate_id=\"team-lead\">Do task T6</teammate-message>"),
		toolCall("Edit"),
		toolCall("SendMessage"),
		inbound("<teammate-message teammate_id=\"team-lead\">Thanks, got it.</teammate-message>"),
		toolCall("Bash"),
		assistantText("Confirmed the migration applied."),
	)

	decision := TeammateIdle(idleEvent(transcript))

	if decision.Nudge {
		t.Fatalf("a teammate that already reported must not be nudged after an ack, got %#v", decision)
	}
}

// A teammate that reported after being nudged is trusted from then on: further
// inbound messages must not resurrect the nudge.
func TestTeammateIdle_ReportedAfterNudge_DoesNotNudgeAgain(t *testing.T) {
	transcript := writeTranscript(t,
		inbound("<teammate-message teammate_id=\"team-lead\">Do task T6</teammate-message>"),
		toolCall("Edit"),
		inbound("TeammateIdle hook error: "+teammateIdleNudgeMarker+" call SendMessage"),
		toolCall("SendMessage"),
		inbound("<teammate-message teammate_id=\"team-lead\">Thanks.</teammate-message>"),
		toolCall("Bash"),
		assistantText("Standing by."),
	)

	decision := TeammateIdle(idleEvent(transcript))

	if decision.Nudge {
		t.Fatalf("the report after the nudge settles the debt, got %#v", decision)
	}
}

// Hard stop: two nudges is all one transcript ever gets, however many
// assignments arrive afterwards.
func TestTeammateIdle_NudgeCapReached_Allows(t *testing.T) {
	transcript := writeTranscript(t,
		inbound("<teammate-message teammate_id=\"team-lead\">Do task T6</teammate-message>"),
		toolCall("Edit"),
		inbound("TeammateIdle hook error: "+teammateIdleNudgeMarker+" call SendMessage"),
		inbound("<teammate-message teammate_id=\"team-lead\">Now do T9</teammate-message>"),
		toolCall("Write"),
		inbound("TeammateIdle hook error: "+teammateIdleNudgeMarker+" call SendMessage"),
		inbound("<teammate-message teammate_id=\"team-lead\">Now do T12</teammate-message>"),
		toolCall("Write"),
		assistantText("T12 done, still not sending anything."),
	)

	decision := TeammateIdle(idleEvent(transcript))

	if decision.Nudge {
		t.Fatalf("nudges must be capped at %d per transcript, got %#v", maxTeammateNudges, decision)
	}
}

func TestTeammateIdle_ReadOnlyLookupIsNotWork(t *testing.T) {
	transcript := writeTranscript(t,
		inbound("<teammate-message teammate_id=\"team-lead\">Stale echo of an old task</teammate-message>"),
		toolCall("Read"),
		toolCall("Grep"),
		assistantText("These are duplicate echoes of tasks I already reported."),
	)

	decision := TeammateIdle(idleEvent(transcript))

	if decision.Nudge {
		t.Fatalf("read-only verification is not a reportable deliverable, got %#v", decision)
	}
}

// Fail open: a missing or unreadable transcript must never wedge an agent.
func TestTeammateIdle_UnreadableTranscript_Allows(t *testing.T) {
	decision := TeammateIdle(idleEvent("/nonexistent/transcript.jsonl"))

	if decision.Nudge || !decision.Continue {
		t.Fatalf("expected fail-open allow on unreadable transcript, got %#v", decision)
	}
}

func TestTeammateIdle_NotATeammate_Allows(t *testing.T) {
	transcript := writeTranscript(t, toolCall("Edit"))

	decision := TeammateIdle(HookEvent{HookEventName: "TeammateIdle", TranscriptPath: transcript})

	if decision.Nudge || !decision.Continue {
		t.Fatalf("expected allow when no teammate_name is present, got %#v", decision)
	}
}

// A nudge must be delivered as a stop-hook "block" decision. Emitting
// {"continue": false} would make Claude Code stop the agent outright, which is
// the opposite of what this hook is for.
func TestNudgeDecisionSerialisesAsStopHookBlock(t *testing.T) {
	payload := nudgePayload("do the thing")

	var got map[string]any
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("nudge payload is not valid JSON: %v", err)
	}
	if got["decision"] != "block" {
		t.Fatalf("expected decision=block, got %#v", got)
	}
	if got["reason"] != "do the thing" {
		t.Fatalf("expected reason to carry the nudge text, got %#v", got)
	}
	if _, present := got["continue"]; present {
		t.Fatalf("nudge must not emit a `continue` field — it would stop the agent: %#v", got)
	}
}

// The defect this resolution exists for, measured on 2026-08-19: Claude Code
// passes the COORDINATOR's transcript in transcript_path, so the hook used to
// judge the lead. Backtested over 269 real idle points it agreed with the lead's
// state 269/269 — it nudged teammates that had reported (up to 14 times in a row,
// each producing a duplicate report) and stayed silent for the ones that had not.
func TestTeammateIdle_JudgesTeammateNotCoordinator(t *testing.T) {
	// Teammate reported; the coordinator (written by writeTranscript) worked and
	// sent nothing. Judging the coordinator would nudge here.
	transcript := writeTranscript(t,
		inbound("<teammate-message teammate_id=\"team-lead\">Do task T6</teammate-message>"),
		toolCall("Edit"),
		toolCall("SendMessage"),
		assistantText("Report sent."),
	)

	decision := TeammateIdle(idleEvent(transcript))

	if decision.Nudge {
		t.Fatalf("a teammate that reported must not be nudged for the coordinator's silence, got %#v", decision)
	}
}

func TestTeammateIdle_PicksTheIdlingTeammateNotAnother(t *testing.T) {
	dir := t.TempDir()
	lead := filepath.Join(dir, "session-abc.jsonl")
	renderTranscript(t, lead, toolCall("SendMessage"))
	// A busy sibling that reported must not excuse the silent one.
	writeTeammateTranscript(t, dir, "session-abc", "other-agent",
		inbound("<teammate-message teammate_id=\"team-lead\">Do T1</teammate-message>"),
		toolCall("Edit"),
		toolCall("SendMessage"),
	)
	writeTeammateTranscript(t, dir, "session-abc", "prev-core",
		inbound("<teammate-message teammate_id=\"team-lead\">Do T6</teammate-message>"),
		toolCall("Edit"),
		assistantText("T6 done — writing the report here as plain text."),
	)

	decision := TeammateIdle(idleEvent(lead))

	if !decision.Nudge {
		t.Fatalf("expected the silent teammate to be nudged, got %#v", decision)
	}
}

// meta.json is the authoritative name, but a transcript without one is still
// found by its filename.
func TestTeammateIdle_ResolvesTeammateWithoutMetaSidecar(t *testing.T) {
	dir := t.TempDir()
	lead := filepath.Join(dir, "session-abc.jsonl")
	renderTranscript(t, lead, toolCall("SendMessage"))
	renderTranscript(t, filepath.Join(dir, "session-abc", "subagents", "agent-aprev-core-0123456789abcdef.jsonl"),
		inbound("<teammate-message teammate_id=\"team-lead\">Do T6</teammate-message>"),
		toolCall("Bash"),
		assistantText("Done, telling you here."),
	)

	decision := TeammateIdle(idleEvent(lead))

	if !decision.Nudge {
		t.Fatalf("expected the teammate to be found by filename, got %#v", decision)
	}
}

// Fail open, loudly-silent: a teammate whose transcript cannot be located is
// never nudged on the strength of somebody else's transcript.
func TestTeammateIdle_TeammateTranscriptNotFound_Allows(t *testing.T) {
	dir := t.TempDir()
	lead := filepath.Join(dir, "session-abc.jsonl")
	renderTranscript(t, lead,
		inbound("<teammate-message teammate_id=\"prev-core\">anything</teammate-message>"),
		toolCall("Edit"),
	)

	decision := TeammateIdle(idleEvent(lead))

	if decision.Nudge || !decision.Continue {
		t.Fatalf("expected fail-open allow when the teammate transcript is missing, got %#v", decision)
	}
}

// If Claude Code ever passes the teammate's own transcript, take it as given.
func TestTeammateIdle_SubagentTranscriptPathUsedAsIs(t *testing.T) {
	dir := t.TempDir()
	transcript := writeTeammateTranscript(t, dir, "session-abc", "prev-core",
		inbound("<teammate-message teammate_id=\"team-lead\">Do T6</teammate-message>"),
		toolCall("Edit"),
		assistantText("T6 done — plain text again."),
	)

	decision := TeammateIdle(idleEvent(transcript))

	if !decision.Nudge {
		t.Fatalf("expected a direct agent transcript to be judged as-is, got %#v", decision)
	}
}
