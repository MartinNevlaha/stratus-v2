package insight

import (
	"context"
	"testing"
	"time"

	"github.com/MartinNevlaha/stratus-v2/config"
	"github.com/MartinNevlaha/stratus-v2/events"
)

// TestHandleEvent_GovernanceViolation_CreatesPairedProposal verifies that
// an incoming governance.violation event results in a persisted remediation
// proposal linked to the originating alert id.
func TestHandleEvent_GovernanceViolation_CreatesPairedProposal(t *testing.T) {
	database := setupTestDB(t)
	cfg := config.InsightConfig{Enabled: true, Interval: 1}
	e := NewEngine(database, cfg)

	ctx := context.Background()
	if err := e.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer e.Stop()

	const alertID int64 = 4242
	evt := events.NewEvent(events.EventGovernanceViolation, "guardian", map[string]any{
		"alert_id": alertID,
		"severity": "warning",
		"file":     "api/routes_thing.go",
		"rules":    "error-handling",
	})

	e.HandleEvent(ctx, evt)

	// handleGovernanceViolation runs as a goroutine; poll briefly.
	var found bool
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		proposals, err := database.ListInsightProposals("governance.remediation", "", "", 0, 10, 0)
		if err != nil {
			t.Fatalf("ListInsightProposals: %v", err)
		}
		if len(proposals) > 0 {
			found = true
			if proposals[0].SourcePatternID != "guardian-alert:4242" {
				t.Errorf("SourcePatternID = %q, want guardian-alert:4242", proposals[0].SourcePatternID)
			}
			if proposals[0].Title == "" {
				t.Error("proposal title should not be empty")
			}
			if proposals[0].Status != "detected" {
				t.Errorf("status = %q, want detected", proposals[0].Status)
			}
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if !found {
		t.Fatal("no governance remediation proposal was persisted within 1s")
	}
}

// TestHandleEvent_GovernanceViolation_FloatAlertIDAccepted ensures the handler
// is tolerant to bus encoders that round-trip int64 as float64.
func TestHandleEvent_GovernanceViolation_FloatAlertIDAccepted(t *testing.T) {
	database := setupTestDB(t)
	cfg := config.InsightConfig{Enabled: true, Interval: 1}
	e := NewEngine(database, cfg)

	ctx := context.Background()
	if err := e.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer e.Stop()

	evt := events.NewEvent(events.EventGovernanceViolation, "guardian", map[string]any{
		"alert_id": float64(7),
		"severity": "info",
		"file":     "x.go",
		"rules":    "some-rule",
	})
	e.HandleEvent(ctx, evt)

	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		proposals, err := database.ListInsightProposals("governance.remediation", "", "", 0, 10, 0)
		if err != nil {
			t.Fatalf("ListInsightProposals: %v", err)
		}
		if len(proposals) > 0 {
			if proposals[0].SourcePatternID != "guardian-alert:7" {
				t.Errorf("SourcePatternID = %q, want guardian-alert:7", proposals[0].SourcePatternID)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("expected a proposal to be created for float-encoded alert_id")
}

// TestHandleEvent_GovernanceViolation_StoppedEngineIsNoOp verifies the early
// return in HandleEvent when the engine has not been started.
func TestHandleEvent_GovernanceViolation_StoppedEngineIsNoOp(t *testing.T) {
	database := setupTestDB(t)
	cfg := config.InsightConfig{Enabled: true, Interval: 1}
	e := NewEngine(database, cfg)
	// Note: do NOT start the engine.

	evt := events.NewEvent(events.EventGovernanceViolation, "guardian", map[string]any{
		"alert_id": int64(1),
		"file":     "y.go",
	})
	e.HandleEvent(context.Background(), evt)

	time.Sleep(50 * time.Millisecond)
	proposals, err := database.ListInsightProposals("governance.remediation", "", "", 0, 10, 0)
	if err != nil {
		t.Fatalf("ListInsightProposals: %v", err)
	}
	if len(proposals) != 0 {
		t.Errorf("expected no proposals when engine is not started, got %d", len(proposals))
	}
}
