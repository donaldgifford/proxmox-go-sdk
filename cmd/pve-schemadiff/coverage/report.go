package coverage

import (
	"fmt"
	"slices"
	"strings"

	"github.com/donaldgifford/proxmox-go-sdk/cmd/pve-schemadiff/schema"
)

// State is how one real PVE endpoint stands relative to the SDK.
type State string

// The four states an endpoint can be in. Every baseline endpoint gets exactly
// one; the three non-gap states are the only ways an endpoint is not debt.
const (
	// StateCovered means mockpve serves a route for it, which by the
	// every-op-is-tested-against-mockpve discipline means the SDK implements it.
	StateCovered State = "covered"
	// StateStub means the SDK deliberately returns ErrUnsupported, with a reason.
	StateStub State = "stub"
	// StateOutOfScope means the SDK has decided not to implement it, per a doc.
	StateOutOfScope State = "out of scope"
	// StateGap means it is simply not implemented — including families nobody has
	// triaged yet, which is the pressure the report exists to keep visible.
	StateGap State = "gap"
)

// Row is one endpoint in the report.
type Row struct {
	Key   Key
	State State
	Note  string // Reason for a stub or out-of-scope row; empty otherwise.
}

// Counts totals one service, or the whole report.
type Counts struct {
	Covered    int
	Stub       int
	OutOfScope int
	Gap        int
	Total      int
}

// Percent renders the covered share as a fixed-width fraction, or "n/a" when
// there is nothing to divide by (a service with no endpoints in this baseline).
func (c Counts) Percent() string {
	if c.Total == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%.1f%%", 100*float64(c.Covered)/float64(c.Total))
}

// ServiceReport is one section of the report.
type ServiceReport struct {
	Name string
	Rows []Row // Sorted by path, then method.
	Counts
}

// Findings are the integrity problems Build detects. They are not report
// content: the checks consume them and fail the run, which is why a report with
// unmatched routes never reaches disk.
type Findings struct {
	// UnmatchedRoutes are mock routes with no baseline endpoint and no allowlist
	// entry — the fabrication guard's offenders. A route here means the SDK is
	// tested against an API that does not exist, which is how the SDN-fabrics and
	// HA DLB paths shipped wrong (INV-0004).
	UnmatchedRoutes []Unmatched

	// AllowedRoutes are unmatched routes an annotation permits, with reasons.
	// They are rendered, so the exemptions stay visible as debt.
	AllowedRoutes []AllowedRoute

	// The stale annotations: entries that no longer describe anything. An
	// exceptions file that silently accumulates dead entries is the hand-
	// maintained API knowledge this tracker exists to replace.
	StaleStubs      []string // Stub paths absent from the baseline.
	StaleAllowlist  []string // Allowlisted routes the mock no longer registers unmatched.
	StaleOutOfScope []string // out_of_scope prefixes matching no baseline endpoint.
}

// Unmatched is one mock route the baseline does not hold, with the verbs PVE
// does serve at that path. The distinction is what makes the guard's message
// actionable: an empty RealMethods means the path itself does not exist
// (fabricated), while a populated one means the path is real and only the verb
// is wrong — two different fixes.
type Unmatched struct {
	Key         Key
	RealMethods []string // Sorted; empty when PVE serves nothing at this path.
}

// Empty reports whether there is nothing to complain about.
func (f *Findings) Empty() bool {
	return len(f.UnmatchedRoutes) == 0 && len(f.StaleStubs) == 0 &&
		len(f.StaleAllowlist) == 0 && len(f.StaleOutOfScope) == 0
}

// Report is the whole measurement: every baseline endpoint classified, grouped
// by service, plus the totals and the findings.
type Report struct {
	Baseline    BaselineInfo
	Services    []ServiceReport // Sorted, unassigned last; empty services omitted.
	Totals      Counts
	SideChannel []string
	RouteCount  int // Distinct normalized mock routes, matched or not.
	Findings    Findings
}

// Build classifies every baseline endpoint against the mock's route table.
//
// It errors only on inputs that make the arithmetic meaningless: a malformed
// route pattern, or a baseline whose endpoints collide once normalized (which
// would mean erasing placeholder names loses information the comparison needs).
// Everything else — fabricated routes, stale annotations — is reported through
// [Findings] so the caller decides what is fatal.
func Build(baseline []schema.Endpoint, routes []string, ann *Annotations) (*Report, error) {
	routeKeys, err := ParsePatterns(routes)
	if err != nil {
		return nil, err
	}
	covered := make(map[Key]bool, len(routeKeys))
	for _, k := range routeKeys {
		covered[k] = true
	}
	known, err := indexBaseline(baseline)
	if err != nil {
		return nil, err
	}
	byPath := indexByPath(known)

	rep := &Report{
		Baseline:    ann.Baseline,
		SideChannel: slices.Clone(ann.SideChannel),
		RouteCount:  len(covered),
	}
	rep.Services = buildServices(known, covered, ann, &rep.Totals)
	rep.Findings = findings(known, byPath, covered, ann)
	return rep, nil
}

// indexBaseline keys the baseline by normalized endpoint, rejecting collisions.
func indexBaseline(baseline []schema.Endpoint) (map[Key]bool, error) {
	known := make(map[Key]bool, len(baseline))
	seen := make(map[Key]string, len(baseline))
	for _, ep := range baseline {
		k := EndpointKey(ep)
		if prev, dup := seen[k]; dup {
			return nil, fmt.Errorf(
				"coverage: baseline endpoints %q and %q both normalize to %v — "+
					"placeholder erasure is lossy for this baseline", prev, ep.Method+" "+ep.Path, k)
		}
		seen[k] = ep.Method + " " + ep.Path
		known[k] = true
	}
	return known, nil
}

// indexByPath groups the baseline's verbs by path, so the fabrication guard can
// tell a wrong verb from a wrong path.
func indexByPath(known map[Key]bool) map[string][]string {
	byPath := make(map[string][]string, len(known))
	for k := range known {
		byPath[k.Path] = append(byPath[k.Path], k.Method)
	}
	for _, methods := range byPath {
		slices.Sort(methods)
	}
	return byPath
}

// buildServices groups the classified endpoints into report sections and
// accumulates the totals.
func buildServices(known, covered map[Key]bool, ann *Annotations, totals *Counts) []ServiceReport {
	rows := make(map[string][]Row, len(known))
	for k := range known {
		svc := ServiceFor(k.Path)
		rows[svc] = append(rows[svc], classify(k, covered[k], ann))
	}
	out := make([]ServiceReport, 0, len(rows))
	for _, svc := range Services() {
		svcRows, ok := rows[svc]
		if !ok {
			continue
		}
		slices.SortFunc(svcRows, byPathThenMethod)
		sr := ServiceReport{Name: svc, Rows: svcRows}
		for _, r := range svcRows {
			sr.add(r.State)
			totals.add(r.State)
		}
		out = append(out, sr)
	}
	return out
}

// classify decides one endpoint's state. Covered wins over every annotation: a
// route that exists is a fact, and an annotation claiming otherwise is stale.
// Out-of-scope then loses to a stub, since a stub is a narrower, per-endpoint
// statement than a family-wide decision.
func classify(k Key, isCovered bool, ann *Annotations) Row {
	if isCovered {
		return Row{Key: k, State: StateCovered}
	}
	if reason, ok := ann.StubReason(k); ok {
		return Row{Key: k, State: StateStub, Note: reason}
	}
	if oos, ok := ann.OutOfScopeFor(k.Path); ok {
		return Row{Key: k, State: StateOutOfScope, Note: oos.Reason + " (" + oos.Doc + ")"}
	}
	return Row{Key: k, State: StateGap}
}

// byPathThenMethod orders rows the way the baseline is ordered: by path, so an
// endpoint's verbs sit together.
func byPathThenMethod(a, b Row) int {
	if c := strings.Compare(a.Key.Path, b.Key.Path); c != 0 {
		return c
	}
	return strings.Compare(a.Key.Method, b.Key.Method)
}

// add counts one row.
func (c *Counts) add(st State) {
	c.Total++
	switch st {
	case StateCovered:
		c.Covered++
	case StateStub:
		c.Stub++
	case StateOutOfScope:
		c.OutOfScope++
	case StateGap:
		c.Gap++
	}
}

// findings collects the fabrication-guard offenders and the stale annotations.
func findings(known map[Key]bool, byPath map[string][]string, covered map[Key]bool, ann *Annotations) Findings {
	var f Findings
	allowedHit := make(map[string]bool, len(ann.AllowUnmatchedRoutes))
	for k := range covered {
		if known[k] {
			continue
		}
		if reason, ok := ann.AllowsRoute(k); ok {
			allowedHit[k.String()] = true
			f.AllowedRoutes = append(f.AllowedRoutes, AllowedRoute{Route: k.String(), Reason: reason})
			continue
		}
		f.UnmatchedRoutes = append(f.UnmatchedRoutes, Unmatched{Key: k, RealMethods: byPath[k.Path]})
	}
	slices.SortFunc(f.UnmatchedRoutes, func(a, b Unmatched) int {
		return strings.Compare(a.Key.String(), b.Key.String())
	})
	slices.SortFunc(f.AllowedRoutes, func(a, b AllowedRoute) int { return strings.Compare(a.Route, b.Route) })

	for _, s := range ann.Stubs {
		if k, err := ParsePattern(s.Path); err == nil && !known[k] {
			f.StaleStubs = append(f.StaleStubs, s.Path)
		}
	}
	for _, r := range ann.AllowUnmatchedRoutes {
		if !allowedHit[r.Route] {
			f.StaleAllowlist = append(f.StaleAllowlist, r.Route)
		}
	}
	for _, o := range ann.OutOfScope {
		if !matchesAny(known, o.Prefix) {
			f.StaleOutOfScope = append(f.StaleOutOfScope, o.Prefix)
		}
	}
	slices.Sort(f.StaleStubs)
	slices.Sort(f.StaleAllowlist)
	slices.Sort(f.StaleOutOfScope)
	return f
}

// matchesAny reports whether any endpoint in the set falls under the prefix.
func matchesAny(set map[Key]bool, prefix string) bool {
	rule := serviceRule{prefix: prefix}
	for k := range set {
		if rule.matches(k.Path) {
			return true
		}
	}
	return false
}
