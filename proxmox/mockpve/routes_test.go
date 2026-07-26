package mockpve_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/mockpve"
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

// Every pattern Routes() reports is genuinely registered on the mux: dialed
// without credentials each one must reach a handler (401 from checkAuth, or a
// 2xx/4xx from the few public handlers) and never the mux's own 404. That makes
// Routes() a verified enumeration of the served surface rather than a list that
// could quietly drift from it — the property the coverage numerator rests on.
func TestRoutesAreAllServed(t *testing.T) {
	t.Parallel()
	srv := mockpve.New()
	routes := srv.Routes()
	if len(routes) < 200 {
		t.Errorf("Routes() = %d patterns, want the full built-in surface (~230) — did registerRoutes lose a service?",
			len(routes))
	}
	seen := make(map[string]bool, len(routes))
	for _, pattern := range routes {
		if seen[pattern] {
			t.Errorf("Routes() reports %q twice", pattern)
		}
		seen[pattern] = true

		method, path, ok := strings.Cut(pattern, " ")
		if !ok || !strings.HasPrefix(path, "/api2/json/") {
			t.Errorf("pattern %q is not the documented \"METHOD /api2/json/...\" form", pattern)
			continue
		}
		req := httptest.NewRequestWithContext(context.Background(), method, wildcardRe.ReplaceAllString(path, "x"), http.NoBody)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code == http.StatusNotFound {
			t.Errorf("pattern %q recorded by Routes() but the mux does not serve it (404)", pattern)
		}
	}
}

// wildcardRe matches a ServeMux path wildcard, e.g. the {node} in
// "/api2/json/nodes/{node}/qemu".
var wildcardRe = regexp.MustCompile(`\{[^}]*\}`)

// Routes() carries the real registered patterns — raw Go 1.22 ServeMux strings
// with the method, the /api2/json prefix, and the original wildcard names
// (mockpve.Server.Routes's documented contract, which the coverage tracker
// normalizes). Samples span the foundation plus several services.
func TestRoutesContainsKnownPatterns(t *testing.T) {
	t.Parallel()
	routes := mockpve.New().Routes()
	for _, want := range []string{
		"GET /api2/json/version",
		"POST /api2/json/access/ticket",
		"GET /api2/json/nodes/{node}/qemu",
		"POST /api2/json/nodes/{node}/lxc",
		"GET /api2/json/storage",
		"GET /api2/json/cluster/ha/status/current",
		"GET /api2/json/cluster/sdn/fabrics/fabric",
		"GET /api2/json/nodes/{node}/network",
		"GET /api2/json/cluster/resources",
		"GET /api2/json/access/users",
	} {
		if !slices.Contains(routes, want) {
			t.Errorf("Routes() missing %q", want)
		}
	}
}

// Two independently constructed servers enumerate the identical route list, in
// the same order — the tracker's numerator must not depend on map iteration or
// construction timing, or the generated report would churn between runs.
func TestRoutesStableAcrossInstances(t *testing.T) {
	t.Parallel()
	first, second := mockpve.New().Routes(), mockpve.New().Routes()
	if !slices.Equal(first, second) {
		t.Errorf("Routes() differs between instances:\nfirst:  %v\nsecond: %v", first, second)
	}

	// The returned slice is a copy: mutating it cannot corrupt the server's
	// list (the doc comment promises callers may retain and sort it).
	srv := mockpve.New()
	snapshot := srv.Routes()
	slices.Sort(snapshot)
	if !slices.Equal(srv.Routes(), first) {
		t.Error("sorting the Routes() result mutated the server's recorded list")
	}
}

// A RegisterHandler extension appears in Routes() too, so the enumeration
// reflects everything the server serves rather than only the built-ins.
func TestRoutesIncludesRegisteredHandler(t *testing.T) {
	t.Parallel()
	srv := mockpve.New()
	const pattern = "GET /api2/json/cluster/nextid"
	srv.RegisterHandler(pattern, http.NotFoundHandler())
	if !slices.Contains(srv.Routes(), pattern) {
		t.Errorf("Routes() missing the RegisterHandler pattern %q", pattern)
	}
}
