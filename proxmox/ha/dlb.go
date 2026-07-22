package ha

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/pverr"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/types"
)

// DLBStatus is the Dynamic Load Balancer configuration and running state. The
// type is retained for a future PVE release that exposes a DLB REST endpoint
// (the storage.VolumeSnapshot precedent) — today none exists, and
// GetDLBStatus always returns pverr.ErrUnsupported.
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

// DLBConfig is the body of a Dynamic Load Balancer config write. Retained
// like DLBStatus; SetDLBConfig always returns pverr.ErrUnsupported today.
type DLBConfig struct {
	Enabled types.PVEBool `json:"enabled"`
	Mode    string        `json:"mode,omitempty"`
	// Extra carries PVE parameters the SDK does not model.
	Extra map[string]string `json:"-"`
}

// GetDLBStatus always returns a pverr.ErrUnsupported-wrapped error and never
// issues a request: PVE has no Dynamic Load Balancer REST endpoint — the
// provisional /cluster/ha/lbalancer path this op originally targeted does not
// exist on a real 9.2 cluster (INV-0004 Finding 4). Continuous rebalancing is
// driven through the CRS scheduler settings instead: see GetCRSSettings /
// SetCRSSettings (the crs datacenter option, e.g. ha-rebalance-on-start).
// The signature is kept so a real implementation can land non-breaking if a
// future PVE release adds the endpoint.
func (*Service) GetDLBStatus(_ context.Context) (*DLBStatus, error) {
	return nil, fmt.Errorf(
		"ha.GetDLBStatus: the Dynamic Load Balancer has no PVE REST endpoint; "+
			"use the CRS settings (ha.GetCRSSettings): %w", pverr.ErrUnsupported,
	)
}

// SetDLBConfig always returns a pverr.ErrUnsupported-wrapped error and never
// issues a request — same reclassification as GetDLBStatus; configure the
// scheduler via SetCRSSettings instead.
func (*Service) SetDLBConfig(_ context.Context, _ *DLBConfig) error {
	return fmt.Errorf(
		"ha.SetDLBConfig: the Dynamic Load Balancer has no PVE REST endpoint; "+
			"use the CRS settings (ha.SetCRSSettings): %w", pverr.ErrUnsupported,
	)
}
