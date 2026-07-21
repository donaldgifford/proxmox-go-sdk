package sdn

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/internal/svcutil"
)

// FabricProtocol is the routing protocol an SDN fabric runs. OpenFabric and
// OSPF are the 9.0 baseline; BGP and WireGuard are 9.2 additions (see
// SDNAdvancedFabrics). The full enum is confirmed by the real 9.2 apidoc.
type FabricProtocol string

// The SDN fabric routing protocols. openfabric/ospf are baseline (9.0);
// bgp/wireguard are gated on PVE 9.2 (SDNAdvancedFabrics).
const (
	FabricProtocolOpenFabric FabricProtocol = "openfabric"
	FabricProtocolOSPF       FabricProtocol = "ospf"
	FabricProtocolBGP        FabricProtocol = "bgp"
	FabricProtocolWireGuard  FabricProtocol = "wireguard"
)

// Fabric is one entry from GET /cluster/sdn/fabrics/fabric or
// /cluster/sdn/fabrics/fabric/{id}. Reads are lossless: fabric configs are
// protocol-dependent (OpenFabric/OSPF carry an area and timers, WireGuard-style
// keepalives appear on 9.2), so unmodelled keys — including the per-protocol
// tunables, the `redistribute` list, and the SDN transaction fields
// (`lock-token`/`digest`, DESIGN-0003 OQ-6) — are preserved in Extra.
//
// The paths and field names are confirmed against the real 9.2 apidoc
// (INV-0004); the exact wire forms of the per-protocol tunables await the
// pvelab live run. Note that a fabric has NO nodes field — node membership is
// its own sub-collection (see FabricNode).
type Fabric struct {
	Fabric      string         `json:"id"`
	Protocol    FabricProtocol `json:"protocol,omitempty"`
	IPPrefix    string         `json:"ip_prefix,omitempty"`
	IP6Prefix   string         `json:"ip6_prefix,omitempty"`
	RouteFilter string         `json:"route_filter,omitempty"`
	// Extra carries fabric keys the SDK does not model.
	Extra map[string]string `json:"-"`
}

var fabricKnownFields = map[string]bool{
	"id": true, "protocol": true, "ip_prefix": true, "ip6_prefix": true,
	"route_filter": true,
}

// UnmarshalJSON decodes the modelled fields and routes unknown keys into Extra.
func (f *Fabric) UnmarshalJSON(data []byte) error {
	type alias Fabric
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("decode sdn fabric: %w", err)
	}
	*f = Fabric(a)
	extra, err := svcutil.DecodeExtra(data, fabricKnownFields)
	if err != nil {
		return fmt.Errorf("decode sdn fabric: %w", err)
	}
	f.Extra = extra
	return nil
}

// FabricSpec is the body of POST /cluster/sdn/fabrics/fabric. Fabric (the id)
// and Protocol are required. A Protocol beyond the 9.0 baseline
// (FabricProtocolBGP) requires PVE 9.2 — CreateFabric enforces this via
// SDNAdvancedFabrics. The `redistribute` list is deliberately unmodelled (its
// array wire form is unverified) — pass it via Extra if needed. Pass the spec
// by pointer.
type FabricSpec struct {
	Fabric      string         `json:"id"`
	Protocol    FabricProtocol `json:"protocol"`
	IPPrefix    string         `json:"ip_prefix,omitempty"`
	IP6Prefix   string         `json:"ip6_prefix,omitempty"`
	RouteFilter string         `json:"route_filter,omitempty"`
	// Extra carries PVE parameters the SDK does not model.
	Extra map[string]string `json:"-"`
}

// FabricUpdate is the body of PUT /cluster/sdn/fabrics/fabric/{id}. Setting an
// advanced Protocol requires PVE 9.2 (see FabricSpec). Use Delete to unset
// keys. Pass it by pointer.
type FabricUpdate struct {
	Protocol    FabricProtocol `json:"protocol,omitempty"`
	IPPrefix    string         `json:"ip_prefix,omitempty"`
	IP6Prefix   string         `json:"ip6_prefix,omitempty"`
	RouteFilter string         `json:"route_filter,omitempty"`
	Delete      string         `json:"delete,omitempty"`
	// Extra carries PVE parameters the SDK does not model.
	Extra map[string]string `json:"-"`
}

// advancedFabricProtocol reports whether p is a fabric protocol introduced after
// the 9.0 baseline and therefore gated on PVE 9.2 (SDNAdvancedFabrics).
func advancedFabricProtocol(p FabricProtocol) bool {
	return p == FabricProtocolBGP || p == FabricProtocolWireGuard
}

// ListFabrics returns every SDN fabric definition.
func (s *Service) ListFabrics(ctx context.Context) ([]Fabric, error) {
	var fabrics []Fabric
	if err := s.c.DoRequest(ctx, http.MethodGet, sdnFabricsPath(), nil, &fabrics); err != nil {
		return nil, fmt.Errorf("sdn.ListFabrics: %w", err)
	}
	return fabrics, nil
}

// GetFabric returns one fabric by id.
func (s *Service) GetFabric(ctx context.Context, fabric string) (*Fabric, error) {
	var f Fabric
	if err := s.c.DoRequest(ctx, http.MethodGet, sdnFabricPath(fabric), nil, &f); err != nil {
		return nil, fmt.Errorf("sdn.GetFabric: %w", err)
	}
	return &f, nil
}

// CreateFabric defines a new SDN fabric. The change is staged into the pending
// config; call ApplySDN to activate it. The write is synchronous (no task). A
// Protocol beyond the 9.0 baseline requires PVE 9.2, else this returns a
// pverr.ErrUnsupported-wrapped error before making any request.
func (s *Service) CreateFabric(ctx context.Context, spec *FabricSpec) error {
	if spec == nil {
		return fmt.Errorf("sdn.CreateFabric: %w", svcutil.ErrNilSpec)
	}
	switch {
	case spec.Fabric == "":
		return fmt.Errorf("sdn.CreateFabric: id: %w", svcutil.ErrMissingField)
	case spec.Protocol == "":
		return fmt.Errorf("sdn.CreateFabric: protocol: %w", svcutil.ErrMissingField)
	}
	if advancedFabricProtocol(spec.Protocol) {
		if err := s.caps.Require("SDN fabric protocol "+string(spec.Protocol), "9.2"); err != nil {
			return fmt.Errorf("sdn.CreateFabric: %w", err)
		}
	}
	body, err := svcutil.EncodeWithExtra(spec, spec.Extra)
	if err != nil {
		return fmt.Errorf("sdn.CreateFabric: %w", err)
	}
	if err := s.c.DoRequest(ctx, http.MethodPost, sdnFabricsPath(), body, nil); err != nil {
		return fmt.Errorf("sdn.CreateFabric: %w", err)
	}
	return nil
}

// UpdateFabric changes a staged fabric. The write is synchronous (no task); call
// ApplySDN to activate it. Setting an advanced Protocol requires PVE 9.2.
func (s *Service) UpdateFabric(ctx context.Context, fabric string, update *FabricUpdate) error {
	if update == nil {
		return fmt.Errorf("sdn.UpdateFabric: %w", svcutil.ErrNilSpec)
	}
	if fabric == "" {
		return fmt.Errorf("sdn.UpdateFabric: fabric: %w", svcutil.ErrMissingField)
	}
	if advancedFabricProtocol(update.Protocol) {
		if err := s.caps.Require("SDN fabric protocol "+string(update.Protocol), "9.2"); err != nil {
			return fmt.Errorf("sdn.UpdateFabric: %w", err)
		}
	}
	body, err := svcutil.EncodeWithExtra(update, update.Extra)
	if err != nil {
		return fmt.Errorf("sdn.UpdateFabric: %w", err)
	}
	if err := s.c.DoRequest(ctx, http.MethodPut, sdnFabricPath(fabric), body, nil); err != nil {
		return fmt.Errorf("sdn.UpdateFabric: %w", err)
	}
	return nil
}

// DeleteFabric removes a fabric from the pending config. The write is
// synchronous (no task); call ApplySDN to activate it.
func (s *Service) DeleteFabric(ctx context.Context, fabric string) error {
	if fabric == "" {
		return fmt.Errorf("sdn.DeleteFabric: fabric: %w", svcutil.ErrMissingField)
	}
	if err := s.c.DoRequest(ctx, http.MethodDelete, sdnFabricPath(fabric), nil, nil); err != nil {
		return fmt.Errorf("sdn.DeleteFabric: %w", err)
	}
	return nil
}
