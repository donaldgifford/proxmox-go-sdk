// Package firewall wraps the Proxmox VE firewall at any of its three scopes —
// the datacenter (cluster), a node, or a single guest. PVE exposes a near
// identical rule / IPSet / options surface at each level, differing only in the
// path prefix, so this package models it with ONE [Service] carrying a scope
// rather than three near-duplicate types.
//
// The one real asymmetry: PVE serves no IPSet API at node scope (a node's
// firewall is rules, options and the log only), so at that scope every IPSet
// method returns a pverr.ErrUnsupported-wrapped error before issuing a request
// rather than letting a 404 back out.
//
// Pick a scope with one of the constructors — [NewClusterScope],
// [NewNodeScope], or [NewGuestScope] — or with the root client's Firewall,
// NodeFirewall, and GuestFirewall accessors. Every method then operates within
// that scope (there is no scope argument):
//
//   - Rules: the ordered rule table, addressed by position (0 is evaluated
//     first) — [Service.ListRules], [Service.CreateRule], [Service.UpdateRule],
//     [Service.DeleteRule].
//   - IPSets: named CIDR sets a rule can reference — [Service.ListIPSets],
//     [Service.CreateIPSet], [Service.AddIPSetEntry], and the 9.1-gated
//     [Service.RenameIPSet] (overlapping-IPSet support). Cluster and guest scope
//     only.
//   - Options: the scoped options block (enable flag, default policies, logging)
//     — [Service.GetOptions] and [Service.SetOptions].
//
// All firewall config writes are synchronous: they return an error, not a
// task reference. Reads are lossless — keys the SDK does not model are preserved
// in each type's Extra map, since the option and rule key sets differ by scope.
//
// One *Service is safe for concurrent use.
package firewall
