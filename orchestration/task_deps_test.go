package orchestration

import "testing"

func TestSetTasksWithDeps_StoresDependencies(t *testing.T) {
	coord := newBugCoord(t)
	_, _ = coord.Start("spec-deps", WorkflowSpec, ComplexityComplex, "Deps")
	st, err := coord.SetTasksWithDeps("spec-deps", []string{"a", "b", "c"}, [][]int{nil, {0}, {0, 1}})
	if err != nil {
		t.Fatalf("SetTasksWithDeps: %v", err)
	}
	if len(st.Tasks[0].DependsOn) != 0 || len(st.Tasks[2].DependsOn) != 2 || st.Tasks[2].DependsOn[1] != 1 {
		t.Fatalf("tasks = %+v", st.Tasks)
	}
}

func TestSetTasksWithDeps_RejectsInvalid(t *testing.T) {
	cases := map[string][][]int{
		"self reference":    {{0}},
		"forward reference": {{1}, nil},
		"out of range":      {nil, {7}},
		"negative":          {nil, {-1}},
		"length mismatch":   {nil},
	}
	for name, deps := range cases {
		t.Run(name, func(t *testing.T) {
			coord := newBugCoord(t)
			_, _ = coord.Start("spec-bad", WorkflowSpec, ComplexityComplex, "Bad")
			if _, err := coord.SetTasksWithDeps("spec-bad", []string{"a", "b"}, deps); err == nil {
				t.Fatalf("expected error for %v", deps)
			}
		})
	}
}

func TestSetTasks_StillWorksWithoutDeps(t *testing.T) {
	coord := newBugCoord(t)
	_, _ = coord.Start("spec-plain", WorkflowSpec, ComplexitySimple, "Plain")
	st, err := coord.SetTasks("spec-plain", []string{"a", "b"})
	if err != nil || len(st.Tasks) != 2 || st.Tasks[1].DependsOn != nil {
		t.Fatalf("st=%+v err=%v", st, err)
	}
}
