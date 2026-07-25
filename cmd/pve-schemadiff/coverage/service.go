package coverage

import (
	"slices"
	"strings"
)

// Unassigned is the bucket for endpoints no rule claims — API families no SDK
// service has taken ownership of. It is a real section in the report, not a
// swept-under-the-rug default, and it is why the rule table below has no
// catch-all for /cluster or /nodes: with one, a PVE minor that adds an API
// family would silently join an existing service's table instead of surfacing.
// Anything landing here is either untriaged debt (the current contents, per
// IMPL-0006 OQ-4a) or brand-new upstream surface, and both want a human
// decision. TestUnassignedSetIsPinned fails when the set changes.
const Unassigned = "unassigned"

// serviceRule assigns a normalized path prefix to a service. exact restricts the
// rule to the prefix itself, which is how the bare index endpoints (GET /cluster,
// GET /nodes) are claimed without turning their whole subtree into a catch-all.
type serviceRule struct {
	prefix  string
	service string
	exact   bool
}

// serviceRules is the static path-family → service table, evaluated
// longest-prefix-first.
//
// Two conventions matter when reading it:
//
// Services are **API path families, not SDK packages**. PVE hangs a guest's
// console and firewall endpoints off /nodes/{}/qemu, so they count under qemu
// even though the SDK implements them in the console and firewall packages.
// Grouping by path keeps the report a map of the API, which is what it measures.
//
// A family is mapped when an SDK service **owns that domain**, whether or not
// any of it is covered yet: /access/tfa maps to access even though the SDK
// implements none of it, so it reads as an access gap rather than an orphan.
// Ownership, not coverage, is the test — which leaves [Unassigned] meaning
// exactly "no service claims this".
var serviceRules = []serviceRule{
	// Foundation.
	{prefix: "/version", service: "version"},
	{prefix: "/nodes/{}/tasks", service: "tasks"},
	{prefix: "/cluster/tasks", service: "tasks"},

	// Compute. /cluster/qemu is the cluster-wide QEMU config (cpu-flags,
	// custom-cpu-models).
	{prefix: "/nodes/{}/qemu", service: "qemu"},
	{prefix: "/cluster/qemu", service: "qemu"},
	{prefix: "/nodes/{}/lxc", service: "lxc"},

	// Storage. PVE hangs disk/ZFS management off /nodes/{}/disks and the
	// template/ISO discovery helpers off /scan, /aplinfo, and the two query-*
	// endpoints; the SDK's storage service owns all of it.
	{prefix: "/storage", service: "storage"},
	{prefix: "/nodes/{}/storage", service: "storage"},
	{prefix: "/nodes/{}/disks", service: "storage"},
	{prefix: "/nodes/{}/scan", service: "storage"},
	{prefix: "/nodes/{}/aplinfo", service: "storage"},
	{prefix: "/nodes/{}/query-url-metadata", service: "storage"},
	{prefix: "/nodes/{}/query-oci-repo-tags", service: "storage"},

	// HA, including replication (the SDK's ha service owns replication jobs).
	{prefix: "/cluster/ha", service: "ha"},
	{prefix: "/cluster/replication", service: "ha"},
	{prefix: "/nodes/{}/replication", service: "ha"},

	// SDN and firewall.
	{prefix: "/cluster/sdn", service: "sdn"},
	{prefix: "/nodes/{}/sdn", service: "sdn"},
	{prefix: "/cluster/firewall", service: "firewall"},
	{prefix: "/nodes/{}/firewall", service: "firewall"},

	// Cluster. Bare /cluster is exact so the untriaged /cluster families
	// (notifications, mapping, jobs, bulk-action) stay unassigned.
	{prefix: "/cluster", service: "cluster", exact: true},
	{prefix: "/cluster/config", service: "cluster"},
	{prefix: "/cluster/options", service: "cluster"},
	{prefix: "/cluster/resources", service: "cluster"},
	{prefix: "/cluster/status", service: "cluster"},
	{prefix: "/cluster/log", service: "cluster"},
	{prefix: "/cluster/nextid", service: "cluster"},

	// Access control: the whole /access subtree, plus cluster ACME accounts,
	// which the SDK's nodes service drives alongside node certificates.
	{prefix: "/access", service: "access"},

	// Node administration and networking.
	{prefix: "/nodes", service: "nodes", exact: true},
	{prefix: "/nodes/{}", service: "nodes", exact: true},
	{prefix: "/nodes/{}/network", service: "nodes"},
	{prefix: "/nodes/{}/apt", service: "nodes"},
	{prefix: "/nodes/{}/certificates", service: "nodes"},
	{prefix: "/cluster/acme", service: "nodes"},
	{prefix: "/nodes/{}/services", service: "nodes"},
	{prefix: "/nodes/{}/dns", service: "nodes"},
	{prefix: "/nodes/{}/hosts", service: "nodes"},
	{prefix: "/nodes/{}/time", service: "nodes"},
	{prefix: "/nodes/{}/subscription", service: "nodes"},
	{prefix: "/nodes/{}/syslog", service: "nodes"},
	{prefix: "/nodes/{}/journal", service: "nodes"},
	{prefix: "/nodes/{}/report", service: "nodes"},
	{prefix: "/nodes/{}/execute", service: "nodes"},
	{prefix: "/nodes/{}/config", service: "nodes"},
	{prefix: "/nodes/{}/hardware", service: "nodes"},
	{prefix: "/nodes/{}/capabilities", service: "nodes"},
	{prefix: "/nodes/{}/version", service: "nodes"},
	{prefix: "/nodes/{}/wakeonlan", service: "nodes"},
	{prefix: "/nodes/{}/startall", service: "nodes"},
	{prefix: "/nodes/{}/stopall", service: "nodes"},
	{prefix: "/nodes/{}/suspendall", service: "nodes"},
	{prefix: "/nodes/{}/migrateall", service: "nodes"},

	// Ceph.
	{prefix: "/cluster/ceph", service: "ceph"},
	{prefix: "/nodes/{}/ceph", service: "ceph"},

	// Backup: the PVE side only (vzdump plus the cluster backup jobs); the
	// PBS-native datastore API is a future separate client.
	{prefix: "/cluster/backup", service: "pbs"},
	{prefix: "/cluster/backup-info", service: "pbs"},
	{prefix: "/nodes/{}/vzdump", service: "pbs"},

	// Metrics. GET /nodes/{}/status is the node status read; the POST on the
	// same path is a node power action, and a path-family map cannot split them
	// by method — counting the read (which the SDK covers) under metrics beats
	// parking a covered endpoint in unassigned.
	{prefix: "/cluster/metrics", service: "metrics"},
	{prefix: "/nodes/{}/rrd", service: "metrics"},
	{prefix: "/nodes/{}/rrddata", service: "metrics"},
	{prefix: "/nodes/{}/status", service: "metrics"},
	{prefix: "/nodes/{}/netstat", service: "metrics"},

	// Console (node shells; guest consoles live under the guest's own family).
	{prefix: "/nodes/{}/vncshell", service: "console"},
	{prefix: "/nodes/{}/vncwebsocket", service: "console"},
	{prefix: "/nodes/{}/spiceshell", service: "console"},
	{prefix: "/nodes/{}/termproxy", service: "console"},
}

// ServiceFor returns the service owning a normalized path, or [Unassigned].
// Matching is longest-prefix over whole path segments, so /nodes/{}/qemu claims
// /nodes/{}/qemu/{}/config but never a hypothetical /nodes/{}/qemuX.
func ServiceFor(path string) string {
	best, bestLen := Unassigned, -1
	for _, r := range serviceRules {
		if len(r.prefix) <= bestLen || !r.matches(path) {
			continue
		}
		best, bestLen = r.service, len(r.prefix)
	}
	return best
}

// matches reports whether path falls under the rule.
func (r serviceRule) matches(path string) bool {
	if r.exact {
		return path == r.prefix
	}
	if !strings.HasPrefix(path, r.prefix) {
		return false
	}
	rest := path[len(r.prefix):]
	return rest == "" || strings.HasPrefix(rest, "/")
}

// Services returns every service the table can assign, sorted, with
// [Unassigned] last — the order report sections are rendered in, so the layout
// stays stable regardless of which endpoints a given baseline happens to hold.
// Alphabetical rather than hand-ordered on purpose: a hand-kept sequence is a
// second list to sync with the rule table, and a service missing from it would
// be a silent bug.
func Services() []string {
	out := make([]string, 0, len(serviceRules))
	for _, r := range serviceRules {
		if !slices.Contains(out, r.service) {
			out = append(out, r.service)
		}
	}
	slices.Sort(out)
	return append(out, Unassigned)
}
