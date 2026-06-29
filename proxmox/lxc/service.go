package lxc

import (
	"context"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/api"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/tasks"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/version"
)

// Service wraps the PVE LXC container endpoints under /nodes/{node}/lxc for a
// single cluster node. It is safe for concurrent use; construct it with
// NewService or via the root client's LXC accessor.
type Service struct {
	c    api.Client
	node string
	caps version.Capabilities
}

// NewService returns a Service bound to node. caps is the version snapshot the
// service consults to gate per-minor 9.x features; tests that do not exercise a
// gate may pass the zero version.Capabilities.
func NewService(c api.Client, node string, caps version.Capabilities) *Service {
	return &Service{c: c, node: node, caps: caps}
}

// API is the LXC service contract, published so consumers can stand in a test
// double for *Service. Operations that start a PVE worker return a tasks.Ref the
// caller awaits with the client's task service; reads return data directly.
type API interface {
	List(ctx context.Context) ([]Container, error)
	Get(ctx context.Context, vmid int) (*ContainerStatus, error)
	Config(ctx context.Context, vmid int) (*Config, error)
	SetConfig(ctx context.Context, vmid int, update *ConfigUpdate) (tasks.Ref, error)
	Create(ctx context.Context, spec *CreateSpec) (tasks.Ref, error)
	Clone(ctx context.Context, vmid int, spec *CloneSpec) (tasks.Ref, error)
	Delete(ctx context.Context, vmid int) (tasks.Ref, error)

	// Power transitions.
	Start(ctx context.Context, vmid int) (tasks.Ref, error)
	Stop(ctx context.Context, vmid int, opts ...StopOption) (tasks.Ref, error)
	Shutdown(ctx context.Context, vmid int, opts ...ShutdownOption) (tasks.Ref, error)
	Reboot(ctx context.Context, vmid int) (tasks.Ref, error)
	Suspend(ctx context.Context, vmid int) (tasks.Ref, error)
	Resume(ctx context.Context, vmid int) (tasks.Ref, error)

	// Snapshots (require a snapshot-capable backing store: ZFS, btrfs, or
	// LVM-thin; see SnapshotSpec).
	Snapshots(ctx context.Context, vmid int) ([]Snapshot, error)
	CreateSnapshot(ctx context.Context, vmid int, spec *SnapshotSpec) (tasks.Ref, error)
	RollbackSnapshot(ctx context.Context, vmid int, name string, opts ...RollbackOption) (tasks.Ref, error)
	DeleteSnapshot(ctx context.Context, vmid int, name string, opts ...DeleteSnapshotOption) (tasks.Ref, error)
}

// Compile-time assertion that *Service implements the full contract.
var _ API = (*Service)(nil)
