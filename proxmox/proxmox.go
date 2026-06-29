package proxmox

import (
	"context"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/api"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/qemu"
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
