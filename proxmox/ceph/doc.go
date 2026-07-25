// Package ceph wraps Proxmox VE 9.x (Squid) Ceph management: pools, OSDs, and
// cluster status.
//
// Ceph is a single cluster-wide entity, but its REST endpoints are addressed
// through a MON node, so every operation takes the node used to reach the
// cluster as a per-call argument — the root client's Ceph accessor itself takes
// no node. One Service is safe for concurrent use.
//
//   - Pools: ListPools / GetPool plus CreatePool and DeletePool, which run as
//     workers and return a tasks.Ref to await.
//   - OSDs: ListOSDs returns the CRUSH tree; CreateOSD and DestroyOSD run as
//     workers.
//   - Cluster: GetStatus (health + maps, lossless) and GetClusterConfig (the
//     ceph.conf, returned verbatim as text).
//
// The Ceph features are baseline 9.0, so nothing is version-gated. The REST
// paths are confirmed against the real 9.2 apidoc and pinned by
// TestCephPathsReal (two of them were wrong until the IMPL-0006 coverage guard
// caught it: the pool collection is singular, and the cluster config is
// /ceph/cfg/raw). Response SHAPES are still provisional — unverified against a
// live cluster in this environment — so reads preserve any unmodelled keys in an
// Extra map and nothing is lost.
//
// RBD mirroring is a Ceph-side (`rbd mirror` CLI) feature with no confirmed PVE
// REST endpoint, so GetMirrorStatus / EnableMirroring / DisableMirroring return
// pverr.ErrUnsupported (drive it over the SSH side-channel meanwhile). The
// method signatures are stable, so they become real calls without a change if
// PVE ever exposes the endpoint.
//
// See docs/design/0001-proxmox-sdk-package-layout.md and
// docs/impl/0001-proxmox-ve-9x-sdk-coverage.md.
package ceph
