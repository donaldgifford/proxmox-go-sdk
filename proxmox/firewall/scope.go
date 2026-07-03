package firewall

import "strconv"

// ScopeKind is the level a firewall Service operates at: the datacenter
// (cluster), a single node, or a single guest.
type ScopeKind string

// The firewall scope kinds.
const (
	ScopeCluster ScopeKind = "cluster"
	ScopeNode    ScopeKind = "node"
	ScopeGuest   ScopeKind = "guest"
)

// GuestKind is the guest type of a guest-scoped firewall: a QEMU VM or an LXC
// container. It selects the /qemu/ or /lxc/ path segment.
type GuestKind string

// The guest kinds a guest-scoped firewall can target.
const (
	GuestQEMU GuestKind = "qemu"
	GuestLXC  GuestKind = "lxc"
)

// scope identifies which firewall a Service targets. It is unexported: callers
// build a scoped Service through NewClusterScope, NewNodeScope, or
// NewGuestScope rather than constructing a scope directly.
type scope struct {
	kind  ScopeKind
	node  string    // node and guest scopes.
	guest GuestKind // guest scope.
	vmid  int       // guest scope.
}

// path returns the REST prefix every firewall endpoint hangs off for this
// scope: /cluster/firewall, /nodes/{node}/firewall, or
// /nodes/{node}/{qemu|lxc}/{vmid}/firewall. All the rule, IPSet, and options
// paths are built on top of it, so the scope logic lives in exactly one place.
func (s scope) path() string {
	switch s.kind {
	case ScopeNode:
		return "/nodes/" + s.node + "/firewall"
	case ScopeGuest:
		return "/nodes/" + s.node + "/" + string(s.guest) + "/" + strconv.Itoa(s.vmid) + "/firewall"
	default: // ScopeCluster
		return "/cluster/firewall"
	}
}
