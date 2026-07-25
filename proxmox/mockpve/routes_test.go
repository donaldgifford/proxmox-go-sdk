package mockpve_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// directRegistration is the call the built-in routes must never make: it
// registers a pattern on the mux without recording it, so Server.Routes()
// would silently under-report the covered surface. Assembled at runtime so
// this file's own source does not count as a hit.
var directRegistration = "s.mux." + "HandleFunc("

// Every built-in route must register through Server.handle so it lands in
// Routes(). The only permitted direct mux.HandleFunc call is the one inside
// handle itself (server.go) — a new per-service file that bypasses the helper
// fails here rather than silently deflating the coverage numerator.
func TestNoDirectMuxRegistrations(t *testing.T) {
	t.Parallel()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Clean(name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		got := strings.Count(string(src), directRegistration)
		want := 0
		if name == "server.go" {
			want = 1 // the single call inside handle.
		}
		if got != want {
			t.Errorf("%s: %d direct %s calls, want %d — register via s.handle so the route lands in Routes()",
				name, got, directRegistration, want)
		}
	}
}
