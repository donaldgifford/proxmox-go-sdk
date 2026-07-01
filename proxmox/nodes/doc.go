// Package nodes wraps per-node administration. Every operation is node-scoped:
// the node name is a per-call argument (the [Service] binds none), so one
// service serves the whole cluster. Construct it with [NewService] or via the
// root client's Nodes accessor; one *Service is safe for concurrent use.
//
// Node networking is the first surface (Phase 5): the configured interfaces at
// /nodes/{node}/network — bridges, bonds, VLANs, and physical NICs.
//
//   - Read: [Service.ListInterfaces], [Service.GetInterface] (lossless
//     [Interface]).
//   - Write: [Service.CreateInterface], [Service.UpdateInterface],
//     [Service.DeleteInterface]. These stage changes into the pending network
//     config; they are synchronous (return an error, no task).
//   - [Service.ApplyNetworkConfig] activates the pending config. PVE may reload
//     synchronously (a zero tasks.Ref) or via a worker (a non-zero Ref to
//     await) — check tasks.Ref.IsZero.
//
// Later phases extend the same Service with node status, package updates, disks,
// and certificates.
package nodes
