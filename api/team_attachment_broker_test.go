package api

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MartinNevlaha/stratus-v2/teams"
)

func TestAttachmentBrokerClaimOnceAndExplicitResult(t *testing.T) {
	b := newTeamAttachmentBroker("native", teams.Profile{Runtime: "codex"}, time.Minute, nil)
	defer b.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	completed := make(chan error, 1)
	go func() {
		r, e := b.Submit(ctx, teams.Input{ID: "task", Text: "work"})
		if e == nil && r.Text != "answer" {
			e = errors.New("wrong answer")
		}
		completed <- e
	}()
	claimCtx, cc := context.WithTimeout(ctx, time.Second)
	defer cc()
	input, err := b.next(claimCtx)
	if err != nil || input == nil || input.ID != "task" {
		t.Fatalf("claim %v %v", input, err)
	}
	poll, pc := context.WithTimeout(ctx, 5*time.Millisecond)
	defer pc()
	if again, e := b.next(poll); again != nil || !errors.Is(e, context.DeadlineExceeded) {
		t.Fatalf("replayed claim %v %v", again, e)
	}
	if e := b.complete("task", "turn", "answer", ""); !errors.Is(e, teams.ErrConflict) {
		t.Fatalf("unacknowledged completion: %v", e)
	}
	b.observe(teams.NativeTurn{ID: "turn", Origin: "user", State: "running"})
	if e := b.ack("task", "wrong", func() error { return nil }); !errors.Is(e, teams.ErrConflict) {
		t.Fatalf("wrong turn ack: %v", e)
	}
	if e := b.ack("task", "turn", func() error { return nil }); e != nil {
		t.Fatal(e)
	}
	b.observe(teams.NativeTurn{State: "idle"})
	select {
	case e := <-completed:
		t.Fatalf("native idle completed input: %v", e)
	default:
	}
	if e := b.complete("task", "wrong", "answer", ""); !errors.Is(e, teams.ErrConflict) {
		t.Fatalf("wrong turn completed: %v", e)
	}
	if e := b.complete("task", "turn", "answer", ""); e != nil {
		t.Fatal(e)
	}
	if e := <-completed; e != nil {
		t.Fatal(e)
	}
	if e := b.complete("task", "turn", "answer", ""); e != nil {
		t.Fatalf("duplicate receipt: %v", e)
	}
	if e := b.complete("task", "turn", "changed", ""); !errors.Is(e, teams.ErrConflict) {
		t.Fatalf("changed receipt accepted: %v", e)
	}
}
func TestAttachmentBrokerLeaseUnblocksAndNeverOwnsNative(t *testing.T) {
	expired := make(chan struct{}, 1)
	b := newTeamAttachmentBroker("native", teams.Profile{Runtime: "codex"}, 20*time.Millisecond, func() { expired <- struct{}{} })
	defer b.Close()
	done := make(chan error, 1)
	go func() { _, err := b.Submit(context.Background(), teams.Input{ID: "task", Text: "work"}); done <- err }()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := b.next(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-expired:
	case <-ctx.Done():
		t.Fatal("lease did not expire")
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("uncertain delivery succeeded")
		}
	case <-ctx.Done():
		t.Fatal("submit leaked")
	}
	if b.Interrupt(ctx) == nil || b.Approve(ctx, "approval", true) == nil {
		t.Fatal("attachment acquired native permissions")
	}
}
func TestAttachmentBrokerCancellationNeverReplays(t *testing.T) {
	b := newTeamAttachmentBroker("native", teams.Profile{Runtime: "codex"}, time.Minute, nil)
	defer b.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := b.Submit(ctx, teams.Input{ID: "task"}); done <- err }()
	wait, wc := context.WithTimeout(context.Background(), time.Second)
	defer wc()
	if _, err := b.next(wait); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := <-done; err == nil {
		t.Fatal("cancelled submit succeeded")
	}
	if _, err := b.next(wait); err == nil {
		t.Fatal("cancelled attachment remained available")
	}
}

func TestAttachmentBrokerNativeTerminalWithoutResultNeedsAttention(t *testing.T) {
	b := newTeamAttachmentBroker("native", teams.Profile{Runtime: "codex"}, time.Minute, nil)
	defer b.Close()
	done := make(chan error, 1)
	go func() { _, err := b.Submit(context.Background(), teams.Input{ID: "task"}); done <- err }()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := b.next(ctx); err != nil {
		t.Fatal(err)
	}
	b.observe(teams.NativeTurn{ID: "native-turn", Origin: "team", State: "running"})
	if err := b.ack("task", "native-turn", func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	b.observe(teams.NativeTurn{ID: "native-turn", Origin: "team", State: "complete"})
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("terminal falsely succeeded")
		}
	case <-ctx.Done():
		t.Fatal("missing result left task waiting indefinitely")
	}
	if err := b.complete("task", "native-turn", "late result", ""); !errors.Is(err, teams.ErrConflict) {
		t.Fatalf("late result accepted %v", err)
	}
}
