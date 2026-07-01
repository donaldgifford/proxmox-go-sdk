// Package sdn wraps Proxmox VE software-defined networking: zones, VNets and
// their subnets, fabrics, and the cluster-wide apply that activates staged
// changes. SDN is cluster-scoped — every endpoint lives under /cluster/sdn and
// binds no node. Construct the [Service] with [NewService] or via the root
// client's SDN accessor; one *Service is safe for concurrent use.
//
// SDN configuration is transactional. Creates, updates, and deletes stage
// changes into a pending config; [Service.ApplySDN] commits them cluster-wide.
// All config writes are synchronous (they return an error, not a task
// reference). Reads are lossless — keys the SDK does not model are preserved in
// each type's Extra map, since zone and fabric configs are type/protocol
// dependent.
//
//   - Zones ([Service.CreateZone], …): the backing technology — simple, VLAN,
//     QinQ, VXLAN, or EVPN ([ZoneType]).
//   - VNets ([Service.CreateVNet], …) and their nested subnets
//     ([Service.CreateSubnet], …).
//   - Fabrics ([Service.CreateFabric], …): the OpenFabric/OSPF routing layer
//     (9.0+). The REST surface is provisional (see [Fabric]); the 9.2 BGP
//     protocol is gated via version.Capabilities.SDNAdvancedFabrics.
//
// Live status ([Service.SDNStatus], [Service.VNetStatus]) has no confirmed PVE
// REST endpoint and returns pverr.ErrUnsupported until one is.
package sdn
