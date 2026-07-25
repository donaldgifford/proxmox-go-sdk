package coverage

import (
	"os"
	"slices"
	"testing"

	"github.com/donaldgifford/proxmox-go-sdk/cmd/pve-schemadiff/schema"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/mockpve"
)

// baselinePath is the committed real-PVE endpoint set. Several tests below run
// the mapper over the whole thing rather than over hand-written examples: the
// rule table's job is to classify THIS baseline, so the baseline is the fixture.
const baselinePath = "../testdata/baseline.json"

// pinnedUnassignedFamilies is the current [Unassigned] set, by API family and
// endpoint count — the SDK's untriaged surface as of the committed baseline
// (IMPL-0006 OQ-4a keeps these visible as gaps rather than declaring them out of
// scope). The pin is what makes an unmapped family loud: a PVE minor that adds
// one fails TestUnassignedFamiliesArePinned with the new paths named, instead of
// quietly swelling a bucket nobody reads.
//
// Shrinking this map is the point. When a service takes a family over, add a
// rule to serviceRules and delete the entry here.
var pinnedUnassignedFamilies = map[string]int{
	"/cluster/bulk-action":   6,  // Bulk guest start/shutdown/suspend/migrate.
	"/cluster/jobs":          7,  // Realm-sync jobs, schedule-analyze.
	"/cluster/mapping":       16, // PCI/USB/dir hardware maps for passthrough.
	"/cluster/notifications": 31, // Notification endpoints, matchers, targets.
	"/pools":                 7,  // Resource pools.
}

// ServiceFor classifies a representative endpoint from every mapped service,
// including the paths whose owner is not guessable from the path alone: PVE
// hangs replication off both /cluster and /nodes, ACME accounts off /cluster
// while the SDK drives them from the nodes service, and vzdump off /nodes while
// backups belong to pbs.
func TestServiceForAssignsFamilies(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path string
		want string
	}{
		{"/version", "version"},
		{"/nodes/{}/tasks/{}/status", "tasks"},
		{"/cluster/tasks", "tasks"},
		{"/nodes/{}/qemu/{}/config", "qemu"},
		{"/cluster/qemu/cpu", "qemu"},
		{"/nodes/{}/lxc/{}/snapshot", "lxc"},
		{"/storage/{}", "storage"},
		{"/nodes/{}/storage/{}/content", "storage"},
		{"/nodes/{}/disks/zfs", "storage"},
		{"/nodes/{}/scan/nfs", "storage"},
		{"/cluster/ha/resources/{}", "ha"},
		{"/cluster/replication/{}", "ha"},
		{"/nodes/{}/replication/{}/status", "ha"},
		{"/cluster/sdn/fabrics/fabric/{}", "sdn"},
		{"/nodes/{}/sdn/zones", "sdn"},
		{"/cluster/firewall/rules", "firewall"},
		{"/nodes/{}/firewall/options", "firewall"},
		{"/cluster", "cluster"},
		{"/cluster/config/nodes/{}", "cluster"},
		{"/cluster/options", "cluster"},
		{"/access/users/{}/token/{}", "access"},
		{"/access/tfa", "access"},
		{"/nodes", "nodes"},
		{"/nodes/{}", "nodes"},
		{"/nodes/{}/network/{}", "nodes"},
		{"/nodes/{}/apt/update", "nodes"},
		{"/nodes/{}/certificates/acme/certificate", "nodes"},
		{"/cluster/acme/account/{}", "nodes"},
		{"/nodes/{}/services/{}/restart", "nodes"},
		{"/cluster/ceph/flags", "ceph"},
		{"/nodes/{}/ceph/osd", "ceph"},
		{"/cluster/backup/{}", "pbs"},
		{"/cluster/backup-info/not-backed-up", "pbs"},
		{"/nodes/{}/vzdump", "pbs"},
		{"/cluster/metrics/server/{}", "metrics"},
		{"/nodes/{}/rrddata", "metrics"},
		{"/nodes/{}/status", "metrics"},
		{"/nodes/{}/termproxy", "console"},
		{"/nodes/{}/vncwebsocket", "console"},
	}
	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			if got := ServiceFor(tc.path); got != tc.want {
				t.Errorf("ServiceFor(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}

// A rule claims whole path segments only, so a longer literal that merely starts
// with a mapped prefix is not swept in.
func TestServiceForMatchesWholeSegments(t *testing.T) {
	t.Parallel()
	for _, path := range []string{
		"/nodes/{}/qemuextra",   // Not /nodes/{}/qemu.
		"/storagefoo",           // Not /storage.
		"/versioning",           // Not /version.
		"/cluster/haproxy",      // Not /cluster/ha.
		"/nodes/{}/networkless", // Not /nodes/{}/network.
	} {
		if got := ServiceFor(path); got != Unassigned {
			t.Errorf("ServiceFor(%q) = %q, want %q — prefixes must match whole segments",
				path, got, Unassigned)
		}
	}
}

// The bare /cluster, /nodes, and /nodes/{} rules exist only to claim their own
// index endpoints. If any of them ever became a subtree rule, every unmapped
// family under it would silently join that service instead of surfacing in
// [Unassigned] — the failure this test exists to catch.
func TestExactRulesDoNotClaimSubtrees(t *testing.T) {
	t.Parallel()
	for _, path := range []string{
		"/cluster/notifications",
		"/cluster/mapping/pci",
		"/nodes/{}/hypothetical-new-family",
	} {
		if got := ServiceFor(path); got != Unassigned {
			t.Errorf("ServiceFor(%q) = %q, want %q — /cluster and /nodes must stay exact-match",
				path, got, Unassigned)
		}
	}
}

// Every unassigned endpoint in the real baseline belongs to a pinned family, and
// each family has exactly the pinned endpoint count.
func TestUnassignedFamiliesArePinned(t *testing.T) {
	t.Parallel()
	counts := make(map[string]int, len(pinnedUnassignedFamilies))
	for _, ep := range loadBaseline(t) {
		key := EndpointKey(ep)
		if ServiceFor(key.Path) != Unassigned {
			continue
		}
		fam, ok := familyOf(key.Path)
		if !ok {
			t.Errorf("%v is unassigned and belongs to no pinned family — map it in serviceRules "+
				"or add its family to pinnedUnassignedFamilies", key)
			continue
		}
		counts[fam]++
	}
	for fam, want := range pinnedUnassignedFamilies {
		if got := counts[fam]; got != want {
			t.Errorf("unassigned family %s holds %d endpoints, pinned at %d", fam, got, want)
		}
	}
	for fam := range counts {
		if _, ok := pinnedUnassignedFamilies[fam]; !ok {
			t.Errorf("family %s is unassigned but not pinned", fam)
		}
	}
}

// A covered endpoint must never land in [Unassigned]: the SDK implements it, so
// some service owns it, and leaving it unmapped would hide real coverage in the
// one section readers treat as debt. This keeps the rule table honest as the SDK
// grows — implementing an unmapped family fails here until it is mapped.
func TestNoCoveredEndpointIsUnassigned(t *testing.T) {
	t.Parallel()
	covered := coveredKeys(t)
	for _, ep := range loadBaseline(t) {
		key := EndpointKey(ep)
		if covered[key] && ServiceFor(key.Path) == Unassigned {
			t.Errorf("%v is covered by a mockpve route but maps to %q — add a rule for its family",
				key, Unassigned)
		}
	}
}

// A rule matching nothing in the baseline is dead weight, and the likeliest
// cause is a typo in the prefix — which would silently park a real family in
// [Unassigned] instead.
func TestEveryRuleMatchesTheBaseline(t *testing.T) {
	t.Parallel()
	baseline := loadBaseline(t)
	for _, r := range serviceRules {
		matched := slices.ContainsFunc(baseline, func(ep schema.Endpoint) bool {
			return r.matches(NormalizePath(ep.Path))
		})
		if !matched {
			t.Errorf("rule %q → %q matches no baseline endpoint (typo, or the family is gone)",
				r.prefix, r.service)
		}
	}
}

// Services is the report's section order: sorted, [Unassigned] last, and every
// service the table can assign appears exactly once.
func TestServicesIsSortedWithUnassignedLast(t *testing.T) {
	t.Parallel()
	got := Services()
	if last := got[len(got)-1]; last != Unassigned {
		t.Errorf("Services() ends with %q, want %q last", last, Unassigned)
	}
	named := got[:len(got)-1]
	if !slices.IsSorted(named) {
		t.Errorf("Services() = %v, want the named services sorted", named)
	}
	for _, r := range serviceRules {
		if !slices.Contains(named, r.service) {
			t.Errorf("Services() omits %q, which serviceRules can assign", r.service)
		}
	}
	if len(slices.Compact(slices.Clone(named))) != len(named) {
		t.Errorf("Services() = %v, want no duplicates", named)
	}
}

// familyOf returns the pinned unassigned family owning path. It reuses
// serviceRule's segment-aware matching so a family prefix cannot claim a longer
// literal, e.g. /pools must not swallow a future /poolsomething.
func familyOf(path string) (string, bool) {
	for fam := range pinnedUnassignedFamilies {
		if (serviceRule{prefix: fam}).matches(path) {
			return fam, true
		}
	}
	return "", false
}

// loadBaseline reads the committed baseline of real PVE endpoints.
func loadBaseline(t *testing.T) []schema.Endpoint {
	t.Helper()
	raw, err := os.ReadFile(baselinePath)
	if err != nil {
		t.Fatalf("read baseline: %v", err)
	}
	endpoints, err := schema.UnmarshalBaseline(raw)
	if err != nil {
		t.Fatalf("parse baseline: %v", err)
	}
	return endpoints
}

// coveredKeys is the mock's route table as a lookup set — the coverage
// numerator.
func coveredKeys(t *testing.T) map[Key]bool {
	t.Helper()
	keys, err := ParsePatterns(mockpve.New().Routes())
	if err != nil {
		t.Fatalf("parse mockpve routes: %v", err)
	}
	set := make(map[Key]bool, len(keys))
	for _, k := range keys {
		set[k] = true
	}
	return set
}
