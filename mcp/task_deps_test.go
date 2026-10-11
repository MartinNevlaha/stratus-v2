package mcp

import (
	"reflect"
	"testing"
)

func TestObj_ArrayFieldCanDeclareItemSchema(t *testing.T) {
	schema := obj(optItems("deps", "nested ints", map[string]any{
		"type": "array", "items": map[string]any{"type": "integer"},
	}))
	props := schema["properties"].(map[string]any)
	items := props["deps"].(map[string]any)["items"].(map[string]any)
	if items["type"] != "array" {
		t.Fatalf("items = %v, want nested array schema", items)
	}
	// An untyped array keeps the old string items default.
	plain := obj(opt("tasks", "array", "titles"))["properties"].(map[string]any)["tasks"].(map[string]any)["items"].(map[string]any)
	if plain["type"] != "string" {
		t.Fatalf("plain items = %v", plain)
	}
}

func TestConvertDepsArg_AcceptsNestedAndStringEncoded(t *testing.T) {
	got, err := convertDepsArg([]any{[]any{}, []any{float64(0)}, "[0,1]"})
	if err != nil {
		t.Fatalf("convertDepsArg: %v", err)
	}
	want := [][]int{{}, {0}, {0, 1}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestConvertDepsArg_RejectsNonIntegers(t *testing.T) {
	if _, err := convertDepsArg([]any{[]any{"x"}}); err == nil {
		t.Error("expected error for non-integer dependency")
	}
	if got, err := convertDepsArg(nil); err != nil || got != nil {
		t.Errorf("nil input = %v, %v", got, err)
	}
}

func TestTransitionPhaseSchema_DeclaresTaskDependenciesAsNestedIntegers(t *testing.T) {
	tool, ok := newTestServer().tools["transition_phase"]
	if !ok {
		t.Fatal("transition_phase tool not registered")
	}
	items := tool.InputSchema["properties"].(map[string]any)["task_dependencies"].(map[string]any)["items"].(map[string]any)
	if items["type"] != "array" {
		t.Fatalf("task_dependencies items = %v", items)
	}
	inner := items["items"].(map[string]any)
	if inner["type"] != "integer" {
		t.Fatalf("inner items = %v", inner)
	}
}
