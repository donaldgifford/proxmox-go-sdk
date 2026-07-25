package coverage_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/donaldgifford/proxmox-go-sdk/cmd/pve-schemadiff/coverage"
)

// validBaseline is the provenance block every fixture needs, since the loader
// requires it.
const validBaseline = `baseline:
  pve_version: "9.2.4"
  source: "r740a apidoc.js"
  captured: "2026-07-22"
`

// A complete file round-trips, and every path is normalized on the way in: an
// entry written in PVE's own spelling (/api2/json prefix, {node} placeholders)
// must match the report's normalized keys, or the annotation would silently
// apply to nothing.
func TestParseAnnotationsNormalizes(t *testing.T) {
	t.Parallel()
	ann, err := coverage.ParseAnnotations([]byte(validBaseline + `
stubs:
  - path: "get /api2/json/nodes/{node}/ceph/mirror"
    reason: "no PVE REST endpoint; use the rbd CLI over ssh"
side_channel:
  - "snippet/backup upload -> /var/lib/vz (no REST upload)"
out_of_scope:
  - prefix: "/nodes/{node}/subscription"
    reason: "licensing, not an SDK concern"
    doc: "ADR-0002"
allow_unmatched_routes:
  - route: "GET /api2/json/cluster/sdn/fabrics"
    reason: "baseline predates the fabric collection"
`))
	if err != nil {
		t.Fatalf("ParseAnnotations: %v", err)
	}
	if got, want := ann.Baseline.PVEVersion, "9.2.4"; got != want {
		t.Errorf("Baseline.PVEVersion = %q, want %q", got, want)
	}
	if got, want := ann.Stubs[0].Path, "GET /nodes/{}/ceph/mirror"; got != want {
		t.Errorf("Stubs[0].Path = %q, want %q (normalized)", got, want)
	}
	if got, want := ann.OutOfScope[0].Prefix, "/nodes/{}/subscription"; got != want {
		t.Errorf("OutOfScope[0].Prefix = %q, want %q (normalized)", got, want)
	}
	if got, want := ann.AllowUnmatchedRoutes[0].Route, "GET /cluster/sdn/fabrics"; got != want {
		t.Errorf("AllowUnmatchedRoutes[0].Route = %q, want %q (normalized)", got, want)
	}
	if got, want := len(ann.SideChannel), 1; got != want {
		t.Errorf("len(SideChannel) = %d, want %d", got, want)
	}
}

// A typoed section name must be an error. Silently accepting it would annotate
// nothing while looking annotated — the failure mode that makes an exceptions
// file untrustworthy.
func TestParseAnnotationsRejectsUnknownKeys(t *testing.T) {
	t.Parallel()
	_, err := coverage.ParseAnnotations([]byte(validBaseline + "side_channels: []\n"))
	if err == nil {
		t.Fatal("ParseAnnotations = nil error, want a strict-decode failure")
	}
	if !strings.Contains(err.Error(), "side_channels") {
		t.Errorf("error %q does not name the unknown key", err)
	}
}

// An empty file is an error, not an empty exception set: it would strip the
// report's provenance and demote every stub to a gap.
func TestParseAnnotationsRejectsEmpty(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"", "\n", "# only a comment\n"} {
		_, err := coverage.ParseAnnotations([]byte(in))
		if !errors.Is(err, coverage.ErrEmptyAnnotations) {
			t.Errorf("ParseAnnotations(%q) error = %v, want ErrEmptyAnnotations", in, err)
		}
	}
}

// Every field the report depends on is required, and each failure names the
// offending entry.
func TestParseAnnotationsValidates(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		yaml string
		want string
	}{
		{
			"missing provenance",
			"stubs: []\n",
			"baseline.pve_version is required",
		},
		{
			"stub without a reason",
			validBaseline + "stubs:\n  - path: \"GET /version\"\n",
			"stubs[0] (GET /version): reason is required",
		},
		{
			"stub path is not METHOD /path",
			validBaseline + "stubs:\n  - path: \"/version\"\n    reason: \"x\"\n",
			"stubs[0]: path",
		},
		{
			"side_channel entry is blank",
			validBaseline + "side_channel:\n  - \"  \"\n",
			"side_channel[0] is empty",
		},
		{
			"out_of_scope prefix is not rooted",
			validBaseline + "out_of_scope:\n  - prefix: \"cluster/x\"\n    reason: \"r\"\n    doc: \"d\"\n",
			"must be a rooted path",
		},
		{
			"out_of_scope without a deciding doc",
			validBaseline + "out_of_scope:\n  - prefix: \"/pools\"\n    reason: \"not yet triaged\"\n",
			"doc is required",
		},
		{
			"allowlisted route without a reason",
			validBaseline + "allow_unmatched_routes:\n  - route: \"GET /nope\"\n",
			"reason is required",
		},
		{
			"duplicate stub",
			validBaseline + "stubs:\n  - path: \"GET /version\"\n    reason: \"a\"\n" +
				"  - path: \"GET /api2/json/version\"\n    reason: \"b\"\n",
			"already annotated by stubs[0]",
		},
		{
			"duplicate out_of_scope prefix",
			validBaseline + "out_of_scope:\n  - prefix: \"/pools\"\n    reason: \"a\"\n    doc: \"d\"\n" +
				"  - prefix: \"/pools\"\n    reason: \"b\"\n    doc: \"d\"\n",
			"already annotated by out_of_scope[0]",
		},
		{
			"duplicate allowlist route",
			validBaseline + "allow_unmatched_routes:\n  - route: \"GET /a\"\n    reason: \"a\"\n" +
				"  - route: \"GET /a\"\n    reason: \"b\"\n",
			"already allowed by allow_unmatched_routes[0]",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := coverage.ParseAnnotations([]byte(tc.yaml))
			if err == nil {
				t.Fatalf("ParseAnnotations = nil error, want one containing %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error =\n%v\nwant a message containing %q", err, tc.want)
			}
		})
	}
}

// Validation reports every problem at once — a hand-edited file is fixed in one
// pass, not one error per run.
func TestParseAnnotationsReportsEveryProblem(t *testing.T) {
	t.Parallel()
	_, err := coverage.ParseAnnotations([]byte(`baseline:
  pve_version: "9.2.4"
stubs:
  - path: "GET /version"
out_of_scope:
  - prefix: "/pools"
    reason: "r"
`))
	if err == nil {
		t.Fatal("ParseAnnotations = nil error, want an aggregate failure")
	}
	for _, want := range []string{
		"baseline.source is required",
		"baseline.captured is required",
		"stubs[0] (GET /version): reason is required",
		"doc is required",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("aggregate error is missing %q:\n%v", want, err)
		}
	}
}

// The lookup helpers are what the report and the guard consult.
func TestAnnotationLookups(t *testing.T) {
	t.Parallel()
	ann, err := coverage.ParseAnnotations([]byte(validBaseline + `
stubs:
  - path: "GET /nodes/{node}/ceph/mirror"
    reason: "rbd CLI only"
out_of_scope:
  - prefix: "/cluster/notifications"
    reason: "owned by the service, not the SDK"
    doc: "ADR-0001"
allow_unmatched_routes:
  - route: "POST /cluster/sdn/fabrics"
    reason: "baseline predates it"
`))
	if err != nil {
		t.Fatalf("ParseAnnotations: %v", err)
	}

	if reason, ok := ann.StubReason(coverage.Key{Method: "GET", Path: "/nodes/{}/ceph/mirror"}); !ok {
		t.Error("StubReason on an annotated stub = not found")
	} else if reason != "rbd CLI only" {
		t.Errorf("StubReason = %q, want %q", reason, "rbd CLI only")
	}
	if _, ok := ann.StubReason(coverage.Key{Method: "POST", Path: "/nodes/{}/ceph/mirror"}); ok {
		t.Error("StubReason matched a different method — stubs are per (method, path)")
	}

	if _, ok := ann.AllowsRoute(coverage.Key{Method: "POST", Path: "/cluster/sdn/fabrics"}); !ok {
		t.Error("AllowsRoute on an allowlisted route = not allowed")
	}
	if _, ok := ann.AllowsRoute(coverage.Key{Method: "GET", Path: "/cluster/sdn/fabrics"}); ok {
		t.Error("AllowsRoute matched a different method — the guard is per (method, path)")
	}

	if got, ok := ann.OutOfScopeFor("/cluster/notifications/endpoints/smtp"); !ok {
		t.Error("OutOfScopeFor on a child path = not found")
	} else if got.Doc != "ADR-0001" {
		t.Errorf("OutOfScopeFor().Doc = %q, want %q", got.Doc, "ADR-0001")
	}
	if _, ok := ann.OutOfScopeFor("/cluster/notificationsX"); ok {
		t.Error("OutOfScopeFor claimed /cluster/notificationsX — prefixes match whole segments")
	}
	if _, ok := ann.OutOfScopeFor("/cluster/ha"); ok {
		t.Error("OutOfScopeFor claimed an unrelated path")
	}
}

// LoadAnnotations reads from disk and names the file in its errors.
func TestLoadAnnotations(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "coverage-annotations.yaml")
	if err := os.WriteFile(path, []byte(validBaseline), 0o600); err != nil {
		t.Fatal(err)
	}
	ann, err := coverage.LoadAnnotations(path)
	if err != nil {
		t.Fatalf("LoadAnnotations: %v", err)
	}
	if got, want := ann.Baseline.Source, "r740a apidoc.js"; got != want {
		t.Errorf("Baseline.Source = %q, want %q", got, want)
	}

	if _, err := coverage.LoadAnnotations(filepath.Join(dir, "absent.yaml")); err == nil {
		t.Error("LoadAnnotations on a missing file = nil error")
	}

	bad := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(bad, []byte("stubs: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	switch _, err := coverage.LoadAnnotations(bad); {
	case err == nil:
		t.Error("LoadAnnotations on an invalid file = nil error")
	case !strings.Contains(err.Error(), "bad.yaml"):
		t.Errorf("error %q does not name the file", err)
	}
}
