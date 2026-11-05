package db

import (
	"path/filepath"
	"testing"
)

func TestEvidenceRevision_RoundTrip(t *testing.T) {
	d := openTestDB(t)
	if err := d.CreateEvidence("e1", "t1", "m1", "gate", "go build: OK", "quality-gate", "pass", "abc1234"); err != nil {
		t.Fatalf("CreateEvidence: %v", err)
	}
	if err := d.CreateEvidence("e2", "t1", "m1", "note", "no revision", "a", "info", ""); err != nil {
		t.Fatalf("CreateEvidence: %v", err)
	}
	byTicket, err := d.ListEvidenceByTicket("t1")
	if err != nil || len(byTicket) != 2 {
		t.Fatalf("ListEvidenceByTicket = %+v, %v", byTicket, err)
	}
	byMission, err := d.ListEvidenceByMission("m1")
	if err != nil || len(byMission) != 2 {
		t.Fatalf("ListEvidenceByMission = %+v, %v", byMission, err)
	}
	for _, list := range [][]SwarmEvidence{byTicket, byMission} {
		for _, e := range list {
			want := map[string]string{"e1": "abc1234", "e2": ""}[e.ID]
			if e.Revision != want {
				t.Errorf("%s revision = %q, want %q", e.ID, e.Revision, want)
			}
		}
	}
}

func TestEvidenceRevisionMigration_AddsColumnToExistingTable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	d, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = d.CreateEvidence("legacy", "t1", "m1", "review", "ok", "rev", "approved", ""); err != nil {
		t.Fatal(err)
	}
	if _, err = d.SQL().Exec("ALTER TABLE swarm_evidence DROP COLUMN revision"); err != nil {
		t.Fatalf("drop column (simulating a pre-revision database): %v", err)
	}
	d.Close()

	d, err = Open(path)
	if err != nil {
		t.Fatalf("reopen old database: %v", err)
	}
	defer d.Close()
	ev, err := d.ListEvidenceByMission("m1")
	if err != nil || len(ev) != 1 || ev[0].ID != "legacy" || ev[0].Revision != "" {
		t.Fatalf("legacy evidence after migration = %+v, %v", ev, err)
	}
	if err = d.CreateEvidence("new", "t1", "m1", "gate", "x", "g", "pass", "deadbeef"); err != nil {
		t.Fatalf("CreateEvidence after migration: %v", err)
	}
}
