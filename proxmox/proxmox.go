package proxmox

import (
	"context"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/access"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/api"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/cluster"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/firewall"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/ha"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/lxc"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/metrics"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/nodes"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/qemu"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/sdn"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/ssh"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/storage"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/tasks"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/version"
)

// Client is the unified Proxmox VE SDK client. It targets a single PVE cluster,
// is safe for concurrent use, and exposes typed per-domain services. Build it
// with NewClient.
type Client struct {
	api     api.Client
	version *version.Service
	tasks   *tasks.Service
	caps    version.Capabilities
}

// NewClient builds a client for the PVE cluster reachable at endpoint,
// authenticated by creds (use api.TokenCredentials, api.TicketCredentials, or
// api.UserCredentials). It fetches /version once to seed Capabilities,
// rejecting any release below the 9.0 floor (ADR-0002) with pverr.ErrUnsupported.
//
// endpoint is the primary node address (host, host:port, or URL); supply the
// cluster's other nodes via WithClusterEndpoints for transport-level failover.
func NewClient(ctx context.Context, endpoint string, creds api.Credentials, opts ...Option) (*Client, error) {
	var cfg clientConfig
	for _, opt := range opts {
		opt(&cfg)
	}

	transport, err := api.New(endpoint, creds, cfg.transport...)
	if err != nil {
		return nil, err
	}

	vsvc := version.NewService(transport)
	caps, err := vsvc.Capabilities(ctx)
	if err != nil {
		return nil, err
	}

	return &Client{
		api:     transport,
		version: vsvc,
		tasks:   tasks.NewService(transport),
		caps:    caps,
	}, nil
}

// API returns the low-level transport — an escape hatch for calls the typed
// services do not yet cover.
func (c *Client) API() api.Client { return c.api }

// Version returns the version service (GET /version, capability parsing).
func (c *Client) Version() *version.Service { return c.version }

// Capabilities returns the version snapshot seeded at construction. Services
// consult it to gate per-minor 9.x features.
func (c *Client) Capabilities() version.Capabilities { return c.caps }

// Tasks returns the task service for awaiting UPIDs. No node argument is needed:
// the node a task runs on is carried by the tasks.Ref (and encoded in its UPID).
func (c *Client) Tasks() *tasks.Service { return c.tasks }

// QEMU returns a QEMU/VM service scoped to node (e.g. "pve"). It shares the
// client's transport and capability snapshot and is safe for concurrent use.
func (c *Client) QEMU(node string) *qemu.Service {
	return qemu.NewService(c.api, node, c.caps)
}

// LXC returns an LXC container service scoped to node (e.g. "pve"). It shares
// the client's transport and capability snapshot and is safe for concurrent use.
func (c *Client) LXC(node string) *lxc.Service {
	return lxc.NewService(c.api, node, c.caps)
}

// Storage returns the storage service. It is not node-scoped: datastore
// configuration is cluster-wide and node-scoped operations take a node
// argument. It shares the client's transport and capability snapshot.
func (c *Client) Storage() *storage.Service {
	return storage.NewService(c.api, c.caps)
}

// Nodes returns the node-administration service. Every operation is node-scoped
// (the node is a per-call argument), so one service serves the whole cluster.
// Phase 5 covers node networking; later phases add status, packages, and disks.
func (c *Client) Nodes() *nodes.Service {
	return nodes.NewService(c.api, c.caps)
}

// HA returns the high-availability service. It is cluster-scoped (no node
// argument): HA resources, rules, CRS settings, the Dynamic Load Balancer, and
// replication jobs are all cluster-wide. It shares the client's transport and
// capability snapshot.
func (c *Client) HA() *ha.Service {
	return ha.NewService(c.api, c.caps)
}

// SDN returns the software-defined-networking service. It is cluster-scoped (no
// node argument): zones, VNets, subnets, and the cluster-wide apply are all
// cluster-wide. It shares the client's transport and capability snapshot.
func (c *Client) SDN() *sdn.Service {
	return sdn.NewService(c.api, c.caps)
}

// Cluster returns the cluster service — cluster-wide resource inventory, status,
// and datacenter options. It is cluster-scoped (no node argument).
func (c *Client) Cluster() *cluster.Service {
	return cluster.NewService(c.api, c.caps)
}

// Access returns the access-control service — users, groups, roles, ACLs, and
// API tokens under the 9.x privilege model. It is cluster-scoped.
func (c *Client) Access() *access.Service {
	return access.NewService(c.api, c.caps)
}

// Metrics returns the metrics service — node/guest RRD series and status, plus
// external metric-server (InfluxDB/Graphite) configuration. Its scope is mixed
// (reads take a node; server CRUD is cluster-scoped), so it binds no node.
func (c *Client) Metrics() *metrics.Service {
	return metrics.NewService(c.api, c.caps)
}

// Firewall returns the datacenter (cluster) firewall service. Use NodeFirewall
// or GuestFirewall for the node- and guest-scoped firewalls; all three expose
// the same rule / IPSet / options surface.
func (c *Client) Firewall() *firewall.Service {
	return firewall.NewClusterScope(c.api, c.caps)
}

// NodeFirewall returns the firewall service scoped to node (e.g. "pve").
func (c *Client) NodeFirewall(node string) *firewall.Service {
	return firewall.NewNodeScope(c.api, c.caps, node)
}

// GuestFirewall returns the firewall service scoped to a single guest — a QEMU
// VM or LXC container (select with kind) identified by vmid on node.
func (c *Client) GuestFirewall(node string, kind firewall.GuestKind, vmid int) *firewall.Service {
	return firewall.NewGuestScope(c.api, c.caps, kind, node, vmid)
}

// SSH returns a disconnected SSH/SFTP side-channel client configured by opts
// (host-key verification and credentials are supplied here, not by the REST
// transport). Unlike the REST services this client is single-use and not safe
// for concurrent use: Connect to a node, use it, then Close. It exists for the
// few operations the REST API cannot do — uploading snippets and backup
// archives, and the occasional host command. See package ssh.
func (*Client) SSH(opts ...ssh.Option) *ssh.Client {
	return ssh.NewClient(opts...)
}
