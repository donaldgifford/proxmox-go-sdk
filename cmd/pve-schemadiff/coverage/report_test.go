package coverage_test

import (
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/donaldgifford/proxmox-go-sdk/cmd/pve-schemadiff/coverage"
	"github.com/donaldgifford/proxmox-go-sdk/cmd/pve-schemadiff/schema"
)

// updateGolden rewrites the golden report instead of comparing against it. The
// golden file is reviewed by hand, so this is a convenience for the reviewer,
// never something CI passes.
var updateGolden = flag.Bool("update-golden", false, "rewrite testdata/report.golden")

// goldenPath holds the expected render of the fixture below. It deliberately
// does not end in .md: the repo's prettier and markdownlint globs cover every
// .md file, and a reformatted golden would no longer be what the renderer
// produces.
const goldenPath = "testdata/report.golden"

// fixtureBaseline is a small stand-in for the real 675-endpoint baseline,
// chosen to exercise every state and both section kinds: services with mixed
// coverage, an annotated stub, an out-of-scope family, and endpoints no service
// rule claims (which must land in "unassigned").
func fixtureBaseline() []schema.Endpoint {
	return []schema.Endpoint{
		{Method: "GET", Path: "/version"},
		{Method: "GET", Path: "/nodes/{node}/qemu"},
		{Method: "POST", Path: "/nodes/{node}/qemu"},
		{Method: "GET", Path: "/nodes/{node}/qemu/{vmid}/config"},
		{Method: "GET", Path: "/nodes/{node}/ceph/mirror"},
		{Method: "GET", Path: "/access/users"},
		{Method: "DELETE", Path: "/access/users/{userid}"},
		{Method: "GET", Path: "/cluster/notifications"},
		{Method: "POST", Path: "/cluster/notifications/targets/{name}/test"},
		{Method: "GET", Path: "/pools"},
	}
}

// fixtureRoutes stands in for mockpve.Routes(). Two entries are unmatched on
// purpose: one allowlisted, one not.
func fixtureRoutes() []string {
	return []string{
		"GET /api2/json/version",
		"GET /api2/json/nodes/{node}/qemu",
		"GET /api2/json/nodes/{node}/qemu/{id}/config", // {id} vs the baseline's {vmid}.
		"GET /api2/json/access/users",
		"GET /api2/json/cluster/sdn/fabrics", // Unmatched, allowlisted.
		"GET /api2/json/nodes/{node}/bogus",  // Unmatched, NOT allowlisted.
	}
}

const fixtureAnnotationsYAML = `baseline:
  pve_version: "9.9.9"
  source: "fixture apidoc.js"
  captured: "2026-01-01"
stubs:
  - path: "GET /nodes/{node}/ceph/mirror"
    reason: "no PVE REST endpoint; rbd CLI over ssh"
side_channel:
  - "snippet/backup upload -> /var/lib/vz"
out_of_scope:
  - prefix: "/cluster/notifications"
    reason: "owned by the consuming service"
    doc: "ADR-0001"
allow_unmatched_routes:
  - route: "GET /cluster/sdn/fabrics"
    reason: "fixture: baseline predates the fabric collection"
`

func fixtureAnnotations(t *testing.T) *coverage.Annotations {
	t.Helper()
	ann, err := coverage.ParseAnnotations([]byte(fixtureAnnotationsYAML))
	if err != nil {
		t.Fatalf("ParseAnnotations: %v", err)
	}
	return ann
}

func fixtureReport(t *testing.T) *coverage.Report {
	t.Helper()
	rep, err := coverage.Build(fixtureBaseline(), fixtureRoutes(), fixtureAnnotations(t))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return rep
}

// Every endpoint gets exactly one state, and the precedence is the documented
// one: a route that exists beats any annotation.
func TestBuildClassifies(t *testing.T) {
	t.Parallel()
	rep := fixtureReport(t)

	states := make(map[string]coverage.State)
	for _, s := range rep.Services {
		for _, row := range s.Rows {
			states[row.Key.String()] = row.State
		}
	}
	tests := map[string]coverage.State{
		"GET /version":                                coverage.StateCovered,
		"GET /nodes/{}/qemu":                          coverage.StateCovered,
		"POST /nodes/{}/qemu":                         coverage.StateGap,
		"GET /nodes/{}/qemu/{}/config":                coverage.StateCovered, // Placeholder names differ.
		"GET /nodes/{}/ceph/mirror":                   coverage.StateStub,
		"GET /access/users":                           coverage.StateCovered,
		"DELETE /access/users/{}":                     coverage.StateGap,
		"GET /cluster/notifications":                  coverage.StateOutOfScope,
		"POST /cluster/notifications/targets/{}/test": coverage.StateOutOfScope,
		"GET /pools":                                  coverage.StateGap,
	}
	for key, want := range tests {
		if got := states[key]; got != want {
			t.Errorf("state of %s = %q, want %q", key, got, want)
		}
	}
	if got, want := len(states), len(fixtureBaseline()); got != want {
		t.Errorf("classified %d endpoints, want %d — every baseline endpoint appears exactly once", got, want)
	}
	if got, want := rep.Totals.Total, len(fixtureBaseline()); got != want {
		t.Errorf("Totals.Total = %d, want %d", got, want)
	}
	if got, want := rep.Totals.Covered, 4; got != want {
		t.Errorf("Totals.Covered = %d, want %d", got, want)
	}
	if got, want := rep.Totals.Percent(), "40.0%"; got != want {
		t.Errorf("Totals.Percent() = %q, want %q", got, want)
	}
}

// A covered endpoint stays covered even when an annotation claims it is a stub
// or out of scope: the route is a fact, the annotation is stale.
func TestBuildCoveredBeatsAnnotations(t *testing.T) {
	t.Parallel()
	ann, err := coverage.ParseAnnotations([]byte(validBaseline + `
stubs:
  - path: "GET /version"
    reason: "stale claim"
out_of_scope:
  - prefix: "/version"
    reason: "stale claim"
    doc: "none"
`))
	if err != nil {
		t.Fatalf("ParseAnnotations: %v", err)
	}
	rep, err := coverage.Build(
		[]schema.Endpoint{{Method: "GET", Path: "/version"}},
		[]string{"GET /api2/json/version"}, ann)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got := rep.Services[0].Rows[0].State; got != coverage.StateCovered {
		t.Errorf("state = %q, want %q", got, coverage.StateCovered)
	}
}

// Services appear sorted with unassigned last, and a service with no endpoints
// in this baseline is omitted rather than rendered empty.
func TestBuildServiceOrder(t *testing.T) {
	t.Parallel()
	svcs := fixtureReport(t).Services
	names := make([]string, 0, len(svcs))
	for _, s := range svcs {
		names = append(names, s.Name)
	}
	want := []string{"access", "ceph", "qemu", "version", coverage.Unassigned}
	if !slices.Equal(names, want) {
		t.Errorf("service order = %v, want %v", names, want)
	}
}

// Build reports the fabrication guard's offenders and every stale annotation,
// but never fails on them — the caller decides what is fatal.
func TestBuildFindings(t *testing.T) {
	t.Parallel()
	f := fixtureReport(t).Findings

	if got, want := len(f.UnmatchedRoutes), 1; got != want {
		t.Fatalf("len(UnmatchedRoutes) = %d, want %d: %v", got, want, f.UnmatchedRoutes)
	}
	if got, want := f.UnmatchedRoutes[0].Key.String(), "GET /nodes/{}/bogus"; got != want {
		t.Errorf("UnmatchedRoutes[0] = %q, want %q", got, want)
	}
	if got := f.UnmatchedRoutes[0].RealMethods; len(got) != 0 {
		t.Errorf("RealMethods = %v, want empty — PVE serves nothing at that path", got)
	}
	if got, want := len(f.AllowedRoutes), 1; got != want {
		t.Fatalf("len(AllowedRoutes) = %d, want %d", got, want)
	}
	if got, want := f.AllowedRoutes[0].Route, "GET /cluster/sdn/fabrics"; got != want {
		t.Errorf("AllowedRoutes[0].Route = %q, want %q", got, want)
	}
	if f.Empty() {
		t.Error("Findings.Empty() = true, want false with an unmatched route")
	}
}

// Stale annotations are reported so the exceptions file cannot rot into the
// hand-maintained API knowledge this tracker replaces.
func TestBuildDetectsStaleAnnotations(t *testing.T) {
	t.Parallel()
	ann, err := coverage.ParseAnnotations([]byte(validBaseline + `
stubs:
  - path: "GET /gone/from/pve"
    reason: "endpoint was removed upstream"
out_of_scope:
  - prefix: "/also/gone"
    reason: "family was removed upstream"
    doc: "ADR-0001"
allow_unmatched_routes:
  - route: "GET /never/registered"
    reason: "the mock does not serve this"
`))
	if err != nil {
		t.Fatalf("ParseAnnotations: %v", err)
	}
	rep, err := coverage.Build(
		[]schema.Endpoint{{Method: "GET", Path: "/version"}},
		[]string{"GET /api2/json/version"}, ann)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	f := rep.Findings
	if got, want := f.StaleStubs, []string{"GET /gone/from/pve"}; !slices.Equal(got, want) {
		t.Errorf("StaleStubs = %v, want %v", got, want)
	}
	if got, want := f.StaleOutOfScope, []string{"/also/gone"}; !slices.Equal(got, want) {
		t.Errorf("StaleOutOfScope = %v, want %v", got, want)
	}
	if got, want := f.StaleAllowlist, []string{"GET /never/registered"}; !slices.Equal(got, want) {
		t.Errorf("StaleAllowlist = %v, want %v", got, want)
	}
}

// A baseline whose endpoints collide once normalized would make the arithmetic
// wrong, so Build refuses it instead of silently under-counting.
func TestBuildRejectsCollidingBaseline(t *testing.T) {
	t.Parallel()
	_, err := coverage.Build([]schema.Endpoint{
		{Method: "GET", Path: "/nodes/{node}/qemu/{vmid}"},
		{Method: "GET", Path: "/nodes/{node}/qemu/{id}"},
	}, nil, fixtureAnnotations(t))
	if err == nil {
		t.Fatal("Build = nil error, want a collision failure")
	}
	if !strings.Contains(err.Error(), "normalize to") {
		t.Errorf("error %q does not explain the collision", err)
	}
}

// A malformed route pattern is an error, not a silently dropped route: dropping
// it would understate coverage.
func TestBuildRejectsMalformedRoute(t *testing.T) {
	t.Parallel()
	if _, err := coverage.Build(fixtureBaseline(), []string{"bogus"}, fixtureAnnotations(t)); err == nil {
		t.Fatal("Build = nil error, want a malformed-pattern failure")
	}
}

// The render must be byte-identical across runs and independent of input order:
// the drift check diffs a regeneration against the committed file, so any map
// walk leaking into the output would fail CI at random.
func TestMarkdownIsByteStable(t *testing.T) {
	t.Parallel()
	first := fixtureReport(t).Markdown()
	for i := range 5 {
		if got := fixtureReport(t).Markdown(); got != first {
			t.Fatalf("render %d differs from the first render", i)
		}
	}

	baseline := fixtureBaseline()
	slices.Reverse(baseline)
	routes := fixtureRoutes()
	slices.Reverse(routes)
	shuffled, err := coverage.Build(baseline, routes, fixtureAnnotations(t))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got := shuffled.Markdown(); got != first {
		t.Error("render depends on input order, want identical output")
	}
}

// The golden file is the reviewed shape of the report: header, summary,
// side-channel and allowlist sections, and one table per service.
func TestMarkdownGolden(t *testing.T) {
	t.Parallel()
	got := fixtureReport(t).Markdown()
	if *updateGolden {
		if err := os.WriteFile(goldenPath, []byte(got), 0o600); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("wrote %s", goldenPath)
		return
	}
	want, err := os.ReadFile(filepath.Clean(goldenPath))
	if err != nil {
		t.Fatalf("read golden (regenerate with -update-golden): %v", err)
	}
	if got != string(want) {
		t.Errorf("render does not match %s:\n--- got ---\n%s", goldenPath, got)
	}
}

// A service with no endpoints reports n/a rather than dividing by zero.
func TestPercentWithoutEndpoints(t *testing.T) {
	t.Parallel()
	if got, want := (coverage.Counts{}).Percent(), "n/a"; got != want {
		t.Errorf("Counts{}.Percent() = %q, want %q", got, want)
	}
}
