package generators

import (
	"testing"
)

func TestRegistry_AllowedCategories(t *testing.T) {
	gens := Registry([]string{"refactor_opportunity", "test_gap"}, false)
	if len(gens) != 2 {
		t.Fatalf("expected 2 generators, got %d", len(gens))
	}
	cats := map[string]bool{}
	for _, g := range gens {
		cats[g.Category()] = true
	}
	if !cats["refactor_opportunity"] {
		t.Error("missing refactor_opportunity generator")
	}
	if !cats["test_gap"] {
		t.Error("missing test_gap generator")
	}
}

func TestRegistry_PromptTuningDisabled(t *testing.T) {
	gens := Registry([]string{"prompt_tuning"}, false)
	if len(gens) != 0 {
		t.Errorf("expected 0 generators when stratusSelfEnabled=false, got %d", len(gens))
	}
}

func TestRegistry_PromptTuningEnabled(t *testing.T) {
	gens := Registry([]string{"prompt_tuning"}, true)
	if len(gens) != 1 {
		t.Fatalf("expected 1 generator when stratusSelfEnabled=true, got %d", len(gens))
	}
	if gens[0].Category() != "prompt_tuning" {
		t.Errorf("expected prompt_tuning, got %q", gens[0].Category())
	}
}

func TestRegistry_UnknownCategoryDropped(t *testing.T) {
	gens := Registry([]string{"unknown_category_xyz", "refactor_opportunity"}, false)
	if len(gens) != 1 {
		t.Fatalf("expected 1 generator, got %d", len(gens))
	}
	if gens[0].Category() != "refactor_opportunity" {
		t.Errorf("expected refactor_opportunity, got %q", gens[0].Category())
	}
}

func TestRegistry_EmptyCategories(t *testing.T) {
	gens := Registry([]string{}, false)
	if len(gens) != 0 {
		t.Errorf("expected 0 generators, got %d", len(gens))
	}
}

func TestRegistry_AllCategories(t *testing.T) {
	all := []string{
		"refactor_opportunity", "test_gap", "architecture_drift",
		"feature_idea", "dx_improvement", "doc_drift", "prompt_tuning",
	}
	gens := Registry(all, true)
	if len(gens) != len(all) {
		t.Errorf("expected %d generators, got %d", len(all), len(gens))
	}
}

// TestRegistry_DoesNotIncludeLegacyCategories is a regression test (T9) that
// ensures the three Stratus-self categories removed in T9 are never produced by
// any Generator, even when they are passed as allowedCategories.
func TestRegistry_DoesNotIncludeLegacyCategories(t *testing.T) {
	legacy := []string{"workflow_routing", "agent_selection", "threshold_adjustment"}

	// Test with stratusSelfEnabled=true to exercise all code paths.
	for _, stratusSelf := range []bool{false, true} {
		gens := Registry(legacy, stratusSelf)
		if len(gens) != 0 {
			t.Errorf("stratusSelfEnabled=%v: expected 0 generators for legacy categories, got %d", stratusSelf, len(gens))
		}
	}

	// Test that even when mixed with valid categories, legacy ones are silently dropped.
	mixed := append(legacy, "refactor_opportunity", "test_gap")
	gens := Registry(mixed, false)
	for _, g := range gens {
		cat := g.Category()
		for _, l := range legacy {
			if cat == l {
				t.Errorf("generator with legacy category %q must not be returned by Registry", cat)
			}
		}
	}
	if len(gens) != 2 {
		t.Errorf("expected 2 generators (refactor_opportunity + test_gap), got %d", len(gens))
	}
}
