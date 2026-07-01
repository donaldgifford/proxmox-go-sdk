package ha

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/internal/svcutil"
)

// CRSSettings is the Cluster Resource Scheduler configuration that governs how
// the HA manager places resources. PVE stores it inside the datacenter options
// as a single compound "crs" property-string
// ("ha=static,ha-rebalance-on-start=1"); the SDK decodes it into typed fields.
type CRSSettings struct {
	// Mode is the scheduler mode: "basic" (count of services) or "static"
	// (static node CPU/memory load). It maps to the crs "ha" sub-key.
	Mode string
	// HARebalanceOnStart rebalances services across nodes when they start,
	// not only on node failure. Maps to the "ha-rebalance-on-start" sub-key.
	HARebalanceOnStart bool
}

// CRSSettingsUpdate is a partial change to the CRS settings. Only the set fields
// are written; the rest of the datacenter options are untouched. Pass it by
// pointer. There is no Extra escape hatch: the write is a single compound "crs"
// property-string, not a flat form body.
type CRSSettingsUpdate struct {
	Mode               string // "" leaves the mode unchanged.
	HARebalanceOnStart *bool  // nil leaves the flag unchanged.
}

// clusterOptionsPayload reads just the "crs" key out of GET /cluster/options.
// Datacenter options carry many keys the HA service does not model; decoding
// into this narrow struct avoids coupling to the full surface.
type clusterOptionsPayload struct {
	CRS string `json:"crs"`
}

// GetCRSSettings reads the Cluster Resource Scheduler configuration from the
// datacenter options.
//
// API-shape caveat: the crs property-string sub-keys ("ha", "ha-rebalance-on-
// start") follow PVE datacenter.cfg convention but are not verified against a
// live 9.x node here.
func (s *Service) GetCRSSettings(ctx context.Context) (*CRSSettings, error) {
	var opts clusterOptionsPayload
	if err := s.c.DoRequest(ctx, http.MethodGet, clusterOptionsPath(), nil, &opts); err != nil {
		return nil, fmt.Errorf("ha.GetCRSSettings: %w", err)
	}
	settings := parseCRSString(opts.CRS)
	return &settings, nil
}

// SetCRSSettings writes a partial CRS change to the datacenter options. The
// write is synchronous (no task).
func (s *Service) SetCRSSettings(ctx context.Context, update *CRSSettingsUpdate) error {
	if update == nil {
		return fmt.Errorf("ha.SetCRSSettings: %w", svcutil.ErrNilSpec)
	}
	crs := encodeCRSString(update)
	if crs == "" {
		return fmt.Errorf("ha.SetCRSSettings: no fields set: %w", svcutil.ErrMissingField)
	}
	body := url.Values{}
	body.Set("crs", crs)
	if err := s.c.DoRequest(ctx, http.MethodPut, clusterOptionsPath(), body, nil); err != nil {
		return fmt.Errorf("ha.SetCRSSettings: %w", err)
	}
	return nil
}

// parseCRSString decodes a PVE crs property-string into CRSSettings. Unknown
// sub-keys are ignored.
func parseCRSString(s string) CRSSettings {
	var out CRSSettings
	for _, part := range strings.Split(s, ",") {
		key, val, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "ha":
			out.Mode = strings.TrimSpace(val)
		case "ha-rebalance-on-start":
			out.HARebalanceOnStart = strings.TrimSpace(val) == "1"
		}
	}
	return out
}

// encodeCRSString builds a PVE crs property-string from the set fields of an
// update, or "" when nothing is set.
func encodeCRSString(u *CRSSettingsUpdate) string {
	var parts []string
	if u.Mode != "" {
		parts = append(parts, "ha="+u.Mode)
	}
	if u.HARebalanceOnStart != nil {
		val := "0"
		if *u.HARebalanceOnStart {
			val = "1"
		}
		parts = append(parts, "ha-rebalance-on-start="+val)
	}
	return strings.Join(parts, ",")
}
