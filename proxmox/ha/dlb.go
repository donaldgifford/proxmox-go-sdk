package ha

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/internal/svcutil"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/types"
)

// DLBStatus is the Dynamic Load Balancer configuration and running state (9.2+).
// The DLB continuously rebalances HA services across nodes using the CRS
// scheduler, rather than only relocating on node failure. Reads are lossless:
// keys outside the typed set are preserved in Extra (the endpoint shape is
// provisional — see GetDLBStatus).
type DLBStatus struct {
	Enabled types.PVEBool `json:"enabled,omitempty"`
	Mode    string        `json:"mode,omitempty"` // scheduler mode, e.g. "static".
	// Extra carries fields the SDK does not model.
	Extra map[string]string `json:"-"`
}

// dlbStatusKnownFields lists the JSON keys DLBStatus models directly; keep it
// in sync with the struct so UnmarshalJSON routes only the rest into Extra.
var dlbStatusKnownFields = map[string]bool{"enabled": true, "mode": true}

// UnmarshalJSON decodes the modelled fields and routes any unknown keys into
// Extra so the read round-trips losslessly.
func (d *DLBStatus) UnmarshalJSON(data []byte) error {
	type alias DLBStatus
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("decode dlb status: %w", err)
	}
	*d = DLBStatus(a)

	var all map[string]json.RawMessage
	if err := json.Unmarshal(data, &all); err != nil {
		return fmt.Errorf("decode dlb status map: %w", err)
	}
	for key, raw := range all {
		if dlbStatusKnownFields[key] {
			continue
		}
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			s = string(raw)
		}
		if d.Extra == nil {
			d.Extra = make(map[string]string)
		}
		d.Extra[key] = s
	}
	return nil
}

// DLBConfig is the body of the Dynamic Load Balancer config write (9.2+). Pass
// it by pointer.
type DLBConfig struct {
	Enabled types.PVEBool `json:"enabled"`
	Mode    string        `json:"mode,omitempty"`
	// Extra carries PVE parameters the SDK does not model.
	Extra map[string]string `json:"-"`
}

// GetDLBStatus returns the Dynamic Load Balancer configuration and status. It is
// gated on the 9.2 DynamicLoadBalancer capability.
//
// API-shape caveat: the PVE 9.2 REST path for the DLB is unconfirmed without a
// live 9.2 node. The provisional path (/cluster/ha/lbalancer) mirrors PVE's
// ha-manager "lbalancer" naming; adjust dlbPath in paths.go once confirmed. The
// capability gate fires before any request, so a sub-9.2 node never reaches the
// wire.
func (s *Service) GetDLBStatus(ctx context.Context) (*DLBStatus, error) {
	if err := s.caps.Require("Dynamic Load Balancer", "9.2"); err != nil {
		return nil, fmt.Errorf("ha.GetDLBStatus: %w", err)
	}
	var status DLBStatus
	if err := s.c.DoRequest(ctx, http.MethodGet, dlbPath(), nil, &status); err != nil {
		return nil, fmt.Errorf("ha.GetDLBStatus: %w", err)
	}
	return &status, nil
}

// SetDLBConfig writes the Dynamic Load Balancer configuration (enable/disable
// and mode). Gated on 9.2. The write is synchronous (no task). Same API-shape
// caveat as GetDLBStatus.
func (s *Service) SetDLBConfig(ctx context.Context, cfg *DLBConfig) error {
	if cfg == nil {
		return fmt.Errorf("ha.SetDLBConfig: %w", svcutil.ErrNilSpec)
	}
	if err := s.caps.Require("Dynamic Load Balancer", "9.2"); err != nil {
		return fmt.Errorf("ha.SetDLBConfig: %w", err)
	}
	body, err := svcutil.EncodeWithExtra(cfg, cfg.Extra)
	if err != nil {
		return fmt.Errorf("ha.SetDLBConfig: %w", err)
	}
	if err := s.c.DoRequest(ctx, http.MethodPut, dlbPath(), body, nil); err != nil {
		return fmt.Errorf("ha.SetDLBConfig: %w", err)
	}
	return nil
}
