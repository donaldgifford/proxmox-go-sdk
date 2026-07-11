package cluster

import (
	"context"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/api"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/version"
)

// Service wraps cluster-wide reads and datacenter options. It is cluster-scoped:
// every endpoint lives under /cluster and binds no node. Construct it with
// NewService or via the root client's Cluster accessor; one *Service is safe for
// concurrent use.
type Service struct {
	c    api.Client
	caps version.Capabilities
}

// NewService returns a cluster Service. caps is accepted for parity with the
// other services; the cluster endpoints are all baseline 9.0 and gate nothing.
func NewService(c api.Client, caps version.Capabilities) *Service {
	return &Service{c: c, caps: caps}
}

// API is the cluster service contract, published so consumers can stand in a
// test double for *Service. Every operation is cluster-scoped (no node
// argument). Reads return typed data; SetOptions is a synchronous write
// (returns an error, not a tasks.Ref).
type API interface {
	// ListResources returns the cluster resource inventory, optionally filtered
	// to one kind (WithResourceType).
	ListResources(ctx context.Context, opts ...ResourceFilter) ([]Resource, error)
	// GetStatus returns the /cluster/status entries — one "cluster" entry plus a
	// "node" entry per member.
	GetStatus(ctx context.Context) ([]StatusEntry, error)
	// GetOptions / SetOptions read and write the datacenter options block.
	GetOptions(ctx context.Context) (*Options, error)
	SetOptions(ctx context.Context, update *OptionsUpdate) error
	// CreateCluster forms a new cluster on the responding node; JoinCluster
	// joins the responding (fresh) node to an existing cluster. Both are
	// fire-and-poll writes: the response body is ignored beyond error status
	// and convergence is observed via ListConfigNodes.
	CreateCluster(ctx context.Context, spec *ClusterCreateSpec) error
	JoinCluster(ctx context.Context, spec *JoinSpec) error
	// JoinInfo returns what a joining node needs to know about this node's
	// cluster (nodelist, fingerprints, preferred contact node).
	JoinInfo(ctx context.Context) (*JoinInfo, error)
	// ListConfigNodes returns the corosync nodelist — the configured cluster
	// membership, and the convergence signal for the two writes above.
	ListConfigNodes(ctx context.Context) ([]ConfigNode, error)
}

// Compile-time assertion that *Service implements the published contract.
var _ API = (*Service)(nil)
