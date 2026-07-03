package cluster

import (
	"encoding/json"
	"fmt"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/internal/svcutil"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/types"
)

// ResourceType is a /cluster/resources filter value.
type ResourceType string

// The resource kinds /cluster/resources can be filtered to.
const (
	ResourceTypeVM      ResourceType = "vm"
	ResourceTypeStorage ResourceType = "storage"
	ResourceTypeNode    ResourceType = "node"
	ResourceTypeSDN     ResourceType = "sdn"
)

// Resource is one entry from GET /cluster/resources — a VM, container, storage,
// node, pool, or SDN object. Which fields are populated depends on Type, so
// reads are lossless: unmodelled keys are preserved in Extra.
type Resource struct {
	ID      string  `json:"id"`
	Type    string  `json:"type"`
	Node    string  `json:"node,omitempty"`
	Status  string  `json:"status,omitempty"`
	Name    string  `json:"name,omitempty"`
	VMID    int     `json:"vmid,omitempty"`
	Storage string  `json:"storage,omitempty"`
	MaxCPU  float64 `json:"maxcpu,omitempty"`
	MaxMem  int64   `json:"maxmem,omitempty"`
	// Extra carries resource keys the SDK does not model.
	Extra map[string]string `json:"-"`
}

var resourceKnownFields = map[string]bool{
	"id": true, "type": true, "node": true, "status": true, "name": true,
	"vmid": true, "storage": true, "maxcpu": true, "maxmem": true,
}

// UnmarshalJSON decodes the modelled fields and routes unknown keys into Extra.
func (r *Resource) UnmarshalJSON(data []byte) error {
	type alias Resource
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("decode cluster resource: %w", err)
	}
	*r = Resource(a)
	extra, err := svcutil.DecodeExtra(data, resourceKnownFields)
	if err != nil {
		return fmt.Errorf("decode cluster resource: %w", err)
	}
	r.Extra = extra
	return nil
}

// StatusEntry is one entry from GET /cluster/status. The list holds one entry
// with Type "cluster" (Name, Nodes, Quorate) and one Type "node" entry per
// member (NodeID, IP, Online, Local). Reads are lossless.
type StatusEntry struct {
	ID      string        `json:"id"`
	Type    string        `json:"type"`
	Name    string        `json:"name,omitempty"`
	Nodes   int           `json:"nodes,omitempty"`   // cluster entry.
	Quorate types.PVEBool `json:"quorate,omitempty"` // cluster entry.
	Version int           `json:"version,omitempty"` // cluster entry.
	NodeID  int           `json:"nodeid,omitempty"`  // node entry.
	IP      string        `json:"ip,omitempty"`      // node entry.
	Online  types.PVEBool `json:"online,omitempty"`  // node entry.
	Local   types.PVEBool `json:"local,omitempty"`   // node entry.
	Level   string        `json:"level,omitempty"`
	// Extra carries status keys the SDK does not model.
	Extra map[string]string `json:"-"`
}

var statusKnownFields = map[string]bool{
	"id": true, "type": true, "name": true, "nodes": true, "quorate": true,
	"version": true, "nodeid": true, "ip": true, "online": true,
	"local": true, "level": true,
}

// UnmarshalJSON decodes the modelled fields and routes unknown keys into Extra.
func (e *StatusEntry) UnmarshalJSON(data []byte) error {
	type alias StatusEntry
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("decode cluster status: %w", err)
	}
	*e = StatusEntry(a)
	extra, err := svcutil.DecodeExtra(data, statusKnownFields)
	if err != nil {
		return fmt.Errorf("decode cluster status: %w", err)
	}
	e.Extra = extra
	return nil
}

// Options is the datacenter options block from GET /cluster/options. The key set
// is broad and version-dependent (HA's crs string, migration settings, console
// type, …), so reads are lossless via Extra — e.g. the HA "crs" property-string
// surfaces in Extra since the ha package owns it.
type Options struct {
	Description string `json:"description,omitempty"`
	Migration   string `json:"migration,omitempty"`
	Console     string `json:"console,omitempty"`
	Keyboard    string `json:"keyboard,omitempty"`
	Language    string `json:"language,omitempty"`
	MacPrefix   string `json:"mac_prefix,omitempty"`
	// Extra carries options keys the SDK does not model (including "crs").
	Extra map[string]string `json:"-"`
}

var optionsKnownFields = map[string]bool{
	"description": true, "migration": true, "console": true,
	"keyboard": true, "language": true, "mac_prefix": true,
}

// UnmarshalJSON decodes the modelled fields and routes unknown keys into Extra.
func (o *Options) UnmarshalJSON(data []byte) error {
	type alias Options
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("decode cluster options: %w", err)
	}
	*o = Options(a)
	extra, err := svcutil.DecodeExtra(data, optionsKnownFields)
	if err != nil {
		return fmt.Errorf("decode cluster options: %w", err)
	}
	o.Extra = extra
	return nil
}

// OptionsUpdate is the body of PUT /cluster/options. All fields are optional;
// use Delete to unset keys. Pass it by pointer.
type OptionsUpdate struct {
	Description string `json:"description,omitempty"`
	Migration   string `json:"migration,omitempty"`
	Console     string `json:"console,omitempty"`
	Keyboard    string `json:"keyboard,omitempty"`
	Language    string `json:"language,omitempty"`
	Delete      string `json:"delete,omitempty"`
	// Extra carries PVE parameters the SDK does not model.
	Extra map[string]string `json:"-"`
}

// ResourceFilter narrows ListResources. Build one with WithResourceType.
type ResourceFilter func(*resourceQuery)

type resourceQuery struct {
	typ ResourceType
}

// WithResourceType filters ListResources to one kind (vm, storage, node, sdn).
func WithResourceType(t ResourceType) ResourceFilter {
	return func(q *resourceQuery) { q.typ = t }
}
