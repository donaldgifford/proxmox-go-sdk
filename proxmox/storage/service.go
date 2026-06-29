package storage

import (
	"context"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/api"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/version"
)

// Service wraps PVE storage: cluster datastore configuration plus the
// node-scoped status, content, volume, upload, and ZFS endpoints. Unlike the
// compute services it does not bind a node — datastore config is cluster-wide,
// and every node-scoped operation takes a node argument. It is safe for
// concurrent use; construct it with NewService or via the root client's Storage
// accessor.
type Service struct {
	c    api.Client
	caps version.Capabilities
}

// NewService returns a storage Service. caps is the version snapshot consulted
// to gate per-minor 9.x features; tests that do not exercise a gate may pass the
// zero version.Capabilities.
func NewService(c api.Client, caps version.Capabilities) *Service {
	return &Service{c: c, caps: caps}
}

// API is the storage service contract, published so consumers can stand in a
// test double for *Service. Cluster-scoped datastore reads take no node;
// everything else takes the node the storage is accessed from. Operations that
// start a PVE worker return a tasks.Ref the caller awaits with the client's task
// service; reads return data directly.
type API interface {
	// Datastore configuration (cluster-scoped) and per-node status (task 1).
	ListDatastores(ctx context.Context) ([]Datastore, error)
	GetDatastore(ctx context.Context, storage string) (*Datastore, error)
	ListNodeStorage(ctx context.Context, node string) ([]StorageStatus, error)
	NodeStorageStatus(ctx context.Context, node, storage string) (*StorageStatus, error)

	// Content listing (task 1).
	ListContent(ctx context.Context, node, storage string, opts ...ListContentOption) ([]Content, error)
	GetVolume(ctx context.Context, node, storage, volid string) (*Content, error)
}

// Compile-time assertion that *Service implements the published contract. The
// interface grows as later Phase 3 tasks land.
var _ API = (*Service)(nil)
