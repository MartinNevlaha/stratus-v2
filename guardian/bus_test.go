package guardian

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/events"
)

// fakeHub implements the hubBroadcaster interface for tests.
type fakeHub struct {
	mu   sync.Mutex
	msgs []broadcastedMsg
}

type broadcastedMsg struct {
	Type    string
	Payload any
}

func (h *fakeHub) BroadcastJSON(msgType string, payload any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.msgs = append(h.msgs, broadcastedMsg{Type: msgType, Payload: payload})
}

func (h *fakeHub) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.msgs)
}

func newTestGuardian(t *testing.T) (*Guardian, *events.InMemoryBus, *fakeHub) {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	hub := &fakeHub{}
	g := &Guardian{
		db:       database,
		hub:      hub,
		projRoot: t.TempDir(),
	}
	bus := events.NewInMemoryBus(64)
	t.Cleanup(bus.Close)
	g.SetEventBus(bus)
	return g, bus, hub
}

// waitForEvents blocks until len(got) >= want or the deadline is reached.
// Returns the accumulated events so the test can assert on content.
func waitForEvents(t *testing.T, got *[]events.Event, mu *sync.Mutex, want int) {
	t.Helper()
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(*got)
		mu.Unlock()
		if n >= want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	t.Fatalf("timed out waiting for %d events, got %d", want, len(*got))
}

func TestGuardian_PublishesAlertEmittedEvent(t *testing.T) {
	g, bus, _ := newTestGuardian(t)

	var got []events.Event
	var mu sync.Mutex
	bus.Subscribe(func(_ context.Context, evt events.Event) {
		if evt.Type == events.EventAlertEmitted {
			mu.Lock()
			got = append(got, evt)
			mu.Unlock()
		}
	})

	g.maybeEmit(alertInput{
		Type:     "memory_health",
		Severity: "info",
		Message:  "test alert",
		Metadata: map[string]any{"dedup_key": "test_dedup_1"},
	})

	waitForEvents(t, &got, &mu, 1)
	if got[0].Payload["type"] != "memory_health" {
		t.Errorf("payload type = %v, want memory_health", got[0].Payload["type"])
	}
	if _, ok := got[0].Payload["alert_id"].(int64); !ok {
		t.Errorf("payload alert_id missing or wrong type: %T", got[0].Payload["alert_id"])
	}
}

func TestGuardian_PublishesGovernanceViolationEvent(t *testing.T) {
	g, bus, _ := newTestGuardian(t)

	var alertEvt, govEvt int
	var mu sync.Mutex
	bus.Subscribe(func(_ context.Context, evt events.Event) {
		mu.Lock()
		defer mu.Unlock()
		switch evt.Type {
		case events.EventAlertEmitted:
			alertEvt++
		case events.EventGovernanceViolation:
			govEvt++
		}
	})

	g.maybeEmit(alertInput{
		Type:     "governance_violation",
		Severity: "warning",
		Message:  "bad file",
		Metadata: map[string]any{
			"dedup_key": "gov_test_file.go",
			"file":      "test_file.go",
			"rules":     "some-rule",
		},
	})

	// Give the async bus time to drain.
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if alertEvt != 1 {
		t.Errorf("EventAlertEmitted count = %d, want 1", alertEvt)
	}
	if govEvt != 1 {
		t.Errorf("EventGovernanceViolation count = %d, want 1", govEvt)
	}
}

func TestGuardian_PublishesCoverageDriftEvent(t *testing.T) {
	g, bus, _ := newTestGuardian(t)

	var covEvt events.Event
	var got int
	var mu sync.Mutex
	bus.Subscribe(func(_ context.Context, evt events.Event) {
		if evt.Type == events.EventCoverageDrift {
			mu.Lock()
			covEvt = evt
			got++
			mu.Unlock()
		}
	})

	g.maybeEmit(alertInput{
		Type:     "coverage_drift",
		Severity: "warning",
		Message:  "coverage dropped",
		Metadata: map[string]any{
			"dedup_key": "coverage_drift",
			"baseline":  80.0,
			"current":   76.5,
			"drop":      3.5,
		},
	})

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if got != 1 {
		t.Fatalf("EventCoverageDrift count = %d, want 1", got)
	}
	if covEvt.Payload["drop"] != 3.5 {
		t.Errorf("payload drop = %v, want 3.5", covEvt.Payload["drop"])
	}
	if covEvt.Payload["baseline"] != 80.0 {
		t.Errorf("payload baseline = %v, want 80.0", covEvt.Payload["baseline"])
	}
}

func TestGuardian_NonGovernanceAlertDoesNotEmitGovernanceEvent(t *testing.T) {
	g, bus, _ := newTestGuardian(t)

	var govEvt int
	var mu sync.Mutex
	bus.Subscribe(func(_ context.Context, evt events.Event) {
		if evt.Type == events.EventGovernanceViolation {
			mu.Lock()
			govEvt++
			mu.Unlock()
		}
	})

	g.maybeEmit(alertInput{
		Type:     "tech_debt",
		Severity: "warning",
		Message:  "debt grew",
		Metadata: map[string]any{"dedup_key": "tech_debt"},
	})

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if govEvt != 0 {
		t.Errorf("expected zero governance events, got %d", govEvt)
	}
}

func TestGuardian_HandlesAgentFailedEvent(t *testing.T) {
	_, bus, hub := newTestGuardian(t)

	evt := events.NewEvent(events.EventAgentFailed, "test", map[string]any{
		"agent_id": "delivery-backend-engineer",
	})
	if err := bus.Publish(context.Background(), evt); err != nil {
		t.Fatalf("publish: %v", err)
	}

	// Poll for the alert to land.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) && hub.count() == 0 {
		time.Sleep(5 * time.Millisecond)
	}

	if hub.count() == 0 {
		t.Fatal("expected guardian_alert broadcast after agent.failed event")
	}
}

func TestGuardian_HandlesReviewFailedEvent(t *testing.T) {
	_, bus, hub := newTestGuardian(t)

	evt := events.NewEvent(events.EventReviewFailed, "test", map[string]any{
		"workflow_id": "spec-thing",
	})
	if err := bus.Publish(context.Background(), evt); err != nil {
		t.Fatalf("publish: %v", err)
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) && hub.count() == 0 {
		time.Sleep(5 * time.Millisecond)
	}

	if hub.count() == 0 {
		t.Fatal("expected guardian_alert broadcast after review.failed event")
	}
}

func TestGuardian_IgnoresUnrelatedEvents(t *testing.T) {
	_, bus, hub := newTestGuardian(t)

	evt := events.NewEvent(events.EventWorkflowStarted, "test", map[string]any{
		"workflow_id": "x",
	})
	if err := bus.Publish(context.Background(), evt); err != nil {
		t.Fatalf("publish: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	if hub.count() != 0 {
		t.Errorf("expected no broadcasts for workflow.started, got %d", hub.count())
	}
}

func TestGuardian_NilBusIsSafe(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	hub := &fakeHub{}
	g := &Guardian{db: database, hub: hub, projRoot: t.TempDir()}
	// No SetEventBus — bus is nil.

	// maybeEmit must not panic when bus is nil.
	g.maybeEmit(alertInput{
		Type:     "memory_health",
		Severity: "info",
		Message:  "no bus test",
		Metadata: map[string]any{"dedup_key": "nil_bus_test"},
	})

	if hub.count() != 1 {
		t.Errorf("broadcast count = %d, want 1", hub.count())
	}
}

func TestGuardian_SetEventBusReplacesSubscription(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	hub := &fakeHub{}
	g := &Guardian{db: database, hub: hub, projRoot: t.TempDir()}

	bus1 := events.NewInMemoryBus(16)
	defer bus1.Close()
	bus2 := events.NewInMemoryBus(16)
	defer bus2.Close()

	g.SetEventBus(bus1)
	g.SetEventBus(bus2) // should unsubscribe from bus1

	// Publish on bus1 — nothing should happen.
	if err := bus1.Publish(context.Background(), events.NewEvent(events.EventAgentFailed, "t", map[string]any{"agent_id": "a"})); err != nil {
		t.Fatalf("publish bus1: %v", err)
	}
	time.Sleep(40 * time.Millisecond)
	if hub.count() != 0 {
		t.Errorf("should not have handled event on bus1 after switch; hub.count=%d", hub.count())
	}

	// Publish on bus2 — should fire.
	if err := bus2.Publish(context.Background(), events.NewEvent(events.EventAgentFailed, "t", map[string]any{"agent_id": "a"})); err != nil {
		t.Fatalf("publish bus2: %v", err)
	}
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) && hub.count() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if hub.count() == 0 {
		t.Error("should have handled event on bus2")
	}
}
