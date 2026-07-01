package nodes

import (
	"context"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/api"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/tasks"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/version"
)

// Service wraps per-node administration. Every operation is node-scoped: the
// node name is a per-call argument (the service binds none), so one Service
// serves the whole cluster. It is safe for concurrent use; construct it with
// NewService or via the root client's Nodes accessor.
//
// Phase 5 lands node networking; later phases extend the same Service with node
// status, packages, disks, and certificates.
type Service struct {
	c    api.Client
	caps version.Capabilities
}

// NewService returns a nodes Service. caps is the version snapshot consulted to
// gate per-minor features; tests that do not exercise a gate may pass the zero
// version.Capabilities.
func NewService(c api.Client, caps version.Capabilities) *Service {
	return &Service{c: c, caps: caps}
}

// API is the nodes service contract, published so consumers can stand in a test
// double for *Service. All operations take the node as their first data
// argument. Interface config writes are synchronous (return an error); applying
// the pending config may start a worker (returns a tasks.Ref). The interface
// grows as later phases add node-admin operations.
type API interface {
	// Node networking (task 1). Interface changes are staged into the pending
	// network config; ApplyNetworkConfig activates them.
	ListInterfaces(ctx context.Context, node string) ([]Interface, error)
	GetInterface(ctx context.Context, node, iface string) (*Interface, error)
	CreateInterface(ctx context.Context, node string, spec *InterfaceSpec) error
	UpdateInterface(ctx context.Context, node, iface string, update *InterfaceUpdate) error
	DeleteInterface(ctx context.Context, node, iface string) error
	ApplyNetworkConfig(ctx context.Context, node string) (tasks.Ref, error)
}

// Compile-time assertion that *Service implements the published contract.
var _ API = (*Service)(nil)
