package coverage_test

import (
	"strings"
	"testing"

	"github.com/donaldgifford/proxmox-go-sdk/cmd/pve-schemadiff/coverage"
	"github.com/donaldgifford/proxmox-go-sdk/cmd/pve-schemadiff/schema"
)

// NormalizePath strips the REST prefix the mock carries and erases placeholder
// names, which is what lets the two sides be compared at all.
func TestNormalizePath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		path string
		want string
	}{
		{"mock pattern loses the prefix", "/api2/json/nodes/{node}/qemu", "/nodes/{}/qemu"},
		{"baseline path has no prefix to lose", "/nodes/{node}/qemu", "/nodes/{}/qemu"},
		{"every placeholder is erased", "/api2/json/nodes/{node}/qemu/{vmid}/config", "/nodes/{}/qemu/{}/config"},
		{"placeholder name is irrelevant", "/nodes/{node}/qemu/{id}/config", "/nodes/{}/qemu/{}/config"},
		{"no placeholders", "/api2/json/version", "/version"},
		{"cluster path", "/api2/json/cluster/ha/status/current", "/cluster/ha/status/current"},
		{"adjacent placeholders", "/api2/json/nodes/{node}/{kind}/{vmid}/firewall", "/nodes/{}/{}/{}/firewall"},
		{"prefix appears only at the front", "/api2/json/nodes/{node}/api2/json", "/nodes/{}/api2/json"},
		{"literal segment resembling a placeholder is kept", "/cluster/sdn/fabrics/fabric", "/cluster/sdn/fabrics/fabric"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := coverage.NormalizePath(tc.path); got != tc.want {
				t.Errorf("NormalizePath(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}

// A mock pattern and the real endpoint it implements must produce the SAME Key
// even when the two sides spell the placeholder differently — the mismatch is
// real (PVE names the fabric id {id}, the mock names it {fabric}), and missing
// it would report a covered endpoint as a gap AND the mock route as fabricated.
func TestKeysAgreeAcrossPlaceholderNames(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		pattern  string // as mockpve registers it.
		endpoint schema.Endpoint
	}{
		{
			"vmid vs id",
			"GET /api2/json/nodes/{node}/qemu/{vmid}/config",
			schema.Endpoint{Method: "GET", Path: "/nodes/{node}/qemu/{id}/config"},
		},
		{
			"fabric vs fabric_id",
			"DELETE /api2/json/cluster/sdn/fabrics/fabric/{fabric}",
			schema.Endpoint{Method: "DELETE", Path: "/cluster/sdn/fabrics/fabric/{fabric_id}"},
		},
		{
			"sid vs id",
			"POST /api2/json/cluster/ha/resources/{sid}/migrate",
			schema.Endpoint{Method: "POST", Path: "/cluster/ha/resources/{id}/migrate"},
		},
		{
			"identical names still agree",
			"GET /api2/json/nodes/{node}/network",
			schema.Endpoint{Method: "GET", Path: "/nodes/{node}/network"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := coverage.ParsePattern(tc.pattern)
			if err != nil {
				t.Fatalf("ParsePattern(%q): %v", tc.pattern, err)
			}
			want := coverage.EndpointKey(tc.endpoint)
			if got != want {
				t.Errorf("ParsePattern(%q) = %v, EndpointKey(%v) = %v — keys must agree",
					tc.pattern, got, tc.endpoint, want)
			}
		})
	}
}

// Distinct endpoints must NOT collide: erasing placeholder names is only safe
// because the surrounding literal segments still discriminate.
func TestKeysStayDistinct(t *testing.T) {
	t.Parallel()
	patterns := []string{
		"GET /api2/json/nodes/{node}/qemu",
		"POST /api2/json/nodes/{node}/qemu",
		"GET /api2/json/nodes/{node}/qemu/{vmid}",
		"GET /api2/json/nodes/{node}/lxc/{vmid}",
		"GET /api2/json/nodes/{node}/qemu/{vmid}/config",
		"GET /api2/json/nodes/{node}/qemu/{vmid}/status/current",
	}
	keys, err := coverage.ParsePatterns(patterns)
	if err != nil {
		t.Fatalf("ParsePatterns: %v", err)
	}
	seen := make(map[coverage.Key]string, len(keys))
	for i, k := range keys {
		if prev, dup := seen[k]; dup {
			t.Errorf("%q and %q both normalize to %v", prev, patterns[i], k)
		}
		seen[k] = patterns[i]
	}
}

// ParsePattern rejects anything that is not "METHOD /path". A method-less
// pattern cannot be matched against the baseline (which is keyed by verb), and
// dropping it silently would inflate the coverage numerator.
func TestParsePatternRejectsMalformed(t *testing.T) {
	t.Parallel()
	for _, pattern := range []string{
		"",
		"/api2/json/version",          // no method.
		"GET",                         // no path.
		"GET api2/json/version",       // path not rooted.
		"GET  ",                       // method only, padded.
		"/api2/json/nodes/{node} GET", // reversed.
	} {
		if _, err := coverage.ParsePattern(pattern); err == nil {
			t.Errorf("ParsePattern(%q) = nil error, want a malformed-pattern error", pattern)
		}
	}
}

// The method is upper-cased and surrounding whitespace ignored, so a pattern
// written "get /x" still keys against the baseline's "GET".
func TestParsePatternNormalizesMethod(t *testing.T) {
	t.Parallel()
	got, err := coverage.ParsePattern("  get /api2/json/version  ")
	if err != nil {
		t.Fatalf("ParsePattern: %v", err)
	}
	if want := (coverage.Key{Method: "GET", Path: "/version"}); got != want {
		t.Errorf("ParsePattern = %v, want %v", got, want)
	}
}

// ParsePatterns reports the offending pattern rather than skipping it.
func TestParsePatternsReportsBadEntry(t *testing.T) {
	t.Parallel()
	_, err := coverage.ParsePatterns([]string{"GET /api2/json/version", "bogus"})
	if err == nil {
		t.Fatal("ParsePatterns = nil error, want a failure naming the bad pattern")
	}
	if !strings.Contains(err.Error(), "bogus") {
		t.Errorf("error %q does not name the offending pattern", err)
	}
}

// Key.String is the form report rows and guard errors use.
func TestKeyString(t *testing.T) {
	t.Parallel()
	k := coverage.Key{Method: "PUT", Path: "/nodes/{}/network"}
	if got, want := k.String(), "PUT /nodes/{}/network"; got != want {
		t.Errorf("Key.String() = %q, want %q", got, want)
	}
}
