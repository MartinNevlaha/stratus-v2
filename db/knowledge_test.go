package db

import "testing"

func TestDeleteSolutionPattern_RemovesOnlyMatchingKey(t *testing.T) {
	database := openTestDB(t)

	keep := &SolutionPattern{ProblemClass: "bug", SolutionPattern: "retry", RepoType: "python", SuccessRate: 0.9, OccurrenceCount: 5}
	drop := &SolutionPattern{ProblemClass: "bug", SolutionPattern: "retry", RepoType: "go", SuccessRate: 1, OccurrenceCount: 5}
	for _, p := range []*SolutionPattern{keep, drop} {
		if err := database.SaveSolutionPattern(p); err != nil {
			t.Fatalf("SaveSolutionPattern: %v", err)
		}
	}

	if err := database.DeleteSolutionPattern("bug", "retry", "go"); err != nil {
		t.Fatalf("DeleteSolutionPattern: %v", err)
	}

	got, err := database.ListSolutionPatterns(SolutionPatternFilters{})
	if err != nil {
		t.Fatalf("ListSolutionPatterns: %v", err)
	}
	if len(got) != 1 || got[0].RepoType != "python" {
		t.Errorf("remaining patterns = %+v, want only the python row", got)
	}
}

func TestDeleteSolutionPattern_MissingKey(t *testing.T) {
	database := openTestDB(t)

	if err := database.DeleteSolutionPattern("bug", "nope", "go"); err != nil {
		t.Errorf("DeleteSolutionPattern on missing key: %v", err)
	}
}
