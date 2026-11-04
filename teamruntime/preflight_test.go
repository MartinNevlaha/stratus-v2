package teamruntime

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"
)

// Opt-in installed-runtime check: only starts owned native servers, initializes
// transports and reads their catalogs. It never sends a model prompt.
func TestInstalledNativePreflight(t *testing.T) {
	if os.Getenv("STRATUS_RUN_NATIVE_PREFLIGHT") != "1" {
		t.Skip("set STRATUS_RUN_NATIVE_PREFLIGHT=1 for no-model installed runtime handshakes")
	}
	for runtime, a := range Adapters() {
		t.Run(runtime, func(t *testing.T) {
			binary := runtime
			if runtime == "claude-code" {
				binary = "claude"
			}
			if _, err := exec.LookPath(binary); err != nil {
				t.Skip("runtime not installed")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
			defer cancel()
			available := a.Preflight(ctx, t.TempDir())
			if !available.Available {
				t.Fatalf("preflight: %s", available.Reason)
			}
			if len(available.Models) == 0 {
				t.Fatal("preflight advertised readiness without a model catalog")
			}
			t.Logf("%s: %d advertised models; %s", runtime, len(available.Models), available.Reason)
		})
	}
}
