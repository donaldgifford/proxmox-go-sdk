package sdn

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/internal/svcutil"
)

// FabricProtocol is the routing protocol an SDN fabric runs. OpenFabric and OSPF
// are the 9.0 baseline; BGP is a 9.2 addition (see SDNAdvancedFabrics).
type FabricProtocol string

// The SDN fabric routing protocols. openfabric/ospf are baseline (9.0); bgp is
// gated on PVE 9.2 (SDNAdvancedFabrics).
const (
	FabricProtocolOpenFabric FabricProtocol = "openfabric"
	FabricProtocolOSPF       FabricProtocol = "ospf"
	FabricProtocolBGP        FabricProtocol = "bgp"
)

// Fabric is one entry from GET /cluster/sdn/fabrics or
// /cluster/sdn/fabrics/{fabric}. Reads are lossless: fabric configs are
// protocol-dependent (OpenFabric carries an area/net, OSPF an area, BGP an ASN),
// so unmodelled keys are preserved in Extra.
//
// API-shape caveat: SDN fabrics are a real 9.0 feature, but the exact REST path
// and field names have NOT been verified against a live 9.x node; the SDK
// targets /cluster/sdn/fabrics with the apidoc field names provisionally. Treat
// the modelled fields as best-effort and rely on Extra for anything unmodelled.
type Fabric struct {
	Fabric   string         `json:"id"`
	Protocol FabricProtocol `json:"protocol,omitempty"`
	Nodes    string         `json:"nodes,omitempty"` // CSV of member nodes.
	Comment  string         `json:"comment,omitempty"`
	// Extra carries fabric keys the SDK does not model.
	Extra map[string]string `json:"-"`
}

var fabricKnownFields = map[string]bool{
	"id": true, "protocol": true, "nodes": true, "comment": true,
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

// FabricSpec is the body of POST /cluster/sdn/fabrics. Fabric (the id) and
// Protocol are required. A Protocol beyond the 9.0 baseline (FabricProtocolBGP)
// requires PVE 9.2 — CreateFabric enforces this via SDNAdvancedFabrics. Pass it
// by pointer.
type FabricSpec struct {
	Fabric   string         `json:"id"`
	Protocol FabricProtocol `json:"protocol"`
	Nodes    string         `json:"nodes,omitempty"`
	Comment  string         `json:"comment,omitempty"`
	// Extra carries PVE parameters the SDK does not model.
	Extra map[string]string `json:"-"`
}

// FabricUpdate is the body of PUT /cluster/sdn/fabrics/{fabric}. Setting an
// advanced Protocol requires PVE 9.2 (see FabricSpec). Use Delete to unset keys.
// Pass it by pointer.
type FabricUpdate struct {
	Protocol FabricProtocol `json:"protocol,omitempty"`
	Nodes    string         `json:"nodes,omitempty"`
	Comment  string         `json:"comment,omitempty"`
	Delete   string         `json:"delete,omitempty"`
	// Extra carries PVE parameters the SDK does not model.
	Extra map[string]string `json:"-"`
}

// advancedFabricProtocol reports whether p is a fabric protocol introduced after
// the 9.0 baseline and therefore gated on PVE 9.2 (SDNAdvancedFabrics).
func advancedFabricProtocol(p FabricProtocol) bool {
	return p == FabricProtocolBGP
}

// ListFabrics returns every SDN fabric.
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
