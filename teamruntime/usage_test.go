package teamruntime

import (
	"strings"
	"testing"

	"github.com/MartinNevlaha/stratus-v2/teams"
)

func TestCodexUsage_AccumulatesAllResponsesWithinTurn(t *testing.T) {
	s := &streamSession{}
	r := teams.Result{NativeID: "thread"}
	var text strings.Builder
	for _, m := range []object{
		{"method": "thread/tokenUsage/updated", "params": object{"threadId": "thread", "turnId": "turn", "tokenUsage": object{"last": object{"inputTokens": float64(11), "outputTokens": float64(3)}, "total": object{"inputTokens": float64(111), "outputTokens": float64(53)}}}},
		{"method": "thread/tokenUsage/updated", "params": object{"threadId": "thread", "turnId": "turn", "tokenUsage": object{"last": object{"inputTokens": float64(7), "outputTokens": float64(2)}, "total": object{"inputTokens": float64(118), "outputTokens": float64(55)}}}},
	} {
		if _, err := s.codexEvent(m, "turn", &r, &text); err != nil {
			t.Fatal(err)
		}
	}
	if r.InputTokens == nil || *r.InputTokens != 18 || r.OutputTokens == nil || *r.OutputTokens != 5 {
		t.Fatalf("turn usage must include both responses, got %v/%v", tokenCount(r.InputTokens), tokenCount(r.OutputTokens))
	}
}
