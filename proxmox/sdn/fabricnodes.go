package sdn

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/internal/svcutil"
)

// FabricNode is one entry from GET /cluster/sdn/fabrics/node/{fabric} — a
// node's membership in a fabric, carrying that node's fabric addressing. Reads
// are lossless: the per-protocol fields (interfaces, WireGuard peers/endpoint/
// public_key, transaction lock-token/digest) are preserved in Extra until the
// pvelab live run confirms their wire forms.
type FabricNode struct {
	NodeID   string         `json:"node_id"`
	Fabric   string         `json:"fabric_id,omitempty"`
	Protocol FabricProtocol `json:"protocol,omitempty"`
	IP       string         `json:"ip,omitempty"`
	IP6      string         `json:"ip6,omitempty"`
	// Extra carries fabric-node keys the SDK does not model.
	Extra map[string]string `json:"-"`
}

var fabricNodeKnownFields = map[string]bool{
	"node_id": true, "fabric_id": true, "protocol": true, "ip": true, "ip6": true,
}

// UnmarshalJSON decodes the modelled fields and routes unknown keys into Extra.
func (n *FabricNode) UnmarshalJSON(data []byte) error {
	type alias FabricNode
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("decode sdn fabric node: %w", err)
	}
	*n = FabricNode(a)
	extra, err := svcutil.DecodeExtra(data, fabricNodeKnownFields)
	if err != nil {
		return fmt.Errorf("decode sdn fabric node: %w", err)
	}
	n.Extra = extra
	return nil
}

// FabricNodeSpec is the body of POST /cluster/sdn/fabrics/node/{fabric}.
// NodeID and Protocol are required (the protocol must match the fabric's); IP
// is a bare IPv4 address (the apidoc format is ipv4, not CIDR). Each
// Interfaces entry is a PVE property string — minimally "name=<iface>",
// optionally with per-protocol keys such as "name=ens19,ip=10.0.0.1/31" — and
// the slice is sent as repeated `interfaces` form values. Pass the spec by
// pointer.
type FabricNodeSpec struct {
	NodeID   string         `json:"node_id"`
	Protocol FabricProtocol `json:"protocol"`
	IP       string         `json:"ip,omitempty"`
	IP6      string         `json:"ip6,omitempty"`
	// Interfaces entries are property strings ("name=<iface>[,…]"), sent as
	// repeated `interfaces` form values.
	Interfaces []string `json:"-"`
	// Extra carries PVE parameters the SDK does not model.
	Extra map[string]string `json:"-"`
}

// FabricNodeUpdate is the body of PUT /cluster/sdn/fabrics/node/{fabric}/{node}.
// Use Delete to unset keys. Pass it by pointer.
type FabricNodeUpdate struct {
	IP     string `json:"ip,omitempty"`
	IP6    string `json:"ip6,omitempty"`
	Delete string `json:"delete,omitempty"`
	// Interfaces is sent as repeated `interfaces` form values.
	Interfaces []string `json:"-"`
	// Extra carries PVE parameters the SDK does not model.
	Extra map[string]string `json:"-"`
}

// ListFabricNodes returns the node membership of one fabric.
func (s *Service) ListFabricNodes(ctx context.Context, fabric string) ([]FabricNode, error) {
	if fabric == "" {
		return nil, fmt.Errorf("sdn.ListFabricNodes: fabric: %w", svcutil.ErrMissingField)
	}
	var nodes []FabricNode
	if err := s.c.DoRequest(ctx, http.MethodGet, sdnFabricNodesPath(fabric), nil, &nodes); err != nil {
		return nil, fmt.Errorf("sdn.ListFabricNodes: %w", err)
	}
	return nodes, nil
}

// GetFabricNode returns one node's membership in a fabric.
func (s *Service) GetFabricNode(ctx context.Context, fabric, node string) (*FabricNode, error) {
	if fabric == "" || node == "" {
		return nil, fmt.Errorf("sdn.GetFabricNode: fabric/node: %w", svcutil.ErrMissingField)
	}
	var n FabricNode
	if err := s.c.DoRequest(ctx, http.MethodGet, sdnFabricNodePath(fabric, node), nil, &n); err != nil {
		return nil, fmt.Errorf("sdn.GetFabricNode: %w", err)
	}
	return &n, nil
}

// CreateFabricNode adds a node to a fabric. The change is staged into the
// pending config; call ApplySDN to activate it. The write is synchronous.
func (s *Service) CreateFabricNode(ctx context.Context, fabric string, spec *FabricNodeSpec) error {
	if spec == nil {
		return fmt.Errorf("sdn.CreateFabricNode: %w", svcutil.ErrNilSpec)
	}
	switch {
	case fabric == "":
		return fmt.Errorf("sdn.CreateFabricNode: fabric: %w", svcutil.ErrMissingField)
	case spec.NodeID == "":
		return fmt.Errorf("sdn.CreateFabricNode: node_id: %w", svcutil.ErrMissingField)
	case spec.Protocol == "":
		return fmt.Errorf("sdn.CreateFabricNode: protocol: %w", svcutil.ErrMissingField)
	}
	body, err := svcutil.EncodeWithExtra(spec, spec.Extra)
	if err != nil {
		return fmt.Errorf("sdn.CreateFabricNode: %w", err)
	}
	if len(spec.Interfaces) > 0 {
		body["interfaces"] = spec.Interfaces
	}
	if err := s.c.DoRequest(ctx, http.MethodPost, sdnFabricNodesPath(fabric), body, nil); err != nil {
		return fmt.Errorf("sdn.CreateFabricNode: %w", err)
	}
	return nil
}

// UpdateFabricNode changes a node's staged fabric membership. The write is
// synchronous; call ApplySDN to activate it.
func (s *Service) UpdateFabricNode(ctx context.Context, fabric, node string, update *FabricNodeUpdate) error {
	if update == nil {
		return fmt.Errorf("sdn.UpdateFabricNode: %w", svcutil.ErrNilSpec)
	}
	if fabric == "" || node == "" {
		return fmt.Errorf("sdn.UpdateFabricNode: fabric/node: %w", svcutil.ErrMissingField)
	}
	body, err := svcutil.EncodeWithExtra(update, update.Extra)
	if err != nil {
		return fmt.Errorf("sdn.UpdateFabricNode: %w", err)
	}
	if len(update.Interfaces) > 0 {
		body["interfaces"] = update.Interfaces
	}
	if err := s.c.DoRequest(ctx, http.MethodPut, sdnFabricNodePath(fabric, node), body, nil); err != nil {
		return fmt.Errorf("sdn.UpdateFabricNode: %w", err)
	}
	return nil
}

// DeleteFabricNode removes a node from a fabric's pending config. The write is
// synchronous; call ApplySDN to activate it.
func (s *Service) DeleteFabricNode(ctx context.Context, fabric, node string) error {
	if fabric == "" || node == "" {
		return fmt.Errorf("sdn.DeleteFabricNode: fabric/node: %w", svcutil.ErrMissingField)
	}
	if err := s.c.DoRequest(ctx, http.MethodDelete, sdnFabricNodePath(fabric, node), nil, nil); err != nil {
		return fmt.Errorf("sdn.DeleteFabricNode: %w", err)
	}
	return nil
}
