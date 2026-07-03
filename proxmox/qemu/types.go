package qemu

import (
	"encoding/json"
	"fmt"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/types"
)

// VM is one entry from GET /nodes/{node}/qemu — the per-node VM summary list.
// Only the fields PVE reliably returns in the listing are modelled; fields the
// SDK does not know are discarded on this read (use Config for the full set).
type VM struct {
	VMID     types.VMID       `json:"vmid"`
	Name     string           `json:"name,omitempty"`
	Status   types.PowerState `json:"status"`
	CPUs     int              `json:"cpus,omitempty"`
	MaxMem   int64            `json:"maxmem,omitempty"`  // bytes.
	MaxDisk  int64            `json:"maxdisk,omitempty"` // bytes.
	Uptime   int64            `json:"uptime,omitempty"`  // seconds.
	Template types.PVEBool    `json:"template,omitempty"`
}

// VMStatus is the runtime status from
// GET /nodes/{node}/qemu/{vmid}/status/current.
type VMStatus struct {
	VMID      types.VMID       `json:"vmid"`
	Name      string           `json:"name,omitempty"`
	Status    types.PowerState `json:"status"`
	QMPStatus string           `json:"qmpstatus,omitempty"` // "running", "paused", …
	Uptime    int64            `json:"uptime,omitempty"`    // seconds.
	CPUs      int              `json:"cpus,omitempty"`
	CPU       float64          `json:"cpu,omitempty"` // utilisation ratio 0.0–1.0.
	Mem       int64            `json:"mem,omitempty"` // bytes in use.
	MaxMem    int64            `json:"maxmem,omitempty"`
	Disk      int64            `json:"disk,omitempty"`
	MaxDisk   int64            `json:"maxdisk,omitempty"`
	NetIn     int64            `json:"netin,omitempty"`
	NetOut    int64            `json:"netout,omitempty"`
	DiskRead  int64            `json:"diskread,omitempty"`
	DiskWrite int64            `json:"diskwrite,omitempty"`
	PID       int              `json:"pid,omitempty"`
}

// configKnownFields lists the JSON keys that map to typed Config fields. Keys
// outside this set are collected into Config.Extra so reads are lossless. Add an
// entry here whenever a field is added to Config.
var configKnownFields = map[string]bool{
	"vmid": true, "name": true, "description": true,
	"cores": true, "sockets": true, "memory": true, "balloon": true,
	"cpu": true, "boot": true, "ostype": true,
	"scsi0": true, "scsi1": true, "net0": true, "net1": true,
	"agent": true, "onboot": true, "template": true, "protection": true,
}

// Config is the VM configuration from GET /nodes/{node}/qemu/{vmid}/config. PVE
// exposes 100+ config keys; the core subset is modelled as typed fields and the
// remainder is preserved verbatim in Extra, so a read never silently drops data.
//
// Config is a read type: it has a custom UnmarshalJSON and must not be marshalled
// back to PVE. Use ConfigUpdate to write configuration.
type Config struct {
	VMID        types.VMID    `json:"vmid,omitempty"`
	Name        string        `json:"name,omitempty"`
	Description string        `json:"description,omitempty"`
	Cores       int           `json:"cores,omitempty"`
	Sockets     int           `json:"sockets,omitempty"`
	Memory      int           `json:"memory,omitempty"`  // MiB.
	Balloon     int           `json:"balloon,omitempty"` // MiB; 0 disables ballooning.
	CPU         string        `json:"cpu,omitempty"`     // "host", "kvm64", …
	Boot        string        `json:"boot,omitempty"`    // e.g. "order=scsi0;net0".
	OSType      string        `json:"ostype,omitempty"`  // "l26", "win11", …
	SCSI0       string        `json:"scsi0,omitempty"`
	SCSI1       string        `json:"scsi1,omitempty"`
	Net0        string        `json:"net0,omitempty"`
	Net1        string        `json:"net1,omitempty"`
	Agent       string        `json:"agent,omitempty"` // e.g. "enabled=1".
	OnBoot      types.PVEBool `json:"onboot,omitempty"`
	Template    types.PVEBool `json:"template,omitempty"`
	Protection  types.PVEBool `json:"protection,omitempty"`
	// Extra holds config keys the SDK does not model, as their raw PVE string
	// values (e.g. "virtio0": "local-lvm:vm-100-disk-0,size=32G"). It is
	// populated on reads and ignored on writes — use ConfigUpdate.Extra to set
	// unmodelled keys.
	Extra map[string]string `json:"-"`
}

// UnmarshalJSON decodes the typed fields and routes every other key into Extra.
func (c *Config) UnmarshalJSON(data []byte) error {
	// A distinct type strips Config's methods, so the default decoder runs for
	// the typed fields without recursing back into this method.
	type alias Config
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("decode config: %w", err)
	}
	*c = Config(a)

	var all map[string]json.RawMessage
	if err := json.Unmarshal(data, &all); err != nil {
		return fmt.Errorf("decode config map: %w", err)
	}
	for key, raw := range all {
		if configKnownFields[key] {
			continue
		}
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			s = string(raw) // non-string field (number/bool): keep the raw token.
		}
		if c.Extra == nil {
			c.Extra = make(map[string]string)
		}
		c.Extra[key] = s
	}
	return nil
}

// CreateSpec is the body of POST /nodes/{node}/qemu. VMID is required; all other
// fields fall back to PVE defaults when zero. Pass it to Service.Create by
// pointer (the struct is large).
type CreateSpec struct {
	VMID    types.VMID     `json:"vmid"` // required.
	Name    string         `json:"name,omitempty"`
	Memory  int            `json:"memory,omitempty"` // MiB.
	Cores   int            `json:"cores,omitempty"`
	Sockets int            `json:"sockets,omitempty"`
	CPU     string         `json:"cpu,omitempty"`
	OSType  string         `json:"ostype,omitempty"`
	SCSI0   string         `json:"scsi0,omitempty"`
	Net0    string         `json:"net0,omitempty"`
	Boot    string         `json:"boot,omitempty"`
	Storage string         `json:"storage,omitempty"` // target storage pool.
	Start   *types.PVEBool `json:"start,omitempty"`
	// Extra carries PVE parameters the SDK does not model; its keys are merged
	// into the request form verbatim.
	Extra map[string]string `json:"-"`
}

// CloneSpec is the body of POST /nodes/{node}/qemu/{vmid}/clone. NewID is
// required. Full=nil takes the PVE default (a linked clone for templates, a full
// clone otherwise). Pass it to Service.Clone by pointer.
type CloneSpec struct {
	NewID       types.VMID     `json:"newid"` // required.
	Name        string         `json:"name,omitempty"`
	Description string         `json:"description,omitempty"`
	Full        *types.PVEBool `json:"full,omitempty"`
	Storage     string         `json:"storage,omitempty"`
	Format      string         `json:"format,omitempty"` // "raw", "qcow2", "vmdk".
	Pool        string         `json:"pool,omitempty"`
	SnapName    string         `json:"snapname,omitempty"`
	Target      string         `json:"target,omitempty"` // target node for the clone.
	// Extra carries PVE parameters the SDK does not model.
	Extra map[string]string `json:"-"`
}

// ConfigUpdate is the body of PUT /nodes/{node}/qemu/{vmid}/config. Only the set
// fields are sent. Optional booleans are pointers so that an unset field is
// omitted rather than sent as 0. Pass it to Service.SetConfig by pointer.
type ConfigUpdate struct {
	Name        string         `json:"name,omitempty"`
	Description string         `json:"description,omitempty"`
	Cores       int            `json:"cores,omitempty"`
	Sockets     int            `json:"sockets,omitempty"`
	Memory      int            `json:"memory,omitempty"`  // MiB.
	Balloon     int            `json:"balloon,omitempty"` // MiB.
	Boot        string         `json:"boot,omitempty"`
	OnBoot      *types.PVEBool `json:"onboot,omitempty"`
	Protection  *types.PVEBool `json:"protection,omitempty"`
	Delete      string         `json:"delete,omitempty"` // comma-separated keys to unset.
	// Extra carries PVE parameters the SDK does not model.
	Extra map[string]string `json:"-"`
}
