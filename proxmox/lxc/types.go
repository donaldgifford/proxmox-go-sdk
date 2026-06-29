package lxc

import (
	"encoding/json"
	"fmt"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/types"
)

// Container is one entry from GET /nodes/{node}/lxc — the per-node container
// summary list.
type Container struct {
	VMID     types.VMID       `json:"vmid"`
	Name     string           `json:"name,omitempty"` // the container hostname.
	Status   types.PowerState `json:"status"`
	CPUs     int              `json:"cpus,omitempty"`
	MaxMem   int64            `json:"maxmem,omitempty"`  // bytes.
	MaxDisk  int64            `json:"maxdisk,omitempty"` // bytes.
	Uptime   int64            `json:"uptime,omitempty"`  // seconds.
	Tags     string           `json:"tags,omitempty"`
	Template types.PVEBool    `json:"template,omitempty"`
}

// ContainerStatus is the runtime status from
// GET /nodes/{node}/lxc/{vmid}/status/current.
type ContainerStatus struct {
	VMID    types.VMID       `json:"vmid"`
	Name    string           `json:"name,omitempty"`
	Status  types.PowerState `json:"status"`
	Uptime  int64            `json:"uptime,omitempty"` // seconds.
	CPUs    int              `json:"cpus,omitempty"`
	CPU     float64          `json:"cpu,omitempty"` // utilisation ratio 0.0–1.0.
	Mem     int64            `json:"mem,omitempty"` // bytes in use.
	MaxMem  int64            `json:"maxmem,omitempty"`
	Swap    int64            `json:"swap,omitempty"`
	MaxSwap int64            `json:"maxswap,omitempty"`
	Disk    int64            `json:"disk,omitempty"`
	MaxDisk int64            `json:"maxdisk,omitempty"`
}

// configKnownFields lists the JSON keys that map to typed Config fields. Keys
// outside this set are collected into Config.Extra so reads are lossless. Add an
// entry here whenever a field is added to Config.
var configKnownFields = map[string]bool{
	"hostname": true, "description": true, "cores": true, "memory": true,
	"swap": true, "arch": true, "ostype": true, "rootfs": true,
	"net0": true, "net1": true, "nameserver": true, "searchdomain": true,
	"features": true, "onboot": true, "unprivileged": true,
	"template": true, "protection": true,
}

// Config is the container configuration from GET /nodes/{node}/lxc/{vmid}/config.
// The core subset is modelled as typed fields; every other key (mount points
// mp0…, lxc.* raw entries, …) is preserved in Extra so a read never drops data.
//
// Config is a read type: it has a custom UnmarshalJSON and must not be marshalled
// back to PVE. Use ConfigUpdate to write configuration.
type Config struct {
	Hostname     string        `json:"hostname,omitempty"`
	Description  string        `json:"description,omitempty"`
	Cores        int           `json:"cores,omitempty"`
	Memory       int           `json:"memory,omitempty"` // MiB.
	Swap         int           `json:"swap,omitempty"`   // MiB.
	Arch         string        `json:"arch,omitempty"`   // "amd64", "arm64", …
	OSType       string        `json:"ostype,omitempty"` // "debian", "alpine", …
	RootFS       string        `json:"rootfs,omitempty"`
	Net0         string        `json:"net0,omitempty"`
	Net1         string        `json:"net1,omitempty"`
	Nameserver   string        `json:"nameserver,omitempty"`
	SearchDomain string        `json:"searchdomain,omitempty"`
	Features     string        `json:"features,omitempty"` // e.g. "nesting=1".
	OnBoot       types.PVEBool `json:"onboot,omitempty"`
	Unprivileged types.PVEBool `json:"unprivileged,omitempty"`
	Template     types.PVEBool `json:"template,omitempty"`
	Protection   types.PVEBool `json:"protection,omitempty"`
	// Extra holds config keys the SDK does not model (mount points, raw lxc.*
	// entries, …) as their raw PVE string values. It is populated on reads and
	// ignored on writes — use ConfigUpdate.Extra to set unmodelled keys.
	Extra map[string]string `json:"-"`
}

// UnmarshalJSON decodes the typed fields and routes every other key into Extra.
func (c *Config) UnmarshalJSON(data []byte) error {
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

// CreateSpec is the body of POST /nodes/{node}/lxc. VMID and OSTemplate are
// required. Pass it to Create by pointer.
type CreateSpec struct {
	VMID          types.VMID     `json:"vmid"`       // required.
	OSTemplate    string         `json:"ostemplate"` // required, e.g. "local:vztmpl/debian-12-...tar.zst".
	Hostname      string         `json:"hostname,omitempty"`
	Storage       string         `json:"storage,omitempty"`
	RootFS        string         `json:"rootfs,omitempty"`
	Cores         int            `json:"cores,omitempty"`
	Memory        int            `json:"memory,omitempty"` // MiB.
	Swap          int            `json:"swap,omitempty"`   // MiB.
	Net0          string         `json:"net0,omitempty"`
	Password      string         `json:"password,omitempty"`
	SSHPublicKeys string         `json:"ssh-public-keys,omitempty"`
	OSType        string         `json:"ostype,omitempty"`
	Pool          string         `json:"pool,omitempty"`
	Unprivileged  *types.PVEBool `json:"unprivileged,omitempty"`
	Start         *types.PVEBool `json:"start,omitempty"`
	// Extra carries PVE parameters the SDK does not model (mount points, …).
	Extra map[string]string `json:"-"`
}

// CloneSpec is the body of POST /nodes/{node}/lxc/{vmid}/clone. NewID is
// required. Full=nil takes the PVE default. Pass it to Clone by pointer.
type CloneSpec struct {
	NewID       types.VMID     `json:"newid"` // required.
	Hostname    string         `json:"hostname,omitempty"`
	Description string         `json:"description,omitempty"`
	Full        *types.PVEBool `json:"full,omitempty"`
	Storage     string         `json:"storage,omitempty"`
	Pool        string         `json:"pool,omitempty"`
	SnapName    string         `json:"snapname,omitempty"`
	Target      string         `json:"target,omitempty"` // target node for the clone.
	// Extra carries PVE parameters the SDK does not model.
	Extra map[string]string `json:"-"`
}

// ConfigUpdate is the body of PUT /nodes/{node}/lxc/{vmid}/config. Only the set
// fields are sent. Optional booleans are pointers so an unset field is omitted
// rather than sent as 0. Pass it to SetConfig by pointer.
type ConfigUpdate struct {
	Hostname    string         `json:"hostname,omitempty"`
	Description string         `json:"description,omitempty"`
	Cores       int            `json:"cores,omitempty"`
	Memory      int            `json:"memory,omitempty"` // MiB.
	Swap        int            `json:"swap,omitempty"`   // MiB.
	Net0        string         `json:"net0,omitempty"`
	Nameserver  string         `json:"nameserver,omitempty"`
	OnBoot      *types.PVEBool `json:"onboot,omitempty"`
	Protection  *types.PVEBool `json:"protection,omitempty"`
	Delete      string         `json:"delete,omitempty"` // comma-separated keys to unset.
	// Extra carries PVE parameters the SDK does not model.
	Extra map[string]string `json:"-"`
}
