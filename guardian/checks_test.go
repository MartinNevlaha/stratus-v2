package guardian

import (
	"strings"
	"testing"
)

func TestAlertMessage_EnglishDefault(t *testing.T) {
	msg := alertMessage("en", "stale_workflow", "My Workflow", "implement", 4)
	if !strings.Contains(msg, "My Workflow") {
		t.Errorf("expected workflow title in message, got: %s", msg)
	}
	if !strings.Contains(msg, "implement") {
		t.Errorf("expected phase in message, got: %s", msg)
	}
	if !strings.Contains(msg, "4") {
		t.Errorf("expected hours in message, got: %s", msg)
	}
}

func TestAlertMessage_SlovakStaleWorkflow(t *testing.T) {
	msg := alertMessage("sk", "stale_workflow", "Môj Workflow", "implement", 4)
	// Slovak translation must differ from English and contain the args.
	enMsg := alertMessage("en", "stale_workflow", "Môj Workflow", "implement", 4)
	if msg == enMsg {
		t.Errorf("Slovak message must differ from English, both: %s", msg)
	}
	if !strings.Contains(msg, "Môj Workflow") {
		t.Errorf("expected workflow title in Slovak message, got: %s", msg)
	}
	if !strings.Contains(msg, "implement") {
		t.Errorf("expected phase in Slovak message, got: %s", msg)
	}
	if !strings.Contains(msg, "4") {
		t.Errorf("expected hours in Slovak message, got: %s", msg)
	}
}

func TestAlertMessage_SlovakStaleWorker(t *testing.T) {
	msg := alertMessage("sk", "stale_worker", "worker-123", "backend", "5m0s")
	enMsg := alertMessage("en", "stale_worker", "worker-123", "backend", "5m0s")
	if msg == enMsg {
		t.Errorf("Slovak stale_worker message must differ from English")
	}
	if !strings.Contains(msg, "worker-123") {
		t.Errorf("expected worker ID in Slovak message, got: %s", msg)
	}
}

func TestAlertMessage_UnknownTypeFallsBack(t *testing.T) {
	// An unknown alert type should fall back gracefully (return non-empty string).
	msg := alertMessage("sk", "unknown_alert_type", "arg1")
	if msg == "" {
		t.Error("expected non-empty fallback message for unknown alert type")
	}
}

func TestAlertMessage_UnknownLangFallsBackToEnglish(t *testing.T) {
	enMsg := alertMessage("en", "stale_workflow", "WF", "plan", 2)
	unknownMsg := alertMessage("zz", "stale_workflow", "WF", "plan", 2)
	if unknownMsg != enMsg {
		t.Errorf("unknown lang should fall back to English, got %q (en: %q)", unknownMsg, enMsg)
	}
}

func TestAlertMessage_ReviewerTimeout_Slovak(t *testing.T) {
	msg := alertMessage("sk", "reviewer_timeout", "My Mission", 30)
	enMsg := alertMessage("en", "reviewer_timeout", "My Mission", 30)
	if msg == enMsg {
		t.Errorf("Slovak reviewer_timeout must differ from English")
	}
	if !strings.Contains(msg, "My Mission") {
		t.Errorf("expected mission title in Slovak message, got: %s", msg)
	}
}

func TestAlertMessage_TicketTimeout_Slovak(t *testing.T) {
	msg := alertMessage("sk", "ticket_timeout", "My Ticket", 30)
	enMsg := alertMessage("en", "ticket_timeout", "My Ticket", 30)
	if msg == enMsg {
		t.Errorf("Slovak ticket_timeout must differ from English")
	}
}

func TestAlertMessage_MemoryHealth_Slovak(t *testing.T) {
	msg := alertMessage("sk", "memory_health", 6000, 5000)
	enMsg := alertMessage("en", "memory_health", 6000, 5000)
	if msg == enMsg {
		t.Errorf("Slovak memory_health must differ from English")
	}
}

func TestAlertMessage_TechDebt_Slovak(t *testing.T) {
	msg := alertMessage("sk", "tech_debt", 5, 20, 25)
	enMsg := alertMessage("en", "tech_debt", 5, 20, 25)
	if msg == enMsg {
		t.Errorf("Slovak tech_debt must differ from English")
	}
}

func TestAlertMessage_CoverageDrift_Slovak(t *testing.T) {
	msg := alertMessage("sk", "coverage_drift", 3.5, 80.0, 76.5)
	enMsg := alertMessage("en", "coverage_drift", 3.5, 80.0, 76.5)
	if msg == enMsg {
		t.Errorf("Slovak coverage_drift must differ from English")
	}
}
