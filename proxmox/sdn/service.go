package sdn

import (
	"context"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/api"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/version"
)

// Service wraps PVE software-defined networking: zones, VNets, and their
// subnets, fabrics and their node membership, plus the cluster-wide apply that
// activates staged changes. Configuration is cluster-scoped — every config
// endpoint lives under /cluster/sdn and binds no node; the live-status reads
// are node-scoped (/nodes/{node}/sdn) and take the node per call. It is safe
// for concurrent use; construct it with NewService or via the root client's
// SDN accessor.
//
// SDN configuration is transactional: creates, updates, and deletes stage
// changes into a pending config, and ApplySDN commits them cluster-wide. All
// config writes are synchronous (they return an error, not a tasks.Ref).
type Service struct {
	c    api.Client
	caps version.Capabilities
}

// NewService returns an SDN Service. caps is the version snapshot consulted to
// gate per-minor 9.x features; tests that do not exercise a gate may pass the
// zero version.Capabilities.
func NewService(c api.Client, caps version.Capabilities) *Service {
	return &Service{c: c, caps: caps}
}

// API is the SDN service contract, published so consumers can stand in a test
// double for *Service. Config operations are cluster-scoped (no node
// argument); the live-status reads are node-scoped and take the node per call.
// Config writes are synchronous — they return an error, not a tasks.Ref;
// reads return typed data directly.
type API interface {
	// Zones (task 2). A zone is the SDN backing technology (simple, VLAN, QinQ,
	// VXLAN, EVPN). Writes stage into the pending config; call ApplySDN to
	// commit.
	ListZones(ctx context.Context) ([]Zone, error)
	GetZone(ctx context.Context, zone string) (*Zone, error)
	CreateZone(ctx context.Context, spec *ZoneSpec) error
	UpdateZone(ctx context.Context, zone string, update *ZoneUpdate) error
	DeleteZone(ctx context.Context, zone string) error

	// VNets and their subnets (task 2). A VNet belongs to a zone; subnets are
	// nested under a VNet. Writes stage into the pending config.
	ListVNets(ctx context.Context) ([]VNet, error)
	GetVNet(ctx context.Context, vnet string) (*VNet, error)
	CreateVNet(ctx context.Context, spec *VNetSpec) error
	UpdateVNet(ctx context.Context, vnet string, update *VNetUpdate) error
	DeleteVNet(ctx context.Context, vnet string) error

	ListSubnets(ctx context.Context, vnet string) ([]Subnet, error)
	GetSubnet(ctx context.Context, vnet, subnet string) (*Subnet, error)
	CreateSubnet(ctx context.Context, vnet string, spec *SubnetSpec) error
	UpdateSubnet(ctx context.Context, vnet, subnet string, update *SubnetUpdate) error
	DeleteSubnet(ctx context.Context, vnet, subnet string) error

	// Fabrics (9.0+) — the OpenFabric/OSPF routing layer, at
	// /cluster/sdn/fabrics/fabric (paths confirmed against the real 9.2
	// apidoc, INV-0004). Writes stage into the pending config; a protocol
	// beyond the 9.0 baseline requires PVE 9.2 (SDNAdvancedFabrics).
	ListFabrics(ctx context.Context) ([]Fabric, error)
	GetFabric(ctx context.Context, fabric string) (*Fabric, error)
	CreateFabric(ctx context.Context, spec *FabricSpec) error
	UpdateFabric(ctx context.Context, fabric string, update *FabricUpdate) error
	DeleteFabric(ctx context.Context, fabric string) error

	// Fabric node membership — a fabric's nodes are their own sub-collection
	// at /cluster/sdn/fabrics/node/{fabric} (a fabric config has no nodes
	// field). Writes stage into the pending config.
	ListFabricNodes(ctx context.Context, fabric string) ([]FabricNode, error)
	GetFabricNode(ctx context.Context, fabric, node string) (*FabricNode, error)
	CreateFabricNode(ctx context.Context, fabric string, spec *FabricNodeSpec) error
	UpdateFabricNode(ctx context.Context, fabric, node string, update *FabricNodeUpdate) error
	DeleteFabricNode(ctx context.Context, fabric, node string) error

	// ApplySDN commits the pending SDN config cluster-wide (task 2). It is
	// synchronous.
	ApplySDN(ctx context.Context) error

	// SDN live status — node-scoped reads under /nodes/{node}/sdn. There is
	// no per-VNet status endpoint (the vnets/{vnet} path is a subdir index);
	// ZoneContent is the per-VNet health read and VNetMACVRF the EVPN view.
	SDNStatus(ctx context.Context, node string) ([]SDNZoneStatus, error)
	ZoneContent(ctx context.Context, node, zone string) ([]VNetStatus, error)
	ZoneBridges(ctx context.Context, node, zone string) ([]ZoneBridge, error)
	ZoneIPVRF(ctx context.Context, node, zone string) ([]IPVRFRoute, error)
	VNetMACVRF(ctx context.Context, node, vnet string) ([]MACVRFEntry, error)
	FabricInterfaces(ctx context.Context, node, fabric string) ([]FabricInterface, error)
	FabricNeighbors(ctx context.Context, node, fabric string) ([]FabricNeighbor, error)
	FabricRoutes(ctx context.Context, node, fabric string) ([]FabricRoute, error)
}

// Compile-time assertion that *Service implements the published contract.
var _ API = (*Service)(nil)
