package ha

import (
	"context"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/api"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/version"
)

// Service wraps PVE high availability: HA resources, the 9.x HA rules
// (node-affinity and resource-affinity — never the deprecated groups), the CRS
// scheduler settings, the 9.2 Dynamic Load Balancer, and storage replication
// jobs. HA is cluster-scoped: unlike the compute services it binds no node —
// every endpoint lives under /cluster. It is safe for concurrent use; construct
// it with NewService or via the root client's HA accessor.
type Service struct {
	c    api.Client
	caps version.Capabilities
}

// NewService returns an HA Service. caps is the version snapshot consulted to
// gate per-minor 9.x features; tests that do not exercise a gate may pass the
// zero version.Capabilities.
func NewService(c api.Client, caps version.Capabilities) *Service {
	return &Service{c: c, caps: caps}
}

// API is the HA service contract, published so consumers can stand in a test
// double for *Service. Every operation is cluster-scoped (no node argument).
// HA config writes are synchronous in PVE — they return an error, not a
// tasks.Ref; reads return typed data directly. The interface grows as later
// Phase 4 tasks land.
type API interface {
	// HA resources (task 1). A resource is identified by its SID, e.g.
	// "vm:100" or "ct:101". Adds/updates/removes are synchronous.
	ListResources(ctx context.Context) ([]HAResource, error)
	GetResource(ctx context.Context, sid string) (*HAResource, error)
	AddResource(ctx context.Context, spec *HAResourceSpec) error
	UpdateResource(ctx context.Context, sid string, update *HAResourceUpdate) error
	RemoveResource(ctx context.Context, sid string) error

	// HA rules (task 2) — the 9.x replacement for HA groups: node-affinity and
	// resource-affinity, with enable/disable. Writes are synchronous.
	ListRules(ctx context.Context) ([]HARule, error)
	GetRule(ctx context.Context, rule string) (*HARule, error)
	CreateRule(ctx context.Context, spec *HARuleSpec) error
	UpdateRule(ctx context.Context, rule string, update *HARuleUpdate) error
	DeleteRule(ctx context.Context, rule string) error

	// CRS settings (task 3) — the static-load scheduler config, stored in the
	// datacenter options. The write is synchronous.
	GetCRSSettings(ctx context.Context) (*CRSSettings, error)
	SetCRSSettings(ctx context.Context, update *CRSSettingsUpdate) error

	// Dynamic Load Balancer (task 4, 9.2+). Gated on DynamicLoadBalancer; the
	// REST path is provisional (see GetDLBStatus).
	GetDLBStatus(ctx context.Context) (*DLBStatus, error)
	SetDLBConfig(ctx context.Context, cfg *DLBConfig) error

	// Arm/Disarm cluster-wide HA switch (9.2+, gated on HAClusterSwitch).
	// Both are synchronous POSTs to /cluster/ha/status/{arm,disarm}-ha; the
	// disarm resource-mode is required (see DisarmHA).
	ArmHA(ctx context.Context) error
	DisarmHA(ctx context.Context, mode ResourceMode) error

	// Storage/ZFS replication jobs (task 6). Requires the 9.x VM.Replicate
	// privilege. Writes are synchronous.
	ListReplicationJobs(ctx context.Context) ([]ReplicationJob, error)
	GetReplicationJob(ctx context.Context, id string) (*ReplicationJob, error)
	CreateReplicationJob(ctx context.Context, spec *ReplicationSpec) error
	UpdateReplicationJob(ctx context.Context, id string, update *ReplicationUpdate) error
	DeleteReplicationJob(ctx context.Context, id string) error
}

// Compile-time assertion that *Service implements the published contract.
var _ API = (*Service)(nil)
