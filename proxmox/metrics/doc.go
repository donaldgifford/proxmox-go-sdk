// Package metrics wraps Proxmox VE 9.x metric reads and external metric-server
// configuration.
//
// The service scope is mixed. RRD time series and node status are node- (or
// guest-) scoped and take the node as a per-call argument:
//
//   - GetNodeRRD / GetVMRRD return the RRD series (CPU, memory, network, disk).
//     Tune the window with WithTimeframe and WithConsolidation.
//   - GetNodeStatus returns the node's live health block.
//
// Reads are lossless: the common metrics are typed and every other key —
// including pressure-stall counters and ZFS-ARC statistics, which are
// REST-with-caveat (present on some 9.x releases, shape unconfirmed) — is
// preserved in an Extra map.
//
// External metric servers (InfluxDB / Graphite sinks) are cluster-scoped
// (/cluster/metrics/server); ListMetricServers and the Create/Update/Delete
// writes are synchronous (they return an error, not a tasks.Ref).
//
// The 9.1 OpenTelemetry exporter is configured through files, not the REST API,
// so GetOTelConfig and SetOTelConfig return pverr.ErrUnsupported (the
// version.Capabilities.OTelExporter gate is reserved for the day PVE exposes a
// REST surface).
//
// Construct a Service with NewService or the root client's Metrics accessor; one
// *Service is safe for concurrent use.
//
// See docs/design/0001-proxmox-sdk-package-layout.md and
// docs/impl/0001-proxmox-ve-9x-sdk-coverage.md.
package metrics
