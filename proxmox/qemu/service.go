package qemu

import (
	"context"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/api"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/tasks"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/version"
)

// Service wraps the PVE QEMU/VM endpoints for a single cluster node. It is safe
// for concurrent use; construct it with NewService (or via the root client's
// QEMU accessor).
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

// API is the QEMU service contract, published so consumers can stand in a test
// double for *Service. Operations that start a PVE worker return a tasks.Ref the
// caller awaits with the client's task service; reads return data directly.
//
// As later phases add operations (power, migrate, snapshots, guest agent),
// callers holding a *Service gain them automatically; this interface is extended
// to match, so doubles that implement it are expected to track the SDK version.
type API interface {
	List(ctx context.Context) ([]VM, error)
	Get(ctx context.Context, vmid int) (*VMStatus, error)
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
	Suspend(ctx context.Context, vmid int, opts ...SuspendOption) (tasks.Ref, error)
	Resume(ctx context.Context, vmid int) (tasks.Ref, error)

	// Devices and migration.
	AddDisk(ctx context.Context, vmid int, spec *DiskSpec) (tasks.Ref, error)
	ResizeDisk(ctx context.Context, vmid int, disk, size string) (tasks.Ref, error)
	RemoveDisk(ctx context.Context, vmid int, slot string) (tasks.Ref, error)
	AddNIC(ctx context.Context, vmid int, spec *NICSpec) (tasks.Ref, error)
	RemoveNIC(ctx context.Context, vmid int, slot string) (tasks.Ref, error)
	Migrate(ctx context.Context, vmid int, spec *MigrateSpec) (tasks.Ref, error)
}

// Compile-time assertion that *Service implements the full contract.
var _ API = (*Service)(nil)
