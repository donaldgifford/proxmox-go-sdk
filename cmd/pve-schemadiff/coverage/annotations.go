package coverage

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"go.yaml.in/yaml/v4"
)

// Annotations is the tracker's only hand-curated input: the exceptions that
// cannot be derived from the baseline and the mock's route table. Everything
// else in the report is computed, which is the point — a hand-maintained map of
// the PVE API is exactly how the fabrics and DLB paths drifted (INV-0004).
//
// Load one with [LoadAnnotations] or [ParseAnnotations]; both decode strictly
// (an unknown key is an error, so a typoed section cannot silently annotate
// nothing) and validate every entry. Loading normalizes each path and prefix to
// the report's own form, so the lookup helpers compare exactly.
//
// The loader checks syntax and required fields only. Whether an annotation still
// describes a real endpoint is a cross-check against the baseline, which the
// report step owns — a stub for a path PVE no longer serves is stale, not
// malformed.
type Annotations struct {
	// Baseline is the provenance of the endpoint baseline, for the report
	// header.
	Baseline BaselineInfo `yaml:"baseline"`

	// Stubs are real endpoints the SDK deliberately ships as documented
	// ErrUnsupported. They are counted apart from both covered and gap, with the
	// reason shown, so a deliberate refusal never reads as an oversight.
	Stubs []Stub `yaml:"stubs"`

	// SideChannel records capabilities the SDK covers over proxmox/ssh rather
	// than REST. These are free text, not endpoints: PVE serves no REST endpoint
	// for them, so they appear in no table and would otherwise be invisible.
	SideChannel []string `yaml:"side_channel"`

	// OutOfScope marks path families the SDK has decided not to implement. Each
	// needs a deciding document — "not yet triaged" is a gap, not a decision
	// (IMPL-0006 OQ-4a), and keeping those in the gap count is what applies the
	// pressure the report exists to apply.
	OutOfScope []OutOfScope `yaml:"out_of_scope"`

	// AllowUnmatchedRoutes is the fabrication guard's escape hatch: mock routes
	// permitted to reference an endpoint the baseline does not hold. Empty is the
	// goal — every entry is a claim that the baseline is wrong, so each carries a
	// reason and should be read as debt.
	AllowUnmatchedRoutes []AllowedRoute `yaml:"allow_unmatched_routes"`
}

// BaselineInfo describes where the endpoint baseline came from. The report
// header states it because "30% of the API" is meaningless without saying which
// PVE version's API was measured, and baseline.json itself carries no metadata
// (it is a bare endpoint array) — hence recording the provenance here.
type BaselineInfo struct {
	PVEVersion string `yaml:"pve_version"` // e.g. "9.2.4".
	Source     string `yaml:"source"`      // Where the apidoc.js came from.
	Captured   string `yaml:"captured"`    // ISO date of the dump.
}

// Stub is one deliberately-unsupported endpoint.
type Stub struct {
	Path   string `yaml:"path"`   // "METHOD /path", normalized on load.
	Reason string `yaml:"reason"` // Why, ideally citing the finding.
}

// OutOfScope is one path family the SDK will not implement.
type OutOfScope struct {
	Prefix string `yaml:"prefix"` // Normalized path prefix, e.g. "/cluster/notifications".
	Reason string `yaml:"reason"`
	Doc    string `yaml:"doc"` // The deciding document, e.g. "ADR-0001".
}

// AllowedRoute is one mock route exempted from the fabrication guard.
type AllowedRoute struct {
	Route  string `yaml:"route"`  // "METHOD /path", normalized on load.
	Reason string `yaml:"reason"` // Why the baseline is believed wrong.
}

// LoadAnnotations reads, strictly decodes, and validates the annotations file.
func LoadAnnotations(path string) (*Annotations, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- path is the operator's own -annotations flag.
	if err != nil {
		return nil, fmt.Errorf("coverage: read annotations: %w", err)
	}
	ann, err := ParseAnnotations(raw)
	if err != nil {
		return nil, fmt.Errorf("coverage: %s: %w", path, err)
	}
	return ann, nil
}

// ErrEmptyAnnotations reports an annotations file with no content. It is an
// error rather than an empty exception set: a truncated file must not quietly
// produce a report with no provenance and every stub demoted to a gap.
var ErrEmptyAnnotations = errors.New("coverage: annotations file is empty")

// ParseAnnotations decodes and validates annotations from YAML.
func ParseAnnotations(raw []byte) (*Annotations, error) {
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	var ann Annotations
	if err := dec.Decode(&ann); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, ErrEmptyAnnotations
		}
		return nil, fmt.Errorf("parse annotations: %w", err)
	}
	ann.normalize()
	if err := ann.Validate(); err != nil {
		return nil, err
	}
	return &ann, nil
}

// normalize rewrites every path and prefix to the report's own form, so an entry
// written with the /api2/json prefix or PVE's placeholder names still matches.
func (a *Annotations) normalize() {
	for i, s := range a.Stubs {
		if k, err := ParsePattern(s.Path); err == nil {
			a.Stubs[i].Path = k.String()
		}
	}
	for i, r := range a.AllowUnmatchedRoutes {
		if k, err := ParsePattern(r.Route); err == nil {
			a.AllowUnmatchedRoutes[i].Route = k.String()
		}
	}
	for i, o := range a.OutOfScope {
		a.OutOfScope[i].Prefix = NormalizePath(o.Prefix)
	}
}

// Validate reports every problem in the file at once, so a hand-edited
// annotations file is fixed in one pass rather than one error per run.
func (a *Annotations) Validate() error {
	return errors.Join(
		a.validateBaseline(),
		errors.Join(a.validateStubs()...),
		errors.Join(a.validateSideChannel()...),
		errors.Join(a.validateOutOfScope()...),
		errors.Join(a.validateAllowlist()...),
	)
}

// validateBaseline requires full provenance: the report header cannot honestly
// say what was measured without it.
func (a *Annotations) validateBaseline() error {
	var errs []error
	for _, f := range []struct{ val, field string }{
		{a.Baseline.PVEVersion, "pve_version"},
		{a.Baseline.Source, "source"},
		{a.Baseline.Captured, "captured"},
	} {
		if strings.TrimSpace(f.val) == "" {
			errs = append(errs, fmt.Errorf("baseline.%s is required", f.field))
		}
	}
	return errors.Join(errs...)
}

func (a *Annotations) validateStubs() []error {
	var errs []error
	seen := make(map[string]int, len(a.Stubs))
	for i, s := range a.Stubs {
		where := fmt.Sprintf("stubs[%d]", i)
		if _, err := ParsePattern(s.Path); err != nil {
			errs = append(errs, fmt.Errorf("%s: path %q: %w", where, s.Path, err))
		}
		if strings.TrimSpace(s.Reason) == "" {
			errs = append(errs, fmt.Errorf("%s (%s): reason is required", where, s.Path))
		}
		if prev, dup := seen[s.Path]; dup {
			errs = append(errs, fmt.Errorf("%s: %s already annotated by stubs[%d]", where, s.Path, prev))
		}
		seen[s.Path] = i
	}
	return errs
}

func (a *Annotations) validateSideChannel() []error {
	var errs []error
	for i, s := range a.SideChannel {
		if strings.TrimSpace(s) == "" {
			errs = append(errs, fmt.Errorf("side_channel[%d] is empty", i))
		}
	}
	return errs
}

func (a *Annotations) validateOutOfScope() []error {
	var errs []error
	seen := make(map[string]int, len(a.OutOfScope))
	for i, o := range a.OutOfScope {
		where := fmt.Sprintf("out_of_scope[%d]", i)
		if !strings.HasPrefix(o.Prefix, "/") {
			errs = append(errs, fmt.Errorf("%s: prefix %q must be a rooted path", where, o.Prefix))
		}
		if strings.TrimSpace(o.Reason) == "" {
			errs = append(errs, fmt.Errorf("%s (%s): reason is required", where, o.Prefix))
		}
		if strings.TrimSpace(o.Doc) == "" {
			errs = append(errs, fmt.Errorf("%s (%s): doc is required — an untriaged family is a gap, "+
				"not a decided non-goal", where, o.Prefix))
		}
		if prev, dup := seen[o.Prefix]; dup {
			errs = append(errs, fmt.Errorf("%s: %s already annotated by out_of_scope[%d]", where, o.Prefix, prev))
		}
		seen[o.Prefix] = i
	}
	return errs
}

func (a *Annotations) validateAllowlist() []error {
	var errs []error
	seen := make(map[string]int, len(a.AllowUnmatchedRoutes))
	for i, r := range a.AllowUnmatchedRoutes {
		where := fmt.Sprintf("allow_unmatched_routes[%d]", i)
		if _, err := ParsePattern(r.Route); err != nil {
			errs = append(errs, fmt.Errorf("%s: route %q: %w", where, r.Route, err))
		}
		if strings.TrimSpace(r.Reason) == "" {
			errs = append(errs, fmt.Errorf("%s (%s): reason is required — an allowlisted route claims "+
				"the baseline is wrong", where, r.Route))
		}
		if prev, dup := seen[r.Route]; dup {
			errs = append(errs, fmt.Errorf("%s: %s already allowed by allow_unmatched_routes[%d]",
				where, r.Route, prev))
		}
		seen[r.Route] = i
	}
	return errs
}

// StubReason returns the reason k is a deliberate stub, if it is one.
func (a *Annotations) StubReason(k Key) (string, bool) {
	want := k.String()
	for _, s := range a.Stubs {
		if s.Path == want {
			return s.Reason, true
		}
	}
	return "", false
}

// AllowsRoute reports whether the fabrication guard should permit an unmatched
// mock route, and why.
func (a *Annotations) AllowsRoute(k Key) (string, bool) {
	want := k.String()
	for _, r := range a.AllowUnmatchedRoutes {
		if r.Route == want {
			return r.Reason, true
		}
	}
	return "", false
}

// OutOfScopeFor returns the out-of-scope decision covering path, if any.
// Matching is segment-aware, like the service rules: /cluster/notifications
// never claims a /cluster/notificationsX.
func (a *Annotations) OutOfScopeFor(path string) (OutOfScope, bool) {
	for _, o := range a.OutOfScope {
		if (serviceRule{prefix: o.Prefix}).matches(path) {
			return o, true
		}
	}
	return OutOfScope{}, false
}
