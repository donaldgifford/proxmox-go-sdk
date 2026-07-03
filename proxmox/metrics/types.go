package metrics

import (
	"encoding/json"
	"fmt"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/internal/svcutil"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/types"
)

// VMKind selects the guest type for GetVMRRD: QEMU virtual machines or LXC
// containers. It is the path segment under /nodes/{node}.
type VMKind string

// The guest kinds a VM RRD query can target.
const (
	KindQEMU VMKind = "qemu"
	KindLXC  VMKind = "lxc"
)

// Timeframe is an RRD consolidation window (the "timeframe" query param).
type Timeframe string

// The RRD timeframes PVE exposes.
const (
	TimeframeHour  Timeframe = "hour"
	TimeframeDay   Timeframe = "day"
	TimeframeWeek  Timeframe = "week"
	TimeframeMonth Timeframe = "month"
	TimeframeYear  Timeframe = "year"
)

// ConsolidationFunc is the RRD consolidation function (the "cf" query param).
type ConsolidationFunc string

// The consolidation functions PVE exposes.
const (
	CFAverage ConsolidationFunc = "AVERAGE"
	CFMax     ConsolidationFunc = "MAX"
)

// RRDPoint is one sample from a node or guest RRD series. The common metrics are
// typed; pressure-stall (some 9.x releases) and ZFS-ARC counters are
// REST-with-caveat and land in Extra, so reads are lossless.
type RRDPoint struct {
	Time      int64   `json:"time"`
	CPU       float64 `json:"cpu,omitempty"`
	MaxCPU    float64 `json:"maxcpu,omitempty"`
	Mem       float64 `json:"mem,omitempty"`
	MaxMem    float64 `json:"maxmem,omitempty"`
	Disk      float64 `json:"disk,omitempty"`
	MaxDisk   float64 `json:"maxdisk,omitempty"`
	NetIn     float64 `json:"netin,omitempty"`
	NetOut    float64 `json:"netout,omitempty"`
	DiskRead  float64 `json:"diskread,omitempty"`
	DiskWrite float64 `json:"diskwrite,omitempty"`
	// Extra carries series keys the SDK does not model (loadavg, pressure-stall,
	// ZFS-ARC counters, …).
	Extra map[string]string `json:"-"`
}

var rrdPointKnownFields = map[string]bool{
	"time": true, "cpu": true, "maxcpu": true, "mem": true, "maxmem": true,
	"disk": true, "maxdisk": true, "netin": true, "netout": true,
	"diskread": true, "diskwrite": true,
}

// UnmarshalJSON decodes the modelled fields and routes unknown keys into Extra.
func (p *RRDPoint) UnmarshalJSON(data []byte) error {
	type alias RRDPoint
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("decode rrd point: %w", err)
	}
	*p = RRDPoint(a)
	extra, err := svcutil.DecodeExtra(data, rrdPointKnownFields)
	if err != nil {
		return fmt.Errorf("decode rrd point: %w", err)
	}
	p.Extra = extra
	return nil
}

// MemoryInfo is a total/used/free byte triple used by NodeStatus.
type MemoryInfo struct {
	Total int64 `json:"total,omitempty"`
	Used  int64 `json:"used,omitempty"`
	Free  int64 `json:"free,omitempty"`
}

// NodeStatus is the payload of GET /nodes/{node}/status: the node's live health.
// Reads are lossless — the many nested/version-specific keys land in Extra.
type NodeStatus struct {
	Uptime     int64       `json:"uptime,omitempty"`
	CPU        float64     `json:"cpu,omitempty"`
	Wait       float64     `json:"wait,omitempty"`
	LoadAvg    []string    `json:"loadavg,omitempty"`
	KVersion   string      `json:"kversion,omitempty"`
	PVEVersion string      `json:"pveversion,omitempty"`
	Memory     *MemoryInfo `json:"memory,omitempty"`
	Swap       *MemoryInfo `json:"swap,omitempty"`
	RootFS     *MemoryInfo `json:"rootfs,omitempty"`
	// Extra carries status keys the SDK does not model (cpuinfo, ksm, idle, …).
	Extra map[string]string `json:"-"`
}

var nodeStatusKnownFields = map[string]bool{
	"uptime": true, "cpu": true, "wait": true, "loadavg": true,
	"kversion": true, "pveversion": true, "memory": true, "swap": true,
	"rootfs": true,
}

// UnmarshalJSON decodes the modelled fields and routes unknown keys into Extra.
func (n *NodeStatus) UnmarshalJSON(data []byte) error {
	type alias NodeStatus
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("decode node status: %w", err)
	}
	*n = NodeStatus(a)
	extra, err := svcutil.DecodeExtra(data, nodeStatusKnownFields)
	if err != nil {
		return fmt.Errorf("decode node status: %w", err)
	}
	n.Extra = extra
	return nil
}

// MetricServer is one external metric target from
// GET /cluster/metrics/server[/{id}] (an InfluxDB or Graphite sink). Reads are
// lossless.
type MetricServer struct {
	ID      string        `json:"id"`
	Type    string        `json:"type,omitempty"` // influxdb or graphite.
	Server  string        `json:"server,omitempty"`
	Port    int           `json:"port,omitempty"`
	Disable types.PVEBool `json:"disable,omitempty"`
	// Extra carries type-specific keys the SDK does not model (bucket, token,
	// organization, path, influxdbproto, …).
	Extra map[string]string `json:"-"`
}

var metricServerKnownFields = map[string]bool{
	"id": true, "type": true, "server": true, "port": true, "disable": true,
}

// UnmarshalJSON decodes the modelled fields and routes unknown keys into Extra.
func (m *MetricServer) UnmarshalJSON(data []byte) error {
	type alias MetricServer
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("decode metric server: %w", err)
	}
	*m = MetricServer(a)
	extra, err := svcutil.DecodeExtra(data, metricServerKnownFields)
	if err != nil {
		return fmt.Errorf("decode metric server: %w", err)
	}
	m.Extra = extra
	return nil
}

// MetricServerSpec is the body of POST /cluster/metrics/server/{id}. ID, Type,
// Server, and Port are required. Type-specific parameters (bucket, token, …) go
// in Extra. Pass it by pointer.
type MetricServerSpec struct {
	ID      string         `json:"id"`
	Type    string         `json:"type"`
	Server  string         `json:"server"`
	Port    int            `json:"port"`
	Disable *types.PVEBool `json:"disable,omitempty"`
	// Extra carries PVE parameters the SDK does not model.
	Extra map[string]string `json:"-"`
}

// MetricServerUpdate is the body of PUT /cluster/metrics/server/{id}. All fields
// optional; use Delete to unset keys. Pass it by pointer.
type MetricServerUpdate struct {
	Server  string         `json:"server,omitempty"`
	Port    int            `json:"port,omitempty"`
	Disable *types.PVEBool `json:"disable,omitempty"`
	Delete  string         `json:"delete,omitempty"`
	// Extra carries PVE parameters the SDK does not model.
	Extra map[string]string `json:"-"`
}

// OTelConfig is the forward-compatible shape of an OpenTelemetry exporter
// config. It is defined so GetOTelConfig/SetOTelConfig have a stable signature;
// in PVE 9.x the exporter is file-configured and has no REST endpoint, so those
// methods return pverr.ErrUnsupported (see otel.go).
type OTelConfig struct {
	Endpoint string            `json:"endpoint,omitempty"`
	Protocol string            `json:"protocol,omitempty"`
	Enabled  types.PVEBool     `json:"enabled,omitempty"`
	Extra    map[string]string `json:"-"`
}
