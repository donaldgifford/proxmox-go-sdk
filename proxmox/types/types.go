package types

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// VMID is a Proxmox guest identifier (a QEMU VM or LXC container). Proxmox VE
// accepts IDs in the range 100–999999999.
type VMID int

// String renders the VMID as its decimal digits.
func (v VMID) String() string { return strconv.Itoa(int(v)) }

// NodeName is a Proxmox cluster node identifier, e.g. "pve" or "pve-node1".
type NodeName string

// GuestRef unambiguously identifies a guest by the node it lives on plus its
// VMID. PVE guest IDs are cluster-unique, but most API paths are node-scoped,
// so callers usually need both.
type GuestRef struct {
	Node NodeName
	VMID VMID
}

// String renders the reference as "<node>/<vmid>".
func (g GuestRef) String() string { return fmt.Sprintf("%s/%s", g.Node, g.VMID) }

// PowerState is the run state a guest reports from its status endpoints.
type PowerState string

// The power states PVE reports for QEMU VMs and LXC containers.
const (
	PowerStateRunning   PowerState = "running"
	PowerStateStopped   PowerState = "stopped"
	PowerStatePaused    PowerState = "paused"
	PowerStateSuspended PowerState = "suspended"
	PowerStateUnknown   PowerState = "unknown"
)

// PVEBool normalises Proxmox's inconsistent boolean encoding. Across endpoints
// and minor versions PVE returns booleans as the integers 0/1, the strings
// "0"/"1" (and "yes"/"no", "true"/"false"), or bare JSON true/false. Use
// PVEBool for any boolean field in a response struct; it accepts all of those
// forms.
//
// In request bodies PVEBool marshals to the integer 1 or 0, which is what the
// PVE REST API expects. Because false marshals to 0 rather than being omitted,
// use *PVEBool when a request field needs `omitempty` semantics.
type PVEBool bool

// UnmarshalJSON decodes the 0/1, "0"/"1", "yes"/"no", and true/false forms PVE
// emits. A JSON null is treated as a no-op (the value is left unchanged), per
// the encoding/json convention for Unmarshalers.
func (b *PVEBool) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return nil
	}

	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("types: decode PVEBool: %w", err)
	}

	switch v := raw.(type) {
	case bool:
		*b = PVEBool(v)
	case float64:
		*b = PVEBool(v != 0)
	case string:
		switch v {
		case "1", "true", "yes":
			*b = true
		case "0", "false", "no", "":
			*b = false
		default:
			return fmt.Errorf("types: cannot unmarshal %q into PVEBool", v)
		}
	default:
		return fmt.Errorf("types: cannot unmarshal %T into PVEBool", raw)
	}
	return nil
}

// MarshalJSON encodes the value as the integer 1 (true) or 0 (false), matching
// what PVE expects in request bodies.
func (b PVEBool) MarshalJSON() ([]byte, error) {
	if b {
		return []byte("1"), nil
	}
	return []byte("0"), nil
}

// Bool returns the value as a plain bool, so callers can write
// cfg.Template.Bool() without a conversion.
func (b PVEBool) Bool() bool { return bool(b) }
