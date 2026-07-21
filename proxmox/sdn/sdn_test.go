package sdn_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/mockpve"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/pverr"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/sdn"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/version"
)

func newService(t *testing.T, mock *mockpve.Server) *sdn.Service {
	t.Helper()
	c, cleanup := mock.NewClient()
	t.Cleanup(cleanup)
	return sdn.NewService(c, version.Capabilities{})
}

// newCappedService builds a Service whose capability snapshot is pinned to ver
// (e.g. "9.1", "9.2"), for exercising the version-gated fabric operations.
func newCappedService(t *testing.T, mock *mockpve.Server, ver string) *sdn.Service {
	t.Helper()
	caps, err := version.Parse(ver)
	if err != nil {
		t.Fatalf("version.Parse(%q): %v", ver, err)
	}
	c, cleanup := mock.NewClient()
	t.Cleanup(cleanup)
	return sdn.NewService(c, caps)
}

func TestListZones(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddZone("localnetwork", "simple")
	mock.AddZone("vlanzone", "vlan")
	svc := newService(t, mock)

	zones, err := svc.ListZones(context.Background())
	if err != nil {
		t.Fatalf("ListZones: %v", err)
	}
	if len(zones) != 2 {
		t.Fatalf("ListZones returned %d, want 2", len(zones))
	}
}

func TestGetZone(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddZone("vlanzone", "vlan")
	svc := newService(t, mock)

	z, err := svc.GetZone(context.Background(), "vlanzone")
	if err != nil {
		t.Fatalf("GetZone: %v", err)
	}
	if z.Zone != "vlanzone" || z.Type != sdn.ZoneTypeVLAN {
		t.Errorf("zone = %+v, want zone=vlanzone type=vlan", z)
	}
}

func TestGetZoneNotFound(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)

	if _, err := svc.GetZone(context.Background(), "ghost"); !errors.Is(err, pverr.ErrNotFound) {
		t.Fatalf("GetZone(ghost) = %v, want ErrNotFound", err)
	}
}

func TestCreateZone(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)
	ctx := context.Background()

	err := svc.CreateZone(ctx, &sdn.ZoneSpec{
		Zone:  "evpnzone",
		Type:  sdn.ZoneTypeEVPN,
		MTU:   1450,
		Nodes: "pve1,pve2",
	})
	if err != nil {
		t.Fatalf("CreateZone: %v", err)
	}

	z, err := svc.GetZone(ctx, "evpnzone")
	if err != nil {
		t.Fatalf("GetZone after create: %v", err)
	}
	if z.MTU != 1450 || z.Nodes != "pve1,pve2" {
		t.Errorf("created zone = %+v, want mtu=1450 nodes=pve1,pve2", z)
	}
}

func TestCreateZoneValidation(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)
	ctx := context.Background()

	if err := svc.CreateZone(ctx, nil); err == nil {
		t.Error("CreateZone(nil) error = nil, want non-nil")
	}
	if err := svc.CreateZone(ctx, &sdn.ZoneSpec{Type: sdn.ZoneTypeSimple}); err == nil {
		t.Error("CreateZone(no zone) error = nil, want non-nil")
	}
	if err := svc.CreateZone(ctx, &sdn.ZoneSpec{Zone: "z"}); err == nil {
		t.Error("CreateZone(no type) error = nil, want non-nil")
	}
}

func TestUpdateZone(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddZone("vlanzone", "vlan")
	svc := newService(t, mock)
	ctx := context.Background()

	if err := svc.UpdateZone(ctx, "vlanzone", &sdn.ZoneUpdate{MTU: 9000}); err != nil {
		t.Fatalf("UpdateZone: %v", err)
	}
	z, err := svc.GetZone(ctx, "vlanzone")
	if err != nil {
		t.Fatalf("GetZone after update: %v", err)
	}
	if z.MTU != 9000 {
		t.Errorf("mtu after update = %d, want 9000", z.MTU)
	}
}

func TestDeleteZone(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddZone("gone", "simple")
	svc := newService(t, mock)
	ctx := context.Background()

	if err := svc.DeleteZone(ctx, "gone"); err != nil {
		t.Fatalf("DeleteZone: %v", err)
	}
	if _, err := svc.GetZone(ctx, "gone"); !errors.Is(err, pverr.ErrNotFound) {
		t.Fatalf("GetZone after delete = %v, want ErrNotFound", err)
	}
}

func TestListVNets(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddVNet("vnet0", "localnetwork")
	svc := newService(t, mock)

	vnets, err := svc.ListVNets(context.Background())
	if err != nil {
		t.Fatalf("ListVNets: %v", err)
	}
	if len(vnets) != 1 {
		t.Fatalf("ListVNets returned %d, want 1", len(vnets))
	}
}

func TestCreateVNet(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)
	ctx := context.Background()

	if err := svc.CreateVNet(ctx, &sdn.VNetSpec{VNet: "vnet1", Zone: "localnetwork", Tag: 100}); err != nil {
		t.Fatalf("CreateVNet: %v", err)
	}
	v, err := svc.GetVNet(ctx, "vnet1")
	if err != nil {
		t.Fatalf("GetVNet after create: %v", err)
	}
	if v.Zone != "localnetwork" || v.Tag != 100 {
		t.Errorf("created vnet = %+v, want zone=localnetwork tag=100", v)
	}
}

func TestCreateVNetValidation(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)
	ctx := context.Background()

	if err := svc.CreateVNet(ctx, nil); err == nil {
		t.Error("CreateVNet(nil) error = nil, want non-nil")
	}
	if err := svc.CreateVNet(ctx, &sdn.VNetSpec{Zone: "z"}); err == nil {
		t.Error("CreateVNet(no vnet) error = nil, want non-nil")
	}
	if err := svc.CreateVNet(ctx, &sdn.VNetSpec{VNet: "v"}); err == nil {
		t.Error("CreateVNet(no zone) error = nil, want non-nil")
	}
}

func TestUpdateVNet(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddVNet("vnet0", "localnetwork")
	svc := newService(t, mock)
	ctx := context.Background()

	if err := svc.UpdateVNet(ctx, "vnet0", &sdn.VNetUpdate{Alias: "front"}); err != nil {
		t.Fatalf("UpdateVNet: %v", err)
	}
	v, err := svc.GetVNet(ctx, "vnet0")
	if err != nil {
		t.Fatalf("GetVNet after update: %v", err)
	}
	if v.Alias != "front" {
		t.Errorf("alias after update = %q, want front", v.Alias)
	}
}

func TestDeleteVNet(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddVNet("vnetx", "localnetwork")
	svc := newService(t, mock)
	ctx := context.Background()

	if err := svc.DeleteVNet(ctx, "vnetx"); err != nil {
		t.Fatalf("DeleteVNet: %v", err)
	}
	if _, err := svc.GetVNet(ctx, "vnetx"); !errors.Is(err, pverr.ErrNotFound) {
		t.Fatalf("GetVNet after delete = %v, want ErrNotFound", err)
	}
}

func TestSubnetCRUD(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddVNet("vnet0", "localnetwork")
	svc := newService(t, mock)
	ctx := context.Background()

	const cidr = "10.0.0.0/24"
	if err := svc.CreateSubnet(ctx, "vnet0", &sdn.SubnetSpec{
		Subnet:  cidr,
		Gateway: "10.0.0.1",
		SNAT:    true,
	}); err != nil {
		t.Fatalf("CreateSubnet: %v", err)
	}

	subnets, err := svc.ListSubnets(ctx, "vnet0")
	if err != nil {
		t.Fatalf("ListSubnets: %v", err)
	}
	if len(subnets) != 1 {
		t.Fatalf("ListSubnets returned %d, want 1", len(subnets))
	}

	sn, err := svc.GetSubnet(ctx, "vnet0", cidr)
	if err != nil {
		t.Fatalf("GetSubnet: %v", err)
	}
	if sn.Gateway != "10.0.0.1" || !bool(sn.SNAT) {
		t.Errorf("subnet = %+v, want gateway=10.0.0.1 snat=true", sn)
	}

	if err := svc.UpdateSubnet(ctx, "vnet0", cidr, &sdn.SubnetUpdate{Gateway: "10.0.0.254"}); err != nil {
		t.Fatalf("UpdateSubnet: %v", err)
	}
	sn, err = svc.GetSubnet(ctx, "vnet0", cidr)
	if err != nil {
		t.Fatalf("GetSubnet after update: %v", err)
	}
	if sn.Gateway != "10.0.0.254" {
		t.Errorf("gateway after update = %q, want 10.0.0.254", sn.Gateway)
	}

	if err := svc.DeleteSubnet(ctx, "vnet0", cidr); err != nil {
		t.Fatalf("DeleteSubnet: %v", err)
	}
	if _, err := svc.GetSubnet(ctx, "vnet0", cidr); !errors.Is(err, pverr.ErrNotFound) {
		t.Fatalf("GetSubnet after delete = %v, want ErrNotFound", err)
	}
}

func TestApplySDN(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)

	if err := svc.ApplySDN(context.Background()); err != nil {
		t.Fatalf("ApplySDN: %v", err)
	}
}

func TestListFabrics(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddFabric("fab0", "openfabric")
	svc := newService(t, mock)

	fabrics, err := svc.ListFabrics(context.Background())
	if err != nil {
		t.Fatalf("ListFabrics: %v", err)
	}
	if len(fabrics) != 1 {
		t.Fatalf("ListFabrics returned %d, want 1", len(fabrics))
	}
}

func TestGetFabric(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddFabric("fab0", "ospf")
	svc := newService(t, mock)

	f, err := svc.GetFabric(context.Background(), "fab0")
	if err != nil {
		t.Fatalf("GetFabric: %v", err)
	}
	if f.Fabric != "fab0" || f.Protocol != sdn.FabricProtocolOSPF {
		t.Errorf("fabric = %+v, want id=fab0 protocol=ospf", f)
	}
}

func TestGetFabricNotFound(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)

	if _, err := svc.GetFabric(context.Background(), "ghost"); !errors.Is(err, pverr.ErrNotFound) {
		t.Fatalf("GetFabric(ghost) = %v, want ErrNotFound", err)
	}
}

func TestCreateFabric(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)
	ctx := context.Background()

	// OpenFabric is the 9.0 baseline: no version gate, so zero caps is fine.
	if err := svc.CreateFabric(ctx, &sdn.FabricSpec{
		Fabric:   "fab1",
		Protocol: sdn.FabricProtocolOpenFabric,
		IPPrefix: "10.0.0.0/24",
	}); err != nil {
		t.Fatalf("CreateFabric: %v", err)
	}
	f, err := svc.GetFabric(ctx, "fab1")
	if err != nil {
		t.Fatalf("GetFabric after create: %v", err)
	}
	if f.Protocol != sdn.FabricProtocolOpenFabric || f.IPPrefix != "10.0.0.0/24" {
		t.Errorf("created fabric = %+v, want protocol=openfabric ip_prefix=10.0.0.0/24", f)
	}
}

func TestCreateFabricValidation(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)
	ctx := context.Background()

	if err := svc.CreateFabric(ctx, nil); err == nil {
		t.Error("CreateFabric(nil) error = nil, want non-nil")
	}
	if err := svc.CreateFabric(ctx, &sdn.FabricSpec{Protocol: sdn.FabricProtocolOSPF}); err == nil {
		t.Error("CreateFabric(no id) error = nil, want non-nil")
	}
	if err := svc.CreateFabric(ctx, &sdn.FabricSpec{Fabric: "f"}); err == nil {
		t.Error("CreateFabric(no protocol) error = nil, want non-nil")
	}
}

// TestCreateFabricAdvancedProtocolGate covers the SDNAdvancedFabrics gate: BGP
// is a 9.2 protocol, so it must be refused below 9.2 and accepted at 9.2.
func TestCreateFabricAdvancedProtocolGate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	mock91 := mockpve.New()
	svc91 := newCappedService(t, mock91, "9.1") // below the 9.2 gate.
	err := svc91.CreateFabric(ctx, &sdn.FabricSpec{Fabric: "bgpfab", Protocol: sdn.FabricProtocolBGP})
	if !errors.Is(err, pverr.ErrUnsupported) {
		t.Fatalf("CreateFabric(bgp) on 9.1 = %v, want ErrUnsupported", err)
	}
	err = svc91.CreateFabric(ctx, &sdn.FabricSpec{Fabric: "wgfab", Protocol: sdn.FabricProtocolWireGuard})
	if !errors.Is(err, pverr.ErrUnsupported) {
		t.Fatalf("CreateFabric(wireguard) on 9.1 = %v, want ErrUnsupported", err)
	}

	mock92 := mockpve.New()
	svc92 := newCappedService(t, mock92, "9.2") // gate satisfied.
	if err := svc92.CreateFabric(ctx, &sdn.FabricSpec{Fabric: "bgpfab", Protocol: sdn.FabricProtocolBGP}); err != nil {
		t.Fatalf("CreateFabric(bgp) on 9.2 = %v, want nil", err)
	}
}

func TestUpdateFabric(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddFabric("fab0", "openfabric")
	svc := newService(t, mock)
	ctx := context.Background()

	if err := svc.UpdateFabric(ctx, "fab0", &sdn.FabricUpdate{RouteFilter: "10.0.0.0/8"}); err != nil {
		t.Fatalf("UpdateFabric: %v", err)
	}
	f, err := svc.GetFabric(ctx, "fab0")
	if err != nil {
		t.Fatalf("GetFabric after update: %v", err)
	}
	if f.RouteFilter != "10.0.0.0/8" {
		t.Errorf("route_filter after update = %q, want 10.0.0.0/8", f.RouteFilter)
	}
}

func TestDeleteFabric(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddFabric("gone", "ospf")
	svc := newService(t, mock)
	ctx := context.Background()

	if err := svc.DeleteFabric(ctx, "gone"); err != nil {
		t.Fatalf("DeleteFabric: %v", err)
	}
	if _, err := svc.GetFabric(ctx, "gone"); !errors.Is(err, pverr.ErrNotFound) {
		t.Fatalf("GetFabric after delete = %v, want ErrNotFound", err)
	}
}

func TestFabricNodeCRUD(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddFabric("fab0", "openfabric")
	svc := newService(t, mock)
	ctx := context.Background()

	if err := svc.CreateFabricNode(ctx, "fab0", &sdn.FabricNodeSpec{
		NodeID:     "pve1",
		Protocol:   sdn.FabricProtocolOpenFabric,
		IP:         "10.0.0.1",
		Interfaces: []string{"ens19", "ens20"},
	}); err != nil {
		t.Fatalf("CreateFabricNode: %v", err)
	}

	nodes, err := svc.ListFabricNodes(ctx, "fab0")
	if err != nil {
		t.Fatalf("ListFabricNodes: %v", err)
	}
	if len(nodes) != 1 || nodes[0].NodeID != "pve1" {
		t.Fatalf("ListFabricNodes = %+v, want one node pve1", nodes)
	}

	n, err := svc.GetFabricNode(ctx, "fab0", "pve1")
	if err != nil {
		t.Fatalf("GetFabricNode: %v", err)
	}
	if n.IP != "10.0.0.1" || n.Fabric != "fab0" {
		t.Errorf("fabric node = %+v, want ip=10.0.0.1 fabric_id=fab0", n)
	}

	if err := svc.UpdateFabricNode(ctx, "fab0", "pve1", &sdn.FabricNodeUpdate{IP: "10.0.0.2"}); err != nil {
		t.Fatalf("UpdateFabricNode: %v", err)
	}
	n, err = svc.GetFabricNode(ctx, "fab0", "pve1")
	if err != nil {
		t.Fatalf("GetFabricNode after update: %v", err)
	}
	if n.IP != "10.0.0.2" {
		t.Errorf("ip after update = %q, want 10.0.0.2", n.IP)
	}

	if err := svc.DeleteFabricNode(ctx, "fab0", "pve1"); err != nil {
		t.Fatalf("DeleteFabricNode: %v", err)
	}
	if _, err := svc.GetFabricNode(ctx, "fab0", "pve1"); !errors.Is(err, pverr.ErrNotFound) {
		t.Fatalf("GetFabricNode after delete = %v, want ErrNotFound", err)
	}
}

func TestFabricNodeValidation(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)
	ctx := context.Background()

	if err := svc.CreateFabricNode(ctx, "fab0", nil); err == nil {
		t.Error("CreateFabricNode(nil) error = nil, want non-nil")
	}
	if err := svc.CreateFabricNode(ctx, "fab0", &sdn.FabricNodeSpec{Protocol: sdn.FabricProtocolOSPF}); err == nil {
		t.Error("CreateFabricNode(no node_id) error = nil, want non-nil")
	}
	if err := svc.CreateFabricNode(ctx, "fab0", &sdn.FabricNodeSpec{NodeID: "pve1"}); err == nil {
		t.Error("CreateFabricNode(no protocol) error = nil, want non-nil")
	}
	// The mock refuses membership writes against an unseeded fabric.
	if err := svc.CreateFabricNode(ctx, "ghost", &sdn.FabricNodeSpec{
		NodeID: "pve1", Protocol: sdn.FabricProtocolOSPF,
	}); !errors.Is(err, pverr.ErrNotFound) {
		t.Errorf("CreateFabricNode(ghost fabric) = %v, want ErrNotFound", err)
	}
}

// TestSDNStatusReads covers the node-scoped live-status surface end-to-end
// against the mock's synthesized state: seeded zones/vnets report available,
// and the VRF/route tables carry the synthetic rows with their array-valued
// fields preserved in Extra as raw JSON.
func TestSDNStatusReads(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddZone("zone0", "vxlan")
	mock.AddVNet("vnet0", "zone0")
	svc := newService(t, mock)
	ctx := context.Background()

	zones, err := svc.SDNStatus(ctx, "pve1")
	if err != nil {
		t.Fatalf("SDNStatus: %v", err)
	}
	if len(zones) != 1 || zones[0].Zone != "zone0" || zones[0].Status != "available" {
		t.Fatalf("SDNStatus = %+v, want zone0 available", zones)
	}

	vnets, err := svc.ZoneContent(ctx, "pve1", "zone0")
	if err != nil {
		t.Fatalf("ZoneContent: %v", err)
	}
	if len(vnets) != 1 || vnets[0].VNet != "vnet0" || vnets[0].Status != "available" {
		t.Fatalf("ZoneContent = %+v, want vnet0 available", vnets)
	}

	bridges, err := svc.ZoneBridges(ctx, "pve1", "zone0")
	if err != nil {
		t.Fatalf("ZoneBridges: %v", err)
	}
	if len(bridges) != 1 || bridges[0].Name != "vnet0" {
		t.Fatalf("ZoneBridges = %+v, want bridge vnet0", bridges)
	}
	if bridges[0].Extra["ports"] == "" {
		t.Errorf("bridge Extra[ports] empty, want the raw ports JSON preserved")
	}

	routes, err := svc.ZoneIPVRF(ctx, "pve1", "zone0")
	if err != nil {
		t.Fatalf("ZoneIPVRF: %v", err)
	}
	if len(routes) != 1 || routes[0].Metric != 20 {
		t.Fatalf("ZoneIPVRF = %+v, want one route with metric 20", routes)
	}
	if routes[0].Extra["nexthops"] == "" {
		t.Errorf("route Extra[nexthops] empty, want the raw nexthops JSON preserved")
	}

	entries, err := svc.VNetMACVRF(ctx, "pve1", "vnet0")
	if err != nil {
		t.Fatalf("VNetMACVRF: %v", err)
	}
	if len(entries) != 1 || entries[0].MAC == "" {
		t.Fatalf("VNetMACVRF = %+v, want one entry with a MAC", entries)
	}
}

func TestSDNStatusNotFound(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)
	ctx := context.Background()

	if _, err := svc.ZoneContent(ctx, "pve1", "ghost"); !errors.Is(err, pverr.ErrNotFound) {
		t.Errorf("ZoneContent(ghost zone) = %v, want ErrNotFound", err)
	}
	if _, err := svc.VNetMACVRF(ctx, "pve1", "ghost"); !errors.Is(err, pverr.ErrNotFound) {
		t.Errorf("VNetMACVRF(ghost vnet) = %v, want ErrNotFound", err)
	}
	if _, err := svc.FabricInterfaces(ctx, "pve1", "ghost"); !errors.Is(err, pverr.ErrNotFound) {
		t.Errorf("FabricInterfaces(ghost fabric) = %v, want ErrNotFound", err)
	}
}

// TestFabricStatusReads covers the fabric runtime reads: interfaces come from
// the requesting node's membership, neighbors are the OTHER member nodes, and
// routes carry the `via` array in Extra.
func TestFabricStatusReads(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddFabric("fab0", "openfabric")
	mock.AddFabricNode("fab0", "pve1")
	mock.AddFabricNode("fab0", "pve2")
	svc := newService(t, mock)
	ctx := context.Background()

	// pve1 has no seeded interfaces (AddFabricNode seeds membership only), so
	// grow them through the SDK update path first.
	if err := svc.UpdateFabricNode(ctx, "fab0", "pve1", &sdn.FabricNodeUpdate{
		Interfaces: []string{"ens19"},
	}); err != nil {
		t.Fatalf("UpdateFabricNode: %v", err)
	}

	ifaces, err := svc.FabricInterfaces(ctx, "pve1", "fab0")
	if err != nil {
		t.Fatalf("FabricInterfaces: %v", err)
	}
	if len(ifaces) != 1 || ifaces[0].Name != "ens19" || ifaces[0].State != "up" {
		t.Fatalf("FabricInterfaces = %+v, want ens19 up", ifaces)
	}

	neighbors, err := svc.FabricNeighbors(ctx, "pve1", "fab0")
	if err != nil {
		t.Fatalf("FabricNeighbors: %v", err)
	}
	if len(neighbors) != 1 || neighbors[0].Neighbor != "pve2" {
		t.Fatalf("FabricNeighbors = %+v, want the one other member pve2", neighbors)
	}
	if neighbors[0].Uptime == "" {
		t.Errorf("neighbor uptime empty, want FRR-style uptime string")
	}

	routes, err := svc.FabricRoutes(ctx, "pve1", "fab0")
	if err != nil {
		t.Fatalf("FabricRoutes: %v", err)
	}
	if len(routes) != 1 || routes[0].Route == "" {
		t.Fatalf("FabricRoutes = %+v, want one route", routes)
	}
	if routes[0].Extra["via"] == "" {
		t.Errorf("route Extra[via] empty, want the raw via JSON preserved")
	}
}

func TestZoneUnmarshalExtra(t *testing.T) {
	t.Parallel()
	// Keys outside the modelled set (here "dhcp") must land in Extra so a zone
	// read round-trips losslessly.
	const blob = `{"zone":"z1","type":"simple","mtu":1500,"dhcp":"dnsmasq","exitnodes":"pve1"}`
	var z sdn.Zone
	if err := json.Unmarshal([]byte(blob), &z); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if z.Zone != "z1" || z.Type != sdn.ZoneTypeSimple || z.MTU != 1500 {
		t.Errorf("modelled fields = %+v, want zone=z1 type=simple mtu=1500", z)
	}
	if z.Extra["dhcp"] != "dnsmasq" || z.Extra["exitnodes"] != "pve1" {
		t.Errorf("Extra = %v, want dhcp=dnsmasq exitnodes=pve1", z.Extra)
	}
}
