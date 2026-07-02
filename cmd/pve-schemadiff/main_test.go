package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const apidocJS = `const apiSchema = [
   { "path" : "/version", "info" : { "GET" : {} } },
   { "path" : "/nodes/{node}/qemu", "info" : { "GET" : {}, "POST" : {} } }
];`

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return p
}

func TestRunUpdateThenNoDrift(t *testing.T) {
	t.Parallel()
	apidoc := writeTemp(t, "apidoc.js", apidocJS)
	baseline := filepath.Join(t.TempDir(), "baseline.json")

	// -update writes the baseline and reports no drift.
	report, drift, err := run(apidoc, baseline, true)
	if err != nil {
		t.Fatalf("run(update): %v", err)
	}
	if drift {
		t.Error("run(update) drift = true, want false")
	}
	if !strings.Contains(report, "baseline updated") {
		t.Errorf("update output = %q, want 'baseline updated'", report)
	}

	// Diffing the same apidoc against the fresh baseline: no drift.
	report, drift, err = run(apidoc, baseline, false)
	if err != nil {
		t.Fatalf("run(diff): %v", err)
	}
	if drift {
		t.Errorf("run(diff) drift = true, want false; output=%q", report)
	}
}

func TestRunDetectsDrift(t *testing.T) {
	t.Parallel()
	apidoc := writeTemp(t, "apidoc.js", apidocJS)
	// Baseline missing the qemu POST endpoint -> apidoc has drift (added).
	baseline := writeTemp(t, "baseline.json", `[
	  {"method":"GET","path":"/version"},
	  {"method":"GET","path":"/nodes/{node}/qemu"}
	]`)

	report, drift, err := run(apidoc, baseline, false)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !drift {
		t.Fatal("run drift = false, want true")
	}
	if !strings.Contains(report, "+ POST /nodes/{node}/qemu") {
		t.Errorf("drift output = %q, want the added POST endpoint", report)
	}
}

func TestRunFileErrors(t *testing.T) {
	t.Parallel()
	if _, _, err := run("/nonexistent/apidoc.js", "/nonexistent/baseline.json", false); err == nil {
		t.Error("run(missing apidoc) error = nil, want non-nil")
	}
}
