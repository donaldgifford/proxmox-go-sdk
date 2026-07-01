package nodes

import (
	"encoding/json"
	"fmt"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/types"
)

// InterfaceType is a PVE network interface type.
type InterfaceType string

// The interface types PVE recognises for /nodes/{node}/network entries.
const (
	InterfaceTypeBridge InterfaceType = "bridge"
	InterfaceTypeBond   InterfaceType = "bond"
	InterfaceTypeVLAN   InterfaceType = "vlan"
	InterfaceTypeEth    InterfaceType = "eth"
)

// Interface is one entry from GET /nodes/{node}/network or
// GET /nodes/{node}/network/{iface}. Reads are lossless: keys outside the typed
// set are preserved in Extra (interface configs are type-dependent).
type Interface struct {
	Iface       string        `json:"iface"`
	Type        InterfaceType `json:"type,omitempty"`
	Address     string        `json:"address,omitempty"`  // IPv4 CIDR.
	Gateway     string        `json:"gateway,omitempty"`  //
	Address6    string        `json:"address6,omitempty"` // IPv6 CIDR.
	Gateway6    string        `json:"gateway6,omitempty"`
	BridgePorts string        `json:"bridge_ports,omitempty"`      // space-separated.
	BridgeSTP   types.PVEBool `json:"bridge_stp,omitempty"`        //
	BridgeFD    int           `json:"bridge_fd,omitempty"`         // forwarding delay.
	VLANAware   types.PVEBool `json:"bridge_vlan_aware,omitempty"` //
	BondSlaves  string        `json:"slaves,omitempty"`            // space-separated.
	BondMode    string        `json:"bond_mode,omitempty"`
	VLANRawDev  string        `json:"vlan-raw-device,omitempty"`
	VLANID      int           `json:"vlan-id,omitempty"`
	Comments    string        `json:"comments,omitempty"`
	Autostart   types.PVEBool `json:"autostart,omitempty"`
	Active      types.PVEBool `json:"active,omitempty"` // read-only runtime state.
	// Extra carries interface keys the SDK does not model.
	Extra map[string]string `json:"-"`
}

// interfaceKnownFields lists the JSON keys Interface models directly; keep it in
// sync with the struct so UnmarshalJSON routes only the rest into Extra.
var interfaceKnownFields = map[string]bool{
	"iface": true, "type": true, "address": true, "gateway": true,
	"address6": true, "gateway6": true, "bridge_ports": true,
	"bridge_stp": true, "bridge_fd": true, "bridge_vlan_aware": true,
	"slaves": true, "bond_mode": true, "vlan-raw-device": true,
	"vlan-id": true, "comments": true, "autostart": true, "active": true,
}

// UnmarshalJSON decodes the modelled fields and routes any unknown keys into
// Extra so an interface read round-trips losslessly.
func (i *Interface) UnmarshalJSON(data []byte) error {
	type alias Interface
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("decode interface: %w", err)
	}
	*i = Interface(a)

	var all map[string]json.RawMessage
	if err := json.Unmarshal(data, &all); err != nil {
		return fmt.Errorf("decode interface map: %w", err)
	}
	for key, raw := range all {
		if interfaceKnownFields[key] {
			continue
		}
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			s = string(raw)
		}
		if i.Extra == nil {
			i.Extra = make(map[string]string)
		}
		i.Extra[key] = s
	}
	return nil
}

// InterfaceSpec is the body of POST /nodes/{node}/network. Iface and Type are
// required. Pass it by pointer. New interfaces are staged into the pending
// network config; call ApplyNetworkConfig to activate them.
type InterfaceSpec struct {
	Iface       string        `json:"iface"`
	Type        InterfaceType `json:"type"`
	Address     string        `json:"address,omitempty"`
	Gateway     string        `json:"gateway,omitempty"`
	Address6    string        `json:"address6,omitempty"`
	Gateway6    string        `json:"gateway6,omitempty"`
	BridgePorts string        `json:"bridge_ports,omitempty"`
	BridgeSTP   types.PVEBool `json:"bridge_stp,omitempty"`
	BridgeFD    int           `json:"bridge_fd,omitempty"`
	VLANAware   types.PVEBool `json:"bridge_vlan_aware,omitempty"`
	BondSlaves  string        `json:"slaves,omitempty"`
	BondMode    string        `json:"bond_mode,omitempty"`
	VLANRawDev  string        `json:"vlan-raw-device,omitempty"`
	VLANID      int           `json:"vlan-id,omitempty"`
	Comments    string        `json:"comments,omitempty"`
	Autostart   types.PVEBool `json:"autostart,omitempty"`
	// Extra carries PVE parameters the SDK does not model.
	Extra map[string]string `json:"-"`
}

// InterfaceUpdate is the body of PUT /nodes/{node}/network/{iface}. All fields
// optional; use Delete to unset keys (a comma-separated list of PVE field
// names). Pass it by pointer.
type InterfaceUpdate struct {
	Address     string        `json:"address,omitempty"`
	Gateway     string        `json:"gateway,omitempty"`
	Address6    string        `json:"address6,omitempty"`
	Gateway6    string        `json:"gateway6,omitempty"`
	BridgePorts string        `json:"bridge_ports,omitempty"`
	BridgeSTP   types.PVEBool `json:"bridge_stp,omitempty"`
	BridgeFD    int           `json:"bridge_fd,omitempty"`
	VLANAware   types.PVEBool `json:"bridge_vlan_aware,omitempty"`
	BondSlaves  string        `json:"slaves,omitempty"`
	BondMode    string        `json:"bond_mode,omitempty"`
	Comments    string        `json:"comments,omitempty"`
	Autostart   types.PVEBool `json:"autostart,omitempty"`
	Delete      string        `json:"delete,omitempty"`
	// Extra carries PVE parameters the SDK does not model.
	Extra map[string]string `json:"-"`
}
