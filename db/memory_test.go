package db

import "testing"

func TestBuildFTS5Query_MultipleTerms_ReturnsOR(t *testing.T) {
	got := buildFTS5Query("auth error")
	want := `"auth"* OR "error"*`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBuildFTS5Query_StopWordsOnly_ReturnsEmpty(t *testing.T) {
	got := buildFTS5Query("the")
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

func TestBuildFTS5Query_MultipleStopWordsOnly_ReturnsEmpty(t *testing.T) {
	got := buildFTS5Query("is a")
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

func TestBuildFTS5Query_MixedTerms_FiltersStopWords(t *testing.T) {
	got := buildFTS5Query("What is the architecture of this project")
	want := `"architecture"* OR "project"*`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBuildFTS5Query_EmptyInput_ReturnsEmpty(t *testing.T) {
	got := buildFTS5Query("")
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

func TestBuildFTS5Query_WhitespaceOnly_ReturnsEmpty(t *testing.T) {
	got := buildFTS5Query("   ")
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

func TestBuildFTS5Query_CaseInsensitiveStopWords(t *testing.T) {
	got := buildFTS5Query("The AND Or")
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

func TestBuildFTS5Query_QuotesEscaped(t *testing.T) {
	got := buildFTS5Query(`say "hello" world`)
	want := `"say"* OR """hello"""* OR "world"*`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBuildFTS5Query_SingleTerm_ReturnsSinglePart(t *testing.T) {
	got := buildFTS5Query("architecture")
	want := `"architecture"*`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
