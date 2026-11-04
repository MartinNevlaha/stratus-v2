package teamruntime

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/MartinNevlaha/stratus-v2/teams"
	_ "modernc.org/sqlite"
)

// This crosses the adapter/manager boundary: a real native alias resolution
// must preserve the actual model without looking like an unapproved fallback.
func TestManagerAcceptsNativeClaudeAliasBinding(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "teams.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	changed := make(chan struct{}, 64)
	m, err := teams.NewManager(db, map[string]teams.Adapter{"claude-code": fixtureAdapter("claude-code")}, func() {
		select {
		case changed <- struct{}{}:
		default:
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tm, err := m.Create(ctx, teams.CreateRequest{ProjectID: "alias-fixture", ProjectRoot: t.TempDir(), Members: []teams.MemberConfig{{ID: "lead", Role: "lead", Profile: teams.Profile{Runtime: "claude-code", Model: "native-alias", Effort: "high"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Enqueue(ctx, tm.ID, teams.TaskRequest{ID: "alias-task", From: "user", To: "lead", Text: "second"}); err != nil {
		t.Fatal(err)
	}
	for {
		state, err := m.Get(tm.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, task := range state.Tasks {
			if task.ID != "alias-task" {
				continue
			}
			if task.State == "complete" {
				if state.Members[0].Profile.Model != "native-alias" || state.Members[0].ObservedModel != "claude-test" {
					t.Fatalf("requested and observed models were not preserved: %+v", state.Members[0])
				}
				return
			}
			if task.State == "needs_user" || task.State == "failed" {
				t.Fatalf("native alias rejected by manager: %+v", task)
			}
		}
		select {
		case <-changed:
		case <-ctx.Done():
			t.Fatalf("alias task never completed: %+v", state)
		}
	}
}
