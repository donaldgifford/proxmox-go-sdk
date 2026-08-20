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
// Phase 5 lands node networking; Phase 6 (task 4) extends the same Service with
// node administration — package updates, disks, and certificates/ACME.
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

	// Package management (task 4). RefreshAptCache runs a worker; the DEB822
	// repository field shapes are provisional (REST-with-caveat, see apt.go).
	ListAptUpdates(ctx context.Context, node string) ([]AptUpdate, error)
	RefreshAptCache(ctx context.Context, node string) (tasks.Ref, error)
	ListRepositories(ctx context.Context, node string) (*Repositories, error)
	UpdateRepository(ctx context.Context, node string, update *RepositoryUpdate) error

	// Disks (task 4). InitializeDisk runs a worker; the SMART attribute table
	// shape is device-dependent (REST-with-caveat, see disks.go).
	ListDisks(ctx context.Context, node string) ([]Disk, error)
	GetDiskSMART(ctx context.Context, node, disk string) (*SMART, error)
	InitializeDisk(ctx context.Context, node, disk string) (tasks.Ref, error)

	// Certificates + ACME (task 4). Custom-cert writes are synchronous; ACME
	// account and node-cert order/renew/revoke run workers (the latter are
	// REST-with-caveat, see certificates.go). ACME accounts are cluster-scoped
	// (no node argument). Custom node scripts have no REST endpoint — run them
	// over the SSH side-channel (c.SSH().Exec), so no method is offered here.
	GetNodeCertificates(ctx context.Context, node string) ([]Certificate, error)
	UploadCustomCertificate(ctx context.Context, node string, spec *CustomCertificateSpec) ([]Certificate, error)
	DeleteCustomCertificate(ctx context.Context, node string) error
	ListACMEAccounts(ctx context.Context) ([]string, error)
	GetACMEAccount(ctx context.Context, name string) (*ACMEAccount, error)
	RegisterACMEAccount(ctx context.Context, spec *ACMEAccountSpec) (tasks.Ref, error)
	UpdateACMEAccount(ctx context.Context, name string, update *ACMEAccountUpdate) (tasks.Ref, error)
	DeactivateACMEAccount(ctx context.Context, name string) (tasks.Ref, error)
	OrderNodeCertificate(ctx context.Context, node string) (tasks.Ref, error)
	RenewNodeCertificate(ctx context.Context, node string) (tasks.Ref, error)
	RevokeNodeCertificate(ctx context.Context, node string) (tasks.Ref, error)

	// ACME challenge plugins (IMPL-0007). Cluster-scoped like the accounts, and
	// every write is synchronous — a plugin is cluster config, not a worker.
	// CreateACMEPlugin and UpdateACMEPlugin carry provider credentials; see
	// ACMEPluginData.
	ListACMEPlugins(ctx context.Context) ([]ACMEPlugin, error)
	GetACMEPlugin(ctx context.Context, id string) (*ACMEPlugin, error)
	CreateACMEPlugin(ctx context.Context, spec *ACMEPluginSpec) error
	UpdateACMEPlugin(ctx context.Context, id string, update *ACMEPluginUpdate) error
	DeleteACMEPlugin(ctx context.Context, id string) error

	// Node configuration (IMPL-0007). The ACME property strings are typed both
	// ways; SetNodeConfig is synchronous and clears nothing implicitly — name a
	// key in NodeConfigUpdate.Delete to unset it.
	GetNodeConfig(ctx context.Context, node string) (*NodeConfig, error)
	SetNodeConfig(ctx context.Context, node string, update *NodeConfigUpdate) error

	// ACME discovery (IMPL-0007). GetACMEChallengeSchema publishes each
	// provider's credential fields — the runtime source of truth behind
	// ACMEPluginData — and the other two describe the CAs a node can order from.
	GetACMEChallengeSchema(ctx context.Context) ([]ACMEChallengeSchemaEntry, error)
	ListACMEDirectories(ctx context.Context) ([]ACMEDirectory, error)
	GetACMEMeta(ctx context.Context, opts ...ACMEMetaOption) (*ACMEMeta, error)
}

// Compile-time assertion that *Service implements the published contract.
var _ API = (*Service)(nil)
