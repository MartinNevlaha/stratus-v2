package hooks

import (
	"os"
	"testing"
)

// Denials are appended to <data dir>/hook_denials.jsonl; without this the tests would write
// into the developer's real Stratus data directory.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "stratus-hooks-test-")
	if err != nil {
		panic(err)
	}
	os.Setenv("STRATUS_DATA_DIR", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
