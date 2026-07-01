package ceph

import (
	"context"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/api"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/tasks"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/version"
)

// Service wraps Ceph pool, OSD, status, and (unsupported) RBD-mirroring
// operations. Ceph is a single cluster-wide entity, but its REST endpoints are
// addressed through a MON node, so every operation takes the node as a per-call
// argument and the service binds none — the root client's Ceph accessor takes no
// node. One *Service is safe for concurrent use; construct it with NewService.
type Service struct {
	c    api.Client
	caps version.Capabilities
}

// NewService returns a ceph Service. caps is accepted for parity with the other
// services; the Ceph endpoints are baseline 9.0 (Squid) and gate nothing.
func NewService(c api.Client, caps version.Capabilities) *Service {
	return &Service{c: c, caps: caps}
}

// API is the ceph service contract, published so consumers can stand in a test
// double for *Service. Every operation takes the node used to reach the cluster.
// Pool/OSD create and delete run workers (return a tasks.Ref); reads return
// typed data. RBD mirroring has no confirmed PVE REST endpoint and returns
// pverr.ErrUnsupported (see mirror.go).
type API interface {
	// Pools.
	ListPools(ctx context.Context, node string) ([]Pool, error)
	GetPool(ctx context.Context, node, name string) (*Pool, error)
	CreatePool(ctx context.Context, node string, spec *PoolSpec) (tasks.Ref, error)
	DeletePool(ctx context.Context, node, name string) (tasks.Ref, error)

	// OSDs.
	ListOSDs(ctx context.Context, node string) (*OSDTree, error)
	CreateOSD(ctx context.Context, node string, spec *OSDSpec) (tasks.Ref, error)
	DestroyOSD(ctx context.Context, node string, osdID int) (tasks.Ref, error)

	// Cluster status + config.
	GetStatus(ctx context.Context, node string) (*Status, error)
	GetClusterConfig(ctx context.Context, node string) (string, error)

	// RBD mirroring — no confirmed PVE REST endpoint; these return
	// pverr.ErrUnsupported (drive `rbd mirror` over SSH meanwhile).
	GetMirrorStatus(ctx context.Context, node, pool string) (*MirrorStatus, error)
	EnableMirroring(ctx context.Context, node string, spec *MirrorSpec) error
	DisableMirroring(ctx context.Context, node, pool string) error
}

// Compile-time assertion that *Service implements the published contract.
var _ API = (*Service)(nil)
