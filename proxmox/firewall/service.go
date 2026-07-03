package firewall

import (
	"context"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/api"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/version"
)

// Service wraps the PVE firewall at one scope — the datacenter (cluster), a
// node, or a single guest. The three scopes share an identical rule / IPSet /
// options surface, so there is ONE Service type carrying a scope; the scope only
// changes which path prefix requests hit (see scope.path). Construct it with
// NewClusterScope, NewNodeScope, or NewGuestScope, or via the root client's
// Firewall / NodeFirewall / GuestFirewall accessors.
//
// A Service binds its scope at construction and is safe for concurrent use.
type Service struct {
	c     api.Client
	caps  version.Capabilities
	scope scope
}

// NewClusterScope returns a Service for the datacenter firewall
// (/cluster/firewall). caps gates per-minor features (e.g. the 9.1 IPSet
// rename).
func NewClusterScope(c api.Client, caps version.Capabilities) *Service {
	return &Service{c: c, caps: caps, scope: scope{kind: ScopeCluster}}
}

// NewNodeScope returns a Service for a node's firewall
// (/nodes/{node}/firewall).
func NewNodeScope(c api.Client, caps version.Capabilities, node string) *Service {
	return &Service{c: c, caps: caps, scope: scope{kind: ScopeNode, node: node}}
}

// NewGuestScope returns a Service for a single guest's firewall
// (/nodes/{node}/{qemu|lxc}/{vmid}/firewall). guest selects the QEMU or LXC path
// segment.
func NewGuestScope(c api.Client, caps version.Capabilities, guest GuestKind, node string, vmid int) *Service {
	return &Service{c: c, caps: caps, scope: scope{kind: ScopeGuest, guest: guest, node: node, vmid: vmid}}
}

// API is the firewall service contract, published so consumers can stand in a
// test double for *Service. Every method operates within the Service's scope
// (no scope argument). Firewall config writes are synchronous — they return an
// error, not a tasks.Ref; reads return typed data directly.
type API interface {
	// Rules — the scoped rule table, addressed by position (0 is first).
	ListRules(ctx context.Context) ([]Rule, error)
	GetRule(ctx context.Context, pos int) (*Rule, error)
	CreateRule(ctx context.Context, spec *RuleSpec) error
	UpdateRule(ctx context.Context, pos int, update *RuleUpdate) error
	DeleteRule(ctx context.Context, pos int) error

	// IPSets and their entries. RenameIPSet is gated on PVE 9.1
	// (OverlappingIPSets).
	ListIPSets(ctx context.Context) ([]IPSet, error)
	CreateIPSet(ctx context.Context, spec *IPSetSpec) error
	RenameIPSet(ctx context.Context, name, newName string) error
	DeleteIPSet(ctx context.Context, name string) error
	ListIPSetEntries(ctx context.Context, name string) ([]IPSetEntry, error)
	AddIPSetEntry(ctx context.Context, name string, entry *IPSetEntrySpec) error
	DeleteIPSetEntry(ctx context.Context, name, cidr string) error

	// Options — the scoped firewall options block (enable flag, default
	// policies, logging, and scope-specific toggles).
	GetOptions(ctx context.Context) (*Options, error)
	SetOptions(ctx context.Context, update *OptionsUpdate) error
}

// Compile-time assertion that *Service implements the published contract.
var _ API = (*Service)(nil)
