package sdn

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/internal/svcutil"
)

// SDN live status is node-scoped: the runtime state lives under
// /nodes/{node}/sdn (verified against the real 9.2 apidoc, INV-0004), unlike
// the cluster-scoped config surface. Every read here is a plain GET returning
// typed rows; all are lossless (unknown keys land in Extra, with non-string
// values — including the array-valued fields called out per type — preserved
// as raw JSON tokens).
//
// The per-object roots (…/zones/{zone}, …/vnets/{vnet}, …/fabrics/{fabric})
// are subdir indexes on real PVE, not data endpoints — hence there is no
// per-VNet status read: ZoneContent covers VNet health and VNetMACVRF covers
// the EVPN view.

// SDNZoneStatus is one row of GET /nodes/{node}/sdn/zones — a zone's runtime
// health on that node. Status is one of "available", "pending", or "error".
type SDNZoneStatus struct {
	Zone   string `json:"zone"`
	Status string `json:"status"`
	// Extra carries status keys the SDK does not model.
	Extra map[string]string `json:"-"`
}

var sdnZoneStatusKnownFields = map[string]bool{"zone": true, "status": true}

// UnmarshalJSON decodes the modelled fields and routes unknown keys into Extra.
func (z *SDNZoneStatus) UnmarshalJSON(data []byte) error {
	type alias SDNZoneStatus
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("decode sdn zone status: %w", err)
	}
	*z = SDNZoneStatus(a)
	extra, err := svcutil.DecodeExtra(data, sdnZoneStatusKnownFields)
	if err != nil {
		return fmt.Errorf("decode sdn zone status: %w", err)
	}
	z.Extra = extra
	return nil
}

// VNetStatus is one row of GET /nodes/{node}/sdn/zones/{zone}/content — a
// VNet's runtime status within its zone on that node. Status and StatusMsg are
// optional on the wire.
type VNetStatus struct {
	VNet      string `json:"vnet"`
	Status    string `json:"status,omitempty"`
	StatusMsg string `json:"statusmsg,omitempty"`
	// Extra carries status keys the SDK does not model.
	Extra map[string]string `json:"-"`
}

var vnetStatusKnownFields = map[string]bool{
	"vnet": true, "status": true, "statusmsg": true,
}

// UnmarshalJSON decodes the modelled fields and routes unknown keys into Extra.
func (v *VNetStatus) UnmarshalJSON(data []byte) error {
	type alias VNetStatus
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("decode sdn vnet status: %w", err)
	}
	*v = VNetStatus(a)
	extra, err := svcutil.DecodeExtra(data, vnetStatusKnownFields)
	if err != nil {
		return fmt.Errorf("decode sdn vnet status: %w", err)
	}
	v.Extra = extra
	return nil
}

// ZoneBridge is one row of GET /nodes/{node}/sdn/zones/{zone}/bridges — a
// bridge the zone materialises on that node. The `ports` member list (an array
// of per-port objects) is unmodelled and lands in Extra as its raw JSON token.
type ZoneBridge struct {
	Name          string `json:"name"`
	VLANFiltering string `json:"vlan_filtering,omitempty"`
	// Extra carries bridge keys the SDK does not model (including `ports`).
	Extra map[string]string `json:"-"`
}

var zoneBridgeKnownFields = map[string]bool{"name": true, "vlan_filtering": true}

// UnmarshalJSON decodes the modelled fields and routes unknown keys into Extra.
func (b *ZoneBridge) UnmarshalJSON(data []byte) error {
	type alias ZoneBridge
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("decode sdn zone bridge: %w", err)
	}
	*b = ZoneBridge(a)
	extra, err := svcutil.DecodeExtra(data, zoneBridgeKnownFields)
	if err != nil {
		return fmt.Errorf("decode sdn zone bridge: %w", err)
	}
	b.Extra = extra
	return nil
}

// IPVRFRoute is one row of GET /nodes/{node}/sdn/zones/{zone}/ip-vrf — an
// entry in the zone's VRF route table on that node (guest /32 routes excluded;
// they live on the vnet bridge directly). The `nexthops` string array is
// unmodelled and lands in Extra as its raw JSON token.
type IPVRFRoute struct {
	IP       string `json:"ip"`
	Protocol string `json:"protocol,omitempty"`
	Metric   int    `json:"metric,omitempty"`
	// Extra carries route keys the SDK does not model (including `nexthops`).
	Extra map[string]string `json:"-"`
}

var ipVRFRouteKnownFields = map[string]bool{
	"ip": true, "protocol": true, "metric": true,
}

// UnmarshalJSON decodes the modelled fields and routes unknown keys into Extra.
func (r *IPVRFRoute) UnmarshalJSON(data []byte) error {
	type alias IPVRFRoute
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("decode sdn ip-vrf route: %w", err)
	}
	*r = IPVRFRoute(a)
	extra, err := svcutil.DecodeExtra(data, ipVRFRouteKnownFields)
	if err != nil {
		return fmt.Errorf("decode sdn ip-vrf route: %w", err)
	}
	r.Extra = extra
	return nil
}

// MACVRFEntry is one row of GET /nodes/{node}/sdn/vnets/{vnet}/mac-vrf — a
// MAC-VRF route the node self-originates or learned via BGP (EVPN zones).
type MACVRFEntry struct {
	IP      string `json:"ip"`
	MAC     string `json:"mac"`
	Nexthop string `json:"nexthop,omitempty"`
	// Extra carries entry keys the SDK does not model.
	Extra map[string]string `json:"-"`
}

var macVRFEntryKnownFields = map[string]bool{
	"ip": true, "mac": true, "nexthop": true,
}

// UnmarshalJSON decodes the modelled fields and routes unknown keys into Extra.
func (e *MACVRFEntry) UnmarshalJSON(data []byte) error {
	type alias MACVRFEntry
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("decode sdn mac-vrf entry: %w", err)
	}
	*e = MACVRFEntry(a)
	extra, err := svcutil.DecodeExtra(data, macVRFEntryKnownFields)
	if err != nil {
		return fmt.Errorf("decode sdn mac-vrf entry: %w", err)
	}
	e.Extra = extra
	return nil
}

// FabricInterface is one row of GET /nodes/{node}/sdn/fabrics/{fabric}/
// interfaces — a network interface participating in the fabric on that node,
// with its FRR state and role (e.g. Point-to-Point, Broadcast).
type FabricInterface struct {
	Name  string `json:"name"`
	State string `json:"state,omitempty"`
	Type  string `json:"type,omitempty"`
	// Extra carries interface keys the SDK does not model.
	Extra map[string]string `json:"-"`
}

var fabricInterfaceKnownFields = map[string]bool{
	"name": true, "state": true, "type": true,
}

// UnmarshalJSON decodes the modelled fields and routes unknown keys into Extra.
func (i *FabricInterface) UnmarshalJSON(data []byte) error {
	type alias FabricInterface
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("decode sdn fabric interface: %w", err)
	}
	*i = FabricInterface(a)
	extra, err := svcutil.DecodeExtra(data, fabricInterfaceKnownFields)
	if err != nil {
		return fmt.Errorf("decode sdn fabric interface: %w", err)
	}
	i.Extra = extra
	return nil
}

// FabricNeighbor is one row of GET /nodes/{node}/sdn/fabrics/{fabric}/
// neighbors — a routing neighbor as reported by FRR. Uptime is FRR's rendering
// (e.g. "8h24m12s"), passed through verbatim.
type FabricNeighbor struct {
	Neighbor string `json:"neighbor"`
	Status   string `json:"status,omitempty"`
	Uptime   string `json:"uptime,omitempty"`
	// Extra carries neighbor keys the SDK does not model.
	Extra map[string]string `json:"-"`
}

var fabricNeighborKnownFields = map[string]bool{
	"neighbor": true, "status": true, "uptime": true,
}

// UnmarshalJSON decodes the modelled fields and routes unknown keys into Extra.
func (n *FabricNeighbor) UnmarshalJSON(data []byte) error {
	type alias FabricNeighbor
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("decode sdn fabric neighbor: %w", err)
	}
	*n = FabricNeighbor(a)
	extra, err := svcutil.DecodeExtra(data, fabricNeighborKnownFields)
	if err != nil {
		return fmt.Errorf("decode sdn fabric neighbor: %w", err)
	}
	n.Extra = extra
	return nil
}

// FabricRoute is one row of GET /nodes/{node}/sdn/fabrics/{fabric}/routes — a
// route table entry (CIDR) the fabric learned. The `via` nexthop string array
// is unmodelled and lands in Extra as its raw JSON token.
type FabricRoute struct {
	Route string `json:"route"`
	// Extra carries route keys the SDK does not model (including `via`).
	Extra map[string]string `json:"-"`
}

var fabricRouteKnownFields = map[string]bool{"route": true}

// UnmarshalJSON decodes the modelled fields and routes unknown keys into Extra.
func (r *FabricRoute) UnmarshalJSON(data []byte) error {
	type alias FabricRoute
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("decode sdn fabric route: %w", err)
	}
	*r = FabricRoute(a)
	extra, err := svcutil.DecodeExtra(data, fabricRouteKnownFields)
	if err != nil {
		return fmt.Errorf("decode sdn fabric route: %w", err)
	}
	r.Extra = extra
	return nil
}

// SDNStatus returns the runtime status of every SDN zone on one node.
func (s *Service) SDNStatus(ctx context.Context, node string) ([]SDNZoneStatus, error) {
	if node == "" {
		return nil, fmt.Errorf("sdn.SDNStatus: node: %w", svcutil.ErrMissingField)
	}
	var zones []SDNZoneStatus
	if err := s.c.DoRequest(ctx, http.MethodGet, nodeSDNZonesPath(node), nil, &zones); err != nil {
		return nil, fmt.Errorf("sdn.SDNStatus: %w", err)
	}
	return zones, nil
}

// ZoneContent returns the runtime status of a zone's VNets on one node. This
// is the per-VNet health read — there is no per-VNet status endpoint (the
// vnets/{vnet} path is a subdir index on real PVE).
func (s *Service) ZoneContent(ctx context.Context, node, zone string) ([]VNetStatus, error) {
	if node == "" || zone == "" {
		return nil, fmt.Errorf("sdn.ZoneContent: node/zone: %w", svcutil.ErrMissingField)
	}
	var vnets []VNetStatus
	if err := s.c.DoRequest(ctx, http.MethodGet, nodeSDNZoneContentPath(node, zone), nil, &vnets); err != nil {
		return nil, fmt.Errorf("sdn.ZoneContent: %w", err)
	}
	return vnets, nil
}

// ZoneBridges returns the bridges a zone materialises on one node.
func (s *Service) ZoneBridges(ctx context.Context, node, zone string) ([]ZoneBridge, error) {
	if node == "" || zone == "" {
		return nil, fmt.Errorf("sdn.ZoneBridges: node/zone: %w", svcutil.ErrMissingField)
	}
	var bridges []ZoneBridge
	if err := s.c.DoRequest(ctx, http.MethodGet, nodeSDNZoneBridgesPath(node, zone), nil, &bridges); err != nil {
		return nil, fmt.Errorf("sdn.ZoneBridges: %w", err)
	}
	return bridges, nil
}

// ZoneIPVRF returns the zone's VRF route table on one node.
func (s *Service) ZoneIPVRF(ctx context.Context, node, zone string) ([]IPVRFRoute, error) {
	if node == "" || zone == "" {
		return nil, fmt.Errorf("sdn.ZoneIPVRF: node/zone: %w", svcutil.ErrMissingField)
	}
	var routes []IPVRFRoute
	if err := s.c.DoRequest(ctx, http.MethodGet, nodeSDNZoneIPVRFPath(node, zone), nil, &routes); err != nil {
		return nil, fmt.Errorf("sdn.ZoneIPVRF: %w", err)
	}
	return routes, nil
}

// VNetMACVRF returns a VNet's MAC-VRF entries on one node (EVPN zones).
func (s *Service) VNetMACVRF(ctx context.Context, node, vnet string) ([]MACVRFEntry, error) {
	if node == "" || vnet == "" {
		return nil, fmt.Errorf("sdn.VNetMACVRF: node/vnet: %w", svcutil.ErrMissingField)
	}
	var entries []MACVRFEntry
	if err := s.c.DoRequest(ctx, http.MethodGet, nodeSDNVNetMACVRFPath(node, vnet), nil, &entries); err != nil {
		return nil, fmt.Errorf("sdn.VNetMACVRF: %w", err)
	}
	return entries, nil
}

// FabricInterfaces returns a fabric's participating interfaces on one node.
func (s *Service) FabricInterfaces(ctx context.Context, node, fabric string) ([]FabricInterface, error) {
	if node == "" || fabric == "" {
		return nil, fmt.Errorf("sdn.FabricInterfaces: node/fabric: %w", svcutil.ErrMissingField)
	}
	var ifaces []FabricInterface
	if err := s.c.DoRequest(ctx, http.MethodGet, nodeSDNFabricInterfacesPath(node, fabric), nil, &ifaces); err != nil {
		return nil, fmt.Errorf("sdn.FabricInterfaces: %w", err)
	}
	return ifaces, nil
}

// FabricNeighbors returns a fabric's routing neighbors on one node, as
// reported by FRR.
func (s *Service) FabricNeighbors(ctx context.Context, node, fabric string) ([]FabricNeighbor, error) {
	if node == "" || fabric == "" {
		return nil, fmt.Errorf("sdn.FabricNeighbors: node/fabric: %w", svcutil.ErrMissingField)
	}
	var neighbors []FabricNeighbor
	if err := s.c.DoRequest(ctx, http.MethodGet, nodeSDNFabricNeighborsPath(node, fabric), nil, &neighbors); err != nil {
		return nil, fmt.Errorf("sdn.FabricNeighbors: %w", err)
	}
	return neighbors, nil
}

// FabricRoutes returns the routes a fabric learned on one node.
func (s *Service) FabricRoutes(ctx context.Context, node, fabric string) ([]FabricRoute, error) {
	if node == "" || fabric == "" {
		return nil, fmt.Errorf("sdn.FabricRoutes: node/fabric: %w", svcutil.ErrMissingField)
	}
	var routes []FabricRoute
	if err := s.c.DoRequest(ctx, http.MethodGet, nodeSDNFabricRoutesPath(node, fabric), nil, &routes); err != nil {
		return nil, fmt.Errorf("sdn.FabricRoutes: %w", err)
	}
	return routes, nil
}
