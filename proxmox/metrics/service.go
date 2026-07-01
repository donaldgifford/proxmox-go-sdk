package metrics

import (
	"context"
	"errors"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/api"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/internal/svcutil"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/types"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/version"
)

// errMissingField aliases the shared sentinel so this package's guards read
// uniformly; errBadKind flags an unknown VMKind passed to GetVMRRD.
var (
	errMissingField = svcutil.ErrMissingField
	errBadKind      = errors.New("unknown guest kind (want qemu or lxc)")
)

// Service wraps metric reads and external metric-server configuration. Its scope
// is mixed: the RRD/status reads are node- (or guest-) scoped and take the node
// as a per-call argument, while the metric-server CRUD is cluster-scoped
// (/cluster/metrics/server). The service therefore binds no node; one *Service
// is safe for concurrent use. Construct it with NewService or via the root
// client's Metrics accessor.
type Service struct {
	c    api.Client
	caps version.Capabilities
}

// NewService returns a metrics Service. caps is accepted for parity with the
// other services and to gate the OpenTelemetry surface (OTelExporter, 9.1); the
// RRD/status/metric-server endpoints are baseline 9.0.
func NewService(c api.Client, caps version.Capabilities) *Service {
	return &Service{c: c, caps: caps}
}

// API is the metrics service contract, published so consumers can stand in a
// test double for *Service. RRD/status reads take a node (and, for guests, a
// kind + vmid); metric-server CRUD is cluster-scoped and synchronous (writes
// return an error, not a tasks.Ref). The OpenTelemetry methods have no 9.x REST
// endpoint and return pverr.ErrUnsupported (see otel.go).
type API interface {
	// RRD + status reads (node/guest scope).
	GetNodeRRD(ctx context.Context, node string, opts ...RRDOption) ([]RRDPoint, error)
	GetVMRRD(ctx context.Context, node string, kind VMKind, vmid types.VMID, opts ...RRDOption) ([]RRDPoint, error)
	GetNodeStatus(ctx context.Context, node string) (*NodeStatus, error)

	// External metric servers (cluster scope, synchronous writes).
	ListMetricServers(ctx context.Context) ([]MetricServer, error)
	GetMetricServer(ctx context.Context, id string) (*MetricServer, error)
	CreateMetricServer(ctx context.Context, spec *MetricServerSpec) error
	UpdateMetricServer(ctx context.Context, id string, update *MetricServerUpdate) error
	DeleteMetricServer(ctx context.Context, id string) error

	// OpenTelemetry exporter (9.1). File-configured in 9.x — these return
	// pverr.ErrUnsupported until a REST endpoint is confirmed.
	GetOTelConfig(ctx context.Context, node string) (*OTelConfig, error)
	SetOTelConfig(ctx context.Context, node string, cfg *OTelConfig) error
}

// Compile-time assertion that *Service implements the published contract.
var _ API = (*Service)(nil)
