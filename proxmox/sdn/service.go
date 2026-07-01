package sdn

import (
	"context"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/api"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/version"
)

// Service wraps PVE software-defined networking: zones, VNets, and their
// subnets, plus the cluster-wide apply that activates staged changes. SDN is
// cluster-scoped — every endpoint lives under /cluster/sdn and binds no node.
// It is safe for concurrent use; construct it with NewService or via the root
// client's SDN accessor.
//
// SDN configuration is transactional: creates, updates, and deletes stage
// changes into a pending config, and ApplySDN commits them cluster-wide. All
// config writes are synchronous (they return an error, not a tasks.Ref).
//
// Task 2 lands zones, VNets, and subnets; task 3 adds fabrics; task 4 adds the
// (currently unsupported) live status reads.
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
// double for *Service. Every operation is cluster-scoped (no node argument).
// Config writes are synchronous — they return an error, not a tasks.Ref; reads
// return typed data directly. The interface grows as later Phase 5 tasks land.
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

	// Fabrics (task 3, 9.0+) — the OpenFabric/OSPF routing layer. Writes stage
	// into the pending config. The REST surface is provisional (see Fabric); a
	// protocol beyond the 9.0 baseline requires PVE 9.2 (SDNAdvancedFabrics).
	ListFabrics(ctx context.Context) ([]Fabric, error)
	GetFabric(ctx context.Context, fabric string) (*Fabric, error)
	CreateFabric(ctx context.Context, spec *FabricSpec) error
	UpdateFabric(ctx context.Context, fabric string, update *FabricUpdate) error
	DeleteFabric(ctx context.Context, fabric string) error

	// ApplySDN commits the pending SDN config cluster-wide (task 2). It is
	// synchronous.
	ApplySDN(ctx context.Context) error
}

// Compile-time assertion that *Service implements the published contract.
var _ API = (*Service)(nil)
