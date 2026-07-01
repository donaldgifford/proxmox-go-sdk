package sdn

import (
	"encoding/json"
	"fmt"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/internal/svcutil"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/types"
)

// ZoneType is the technology backing an SDN zone.
type ZoneType string

// The SDN zone types PVE supports.
const (
	ZoneTypeSimple ZoneType = "simple"
	ZoneTypeVLAN   ZoneType = "vlan"
	ZoneTypeQinQ   ZoneType = "qinq"
	ZoneTypeVXLAN  ZoneType = "vxlan"
	ZoneTypeEVPN   ZoneType = "evpn"
)

// Zone is one entry from GET /cluster/sdn/zones or /cluster/sdn/zones/{zone}.
// Reads are lossless: zone configs are type-dependent (EVPN carries VRF and
// controller fields, VXLAN carries peers, VLAN carries a bridge), so unmodelled
// keys are preserved in Extra.
type Zone struct {
	Zone       string   `json:"zone"`
	Type       ZoneType `json:"type,omitempty"`
	MTU        int      `json:"mtu,omitempty"`
	Nodes      string   `json:"nodes,omitempty"` // CSV of node names.
	IPAM       string   `json:"ipam,omitempty"`
	DNS        string   `json:"dns,omitempty"`
	ReverseDNS string   `json:"reversedns,omitempty"`
	DNSZone    string   `json:"dnszone,omitempty"`
	Controller string   `json:"controller,omitempty"` // EVPN.
	VRFVXLan   int      `json:"vrf-vxlan,omitempty"`  // EVPN.
	Peers      string   `json:"peers,omitempty"`      // VXLAN: CSV of peer IPs.
	Bridge     string   `json:"bridge,omitempty"`     // VLAN/QinQ.
	// Extra carries zone keys the SDK does not model.
	Extra map[string]string `json:"-"`
}

var zoneKnownFields = map[string]bool{
	"zone": true, "type": true, "mtu": true, "nodes": true, "ipam": true,
	"dns": true, "reversedns": true, "dnszone": true, "controller": true,
	"vrf-vxlan": true, "peers": true, "bridge": true,
}

// UnmarshalJSON decodes the modelled fields and routes unknown keys into Extra.
func (z *Zone) UnmarshalJSON(data []byte) error {
	type alias Zone
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("decode sdn zone: %w", err)
	}
	*z = Zone(a)
	extra, err := svcutil.DecodeExtra(data, zoneKnownFields)
	if err != nil {
		return fmt.Errorf("decode sdn zone: %w", err)
	}
	z.Extra = extra
	return nil
}

// ZoneSpec is the body of POST /cluster/sdn/zones. Zone and Type are required.
// Zone changes are staged; call ApplySDN to activate them. Pass it by pointer.
type ZoneSpec struct {
	Zone       string   `json:"zone"`
	Type       ZoneType `json:"type"`
	MTU        int      `json:"mtu,omitempty"`
	Nodes      string   `json:"nodes,omitempty"`
	IPAM       string   `json:"ipam,omitempty"`
	Controller string   `json:"controller,omitempty"`
	VRFVXLan   int      `json:"vrf-vxlan,omitempty"`
	Peers      string   `json:"peers,omitempty"`
	Bridge     string   `json:"bridge,omitempty"`
	// Extra carries PVE parameters the SDK does not model.
	Extra map[string]string `json:"-"`
}

// ZoneUpdate is the body of PUT /cluster/sdn/zones/{zone}. Use Delete to unset
// keys. Pass it by pointer.
type ZoneUpdate struct {
	MTU    int    `json:"mtu,omitempty"`
	Nodes  string `json:"nodes,omitempty"`
	IPAM   string `json:"ipam,omitempty"`
	Delete string `json:"delete,omitempty"`
	// Extra carries PVE parameters the SDK does not model.
	Extra map[string]string `json:"-"`
}

// VNet is one entry from GET /cluster/sdn/vnets or /cluster/sdn/vnets/{vnet}.
// Reads are lossless.
type VNet struct {
	VNet  string `json:"vnet"`
	Zone  string `json:"zone,omitempty"`
	Tag   int    `json:"tag,omitempty"` // VLAN/VXLAN tag.
	Alias string `json:"alias,omitempty"`
	// Extra carries vnet keys the SDK does not model.
	Extra map[string]string `json:"-"`
}

var vnetKnownFields = map[string]bool{
	"vnet": true, "zone": true, "tag": true, "alias": true,
}

// UnmarshalJSON decodes the modelled fields and routes unknown keys into Extra.
func (v *VNet) UnmarshalJSON(data []byte) error {
	type alias VNet
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("decode sdn vnet: %w", err)
	}
	*v = VNet(a)
	extra, err := svcutil.DecodeExtra(data, vnetKnownFields)
	if err != nil {
		return fmt.Errorf("decode sdn vnet: %w", err)
	}
	v.Extra = extra
	return nil
}

// VNetSpec is the body of POST /cluster/sdn/vnets. VNet and Zone are required.
// Pass it by pointer.
type VNetSpec struct {
	VNet  string `json:"vnet"`
	Zone  string `json:"zone"`
	Tag   int    `json:"tag,omitempty"`
	Alias string `json:"alias,omitempty"`
	// Extra carries PVE parameters the SDK does not model.
	Extra map[string]string `json:"-"`
}

// VNetUpdate is the body of PUT /cluster/sdn/vnets/{vnet}. Pass it by pointer.
type VNetUpdate struct {
	Tag    int    `json:"tag,omitempty"`
	Alias  string `json:"alias,omitempty"`
	Delete string `json:"delete,omitempty"`
	// Extra carries PVE parameters the SDK does not model.
	Extra map[string]string `json:"-"`
}

// Subnet is one entry from GET /cluster/sdn/vnets/{vnet}/subnets. Reads are
// lossless.
type Subnet struct {
	Subnet  string        `json:"subnet"` // CIDR, e.g. "10.0.0.0/24".
	VNet    string        `json:"vnet,omitempty"`
	Gateway string        `json:"gateway,omitempty"`
	SNAT    types.PVEBool `json:"snat,omitempty"`
	// Extra carries subnet keys the SDK does not model.
	Extra map[string]string `json:"-"`
}

var subnetKnownFields = map[string]bool{
	"subnet": true, "vnet": true, "gateway": true, "snat": true,
}

// UnmarshalJSON decodes the modelled fields and routes unknown keys into Extra.
func (s *Subnet) UnmarshalJSON(data []byte) error {
	type alias Subnet
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("decode sdn subnet: %w", err)
	}
	*s = Subnet(a)
	extra, err := svcutil.DecodeExtra(data, subnetKnownFields)
	if err != nil {
		return fmt.Errorf("decode sdn subnet: %w", err)
	}
	s.Extra = extra
	return nil
}

// SubnetSpec is the body of POST /cluster/sdn/vnets/{vnet}/subnets. Subnet (a
// CIDR) is required. Pass it by pointer.
type SubnetSpec struct {
	Subnet  string        `json:"subnet"`
	Gateway string        `json:"gateway,omitempty"`
	SNAT    types.PVEBool `json:"snat,omitempty"`
	// Extra carries PVE parameters the SDK does not model.
	Extra map[string]string `json:"-"`
}

// SubnetUpdate is the body of PUT /cluster/sdn/vnets/{vnet}/subnets/{subnet}.
// Pass it by pointer.
type SubnetUpdate struct {
	Gateway string        `json:"gateway,omitempty"`
	SNAT    types.PVEBool `json:"snat,omitempty"`
	Delete  string        `json:"delete,omitempty"`
	// Extra carries PVE parameters the SDK does not model.
	Extra map[string]string `json:"-"`
}
