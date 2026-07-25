package coverage_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/donaldgifford/proxmox-go-sdk/cmd/pve-schemadiff/coverage"
	"github.com/donaldgifford/proxmox-go-sdk/cmd/pve-schemadiff/schema"
)

// The guard validated against the exact drift it exists to prevent: the flat
// /cluster/sdn/fabrics shape the SDK actually shipped (DESIGN-0003, INV-0004 F4)
// must be named in the error.
func TestCheckNamesFabricatedRoute(t *testing.T) {
	t.Parallel()
	ann := fixtureAnnotations(t)
	ann.AllowUnmatchedRoutes = nil // Drop the fixture's exemption for this route.
	rep, err := coverage.Build(fixtureBaseline(), []string{
		"GET /api2/json/version",
		"GET /api2/json/cluster/sdn/fabrics",
		"GET /api2/json/cluster/ha/lbalancer",
	}, ann)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	checkErr := rep.Check()
	if checkErr == nil {
		t.Fatal("Check() = nil, want a fabrication failure")
	}
	var fab *coverage.FabricationError
	if !errors.As(checkErr, &fab) {
		t.Fatalf("Check() = %v, want a *FabricationError", checkErr)
	}
	if got, want := len(fab.Routes), 2; got != want {
		t.Errorf("len(Routes) = %d, want %d", got, want)
	}
	for _, want := range []string{"/cluster/sdn/fabrics", "/cluster/ha/lbalancer", "fabricated path"} {
		if !strings.Contains(checkErr.Error(), want) {
			t.Errorf("error does not mention %q:\n%v", want, checkErr)
		}
	}
}

// A route whose path is real but whose verb is not gets a different message,
// because it is a different bug: the mock registered the wrong method rather
// than inventing a path. Both of the real 26 findings' shapes are covered here —
// the qemu status/{action} wildcard is a wrong-verb case in disguise.
func TestCheckDistinguishesWrongVerb(t *testing.T) {
	t.Parallel()
	ann := fixtureAnnotations(t)
	ann.AllowUnmatchedRoutes = nil
	rep, err := coverage.Build(
		[]schema.Endpoint{
			{Method: "GET", Path: "/nodes/{node}/qemu"},
			{Method: "POST", Path: "/nodes/{node}/qemu"},
		},
		[]string{"DELETE /api2/json/nodes/{node}/qemu"}, ann)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	var fab *coverage.FabricationError
	if !errors.As(rep.Check(), &fab) {
		t.Fatalf("Check() = %v, want a *FabricationError", rep.Check())
	}
	if got, want := len(fab.Routes[0].RealMethods), 2; got != want {
		t.Fatalf("RealMethods = %v, want %d entries", fab.Routes[0].RealMethods, want)
	}
	msg := fab.Error()
	if !strings.Contains(msg, "PVE serves only GET, POST at this path") {
		t.Errorf("error does not name the real verbs:\n%s", msg)
	}
	if strings.Contains(msg, "fabricated path") {
		t.Errorf("wrong-verb route reported as a fabricated path:\n%s", msg)
	}
}

// An allowlisted route does not fail the guard — that is what the escape hatch
// is for — and the fixture as committed is otherwise clean.
func TestCheckHonorsAllowlist(t *testing.T) {
	t.Parallel()
	ann := fixtureAnnotations(t)
	rep, err := coverage.Build(fixtureBaseline(), []string{
		"GET /api2/json/version",
		"GET /api2/json/cluster/sdn/fabrics", // Allowlisted by the fixture.
	}, ann)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := rep.Check(); err != nil {
		t.Errorf("Check() = %v, want nil for an allowlisted route", err)
	}
}

// Stale annotations fail the run too: the exceptions file is small and
// hand-written, so keeping it exact is cheap and letting it rot is how the
// report stops being believable.
func TestCheckRejectsStaleAnnotations(t *testing.T) {
	t.Parallel()
	ann, err := coverage.ParseAnnotations([]byte(validBaseline + `
stubs:
  - path: "GET /gone/from/pve"
    reason: "removed upstream"
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
	var stale *coverage.StaleAnnotationsError
	if !errors.As(rep.Check(), &stale) {
		t.Fatalf("Check() = %v, want a *StaleAnnotationsError", rep.Check())
	}
	if !strings.Contains(stale.Error(), "GET /gone/from/pve is not in the baseline") {
		t.Errorf("error does not name the stale stub:\n%v", stale)
	}
}

// A clean report passes both checks.
func TestCheckCleanReport(t *testing.T) {
	t.Parallel()
	ann := fixtureAnnotations(t)
	ann.AllowUnmatchedRoutes = nil
	rep, err := coverage.Build(fixtureBaseline(), []string{"GET /api2/json/version"}, ann)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := rep.Check(); err != nil {
		t.Errorf("Check() = %v, want nil", err)
	}
	if !rep.Findings.Empty() {
		t.Error("Findings.Empty() = false, want true")
	}
}

// The drift check is byte-exact: the report is machine-owned, so a hand edit
// must fail rather than pass as close enough.
func TestCheckDrift(t *testing.T) {
	t.Parallel()
	const rendered = "line one\nline two\nline three\n"

	if err := coverage.CheckDrift("docs/COVERAGE.md", rendered, rendered); err != nil {
		t.Errorf("CheckDrift on identical input = %v, want nil", err)
	}

	edited := strings.Replace(rendered, "line two", "line 2 (hand-edited)", 1)
	err := coverage.CheckDrift("docs/COVERAGE.md", edited, rendered)
	var drift *coverage.DriftError
	if !errors.As(err, &drift) {
		t.Fatalf("CheckDrift on an edited file = %v, want a *DriftError", err)
	}
	if drift.Line != 2 {
		t.Errorf("DriftError.Line = %d, want 2", drift.Line)
	}
	for _, want := range []string{"docs/COVERAGE.md", "line 2 (hand-edited)", "just coverage"} {
		if !strings.Contains(drift.Error(), want) {
			t.Errorf("error does not mention %q:\n%v", want, drift)
		}
	}

	// A committed file that is a strict prefix of the render has no differing
	// line to point at, so the message falls back to the length difference.
	err = coverage.CheckDrift("docs/COVERAGE.md", "line one\nline two", rendered)
	if !errors.As(err, &drift) {
		t.Fatalf("CheckDrift on a truncated file = %v, want a *DriftError", err)
	}
	if drift.Line != 0 {
		t.Errorf("DriftError.Line = %d, want 0 for a length-only difference", drift.Line)
	}
	if !strings.Contains(drift.Error(), "length differs") {
		t.Errorf("length-only error is not explained:\n%v", drift)
	}
}
