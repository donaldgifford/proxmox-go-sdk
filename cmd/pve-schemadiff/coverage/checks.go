package coverage

import (
	"errors"
	"fmt"
	"strings"
)

// FabricationError reports mockpve routes referencing endpoints real PVE does
// not serve. This is the check that gives the report teeth: the mock is the
// SDK's stand-in for PVE, so a route with no real endpoint behind it means the
// SDK is tested — and passing — against an API that does not exist. That is
// exactly how the SDN-fabrics paths and the HA Dynamic Load Balancer shipped
// wrong and stayed wrong until a live run (INV-0004).
type FabricationError struct {
	Routes []Unmatched
}

// Error names every offender, and separates the two failure modes, because they
// have different fixes: a path PVE does not serve at all is a fabricated path,
// while a path it serves under other verbs is a wrong-verb registration.
func (e *FabricationError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "coverage: %d mockpve route(s) reference endpoints the baseline does not hold",
		len(e.Routes))
	for _, u := range e.Routes {
		if len(u.RealMethods) == 0 {
			fmt.Fprintf(&b, "\n  %s — no such path on PVE (fabricated path)", u.Key)
			continue
		}
		fmt.Fprintf(&b, "\n  %s — PVE serves only %s at this path (wrong verb)",
			u.Key, strings.Join(u.RealMethods, ", "))
	}
	b.WriteString("\nfix the mock to mirror real PVE, or allowlist the route with a reason " +
		"in the annotations file")
	return b.String()
}

// StaleAnnotationsError reports exceptions that no longer describe anything: a
// stub for an endpoint the baseline does not hold, an allowlist entry for a route
// the mock does not serve unmatched, or an out-of-scope prefix matching no
// endpoint. Left alone these rot, and an exceptions file nobody trusts is the
// hand-maintained API knowledge this tracker exists to replace.
type StaleAnnotationsError struct {
	Stubs      []string
	Allowlist  []string
	OutOfScope []string
}

// Error lists each stale entry under the section that holds it.
func (e *StaleAnnotationsError) Error() string {
	var b strings.Builder
	b.WriteString("coverage: stale annotations (they match nothing; delete them)")
	for _, s := range e.Stubs {
		fmt.Fprintf(&b, "\n  stubs: %s is not in the baseline", s)
	}
	for _, s := range e.Allowlist {
		fmt.Fprintf(&b, "\n  allow_unmatched_routes: %s is not an unmatched mockpve route", s)
	}
	for _, s := range e.OutOfScope {
		fmt.Fprintf(&b, "\n  out_of_scope: %s matches no baseline endpoint", s)
	}
	return b.String()
}

// Check reports every fatal finding in the report.
//
// It runs in both tool modes — writing the report and verifying it — so a
// fabricated route can never be committed, whether or not anyone regenerated
// the doc. A stale annotation is fatal for the same reason: the exceptions file
// is small and hand-written, so keeping it exact is cheap and letting it rot is
// how the report stops being believable.
func (r *Report) Check() error {
	var errs []error
	if len(r.Findings.UnmatchedRoutes) > 0 {
		errs = append(errs, &FabricationError{Routes: r.Findings.UnmatchedRoutes})
	}
	if len(r.Findings.StaleStubs) > 0 || len(r.Findings.StaleAllowlist) > 0 ||
		len(r.Findings.StaleOutOfScope) > 0 {
		errs = append(errs, &StaleAnnotationsError{
			Stubs:      r.Findings.StaleStubs,
			Allowlist:  r.Findings.StaleAllowlist,
			OutOfScope: r.Findings.StaleOutOfScope,
		})
	}
	return errors.Join(errs...)
}

// DriftError reports a committed report that no longer matches a regeneration.
// The fix is mechanical, so the message says so rather than dumping a diff: the
// PR's own diff of the regenerated file is the readable version.
type DriftError struct {
	Path string
	Line int    // 1-indexed first differing line; 0 when only the length differs.
	Want string // The regenerated line (what the file should say).
	Got  string // The committed line.
}

// Error states where the file diverges and how to fix it.
func (e *DriftError) Error() string {
	if e.Line == 0 {
		return fmt.Sprintf("coverage: %s is stale (length differs); regenerate with `just coverage`", e.Path)
	}
	return fmt.Sprintf("coverage: %s is stale at line %d; regenerate with `just coverage`\n  committed: %s\n  expected:  %s",
		e.Path, e.Line, e.Got, e.Want)
}

// CheckDrift compares a committed report against a fresh render, returning a
// *[DriftError] when they differ. Comparison is byte-exact by design: the report
// is machine-owned, so "close enough" would let a hand edit survive.
func CheckDrift(path, committed, regenerated string) error {
	if committed == regenerated {
		return nil
	}
	got, want := strings.Split(committed, "\n"), strings.Split(regenerated, "\n")
	for i := range min(len(got), len(want)) {
		if got[i] != want[i] {
			return &DriftError{Path: path, Line: i + 1, Got: got[i], Want: want[i]}
		}
	}
	return &DriftError{Path: path}
}
