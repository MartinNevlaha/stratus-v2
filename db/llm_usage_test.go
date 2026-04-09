package db

import "testing"

func TestRecordTokenUsage_Insert(t *testing.T) {
	d := openTestDB(t)
	err := d.RecordTokenUsage("2026-04-09", "insight", 100, 50)
	if err != nil {
		t.Fatalf("RecordTokenUsage: %v", err)
	}

	input, output, err := d.GetDailyTokenUsage("2026-04-09", "insight")
	if err != nil {
		t.Fatalf("GetDailyTokenUsage: %v", err)
	}
	if input != 100 {
		t.Errorf("input_tokens = %d, want 100", input)
	}
	if output != 50 {
		t.Errorf("output_tokens = %d, want 50", output)
	}
}

func TestRecordTokenUsage_Upsert(t *testing.T) {
	d := openTestDB(t)
	// First insert
	err := d.RecordTokenUsage("2026-04-09", "wiki_engine", 100, 50)
	if err != nil {
		t.Fatal(err)
	}
	// Second insert — should accumulate
	err = d.RecordTokenUsage("2026-04-09", "wiki_engine", 200, 100)
	if err != nil {
		t.Fatal(err)
	}

	input, output, err := d.GetDailyTokenUsage("2026-04-09", "wiki_engine")
	if err != nil {
		t.Fatal(err)
	}
	if input != 300 {
		t.Errorf("input_tokens = %d, want 300", input)
	}
	if output != 150 {
		t.Errorf("output_tokens = %d, want 150", output)
	}
}

func TestGetDailyTokenUsageTotal(t *testing.T) {
	d := openTestDB(t)
	_ = d.RecordTokenUsage("2026-04-09", "wiki_engine", 100, 50)
	_ = d.RecordTokenUsage("2026-04-09", "evolution_loop", 200, 100)

	input, output, err := d.GetDailyTokenUsageTotal("2026-04-09")
	if err != nil {
		t.Fatal(err)
	}
	if input != 300 {
		t.Errorf("total input = %d, want 300", input)
	}
	if output != 150 {
		t.Errorf("total output = %d, want 150", output)
	}
}

func TestGetDailyTokenUsage_NoRows(t *testing.T) {
	d := openTestDB(t)
	input, output, err := d.GetDailyTokenUsage("2026-04-09", "nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if input != 0 || output != 0 {
		t.Errorf("expected zeros, got %d/%d", input, output)
	}
}

func TestGetTokenUsageHistory(t *testing.T) {
	d := openTestDB(t)
	_ = d.RecordTokenUsage("2026-04-09", "wiki_engine", 100, 50)
	_ = d.RecordTokenUsage("2026-04-08", "guardian", 200, 100)

	rows, err := d.GetTokenUsageHistory(7)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Errorf("got %d rows, want 2", len(rows))
	}
}
