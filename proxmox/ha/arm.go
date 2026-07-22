package ha

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/internal/svcutil"
)

// ResourceMode selects what happens to HA-managed resources while the cluster
// is disarmed (the required resource-mode parameter of DisarmHA; mirrored back
// by the resource_mode field of HAStatusEntry).
type ResourceMode string

const (
	// ResourceModeFreeze preserves resource state: new commands and state
	// changes are not applied while disarmed, and management resumes where it
	// left off on re-arm.
	ResourceModeFreeze ResourceMode = "freeze"
	// ResourceModeIgnore drops resources from HA tracking while disarmed; the
	// CRM ignores them entirely until the cluster is re-armed.
	ResourceModeIgnore ResourceMode = "ignore"
)

// ArmHA arms the HA stack cluster-wide (9.2+): the CRM resumes applying state
// changes and commands across the cluster. The op drives
// POST /cluster/ha/status/arm-ha, which takes no parameters and is synchronous
// (no task). It is gated on the HAClusterSwitch capability — on a pre-9.2
// cluster it returns a pverr.ErrUnsupported-wrapped error before any request
// is issued.
//
// Arming is idempotent on an already-armed cluster. Observe the transition via
// HAStatusCurrent: the armed-state field moves through the ArmedState enum.
func (s *Service) ArmHA(ctx context.Context) error {
	if err := s.caps.Require("HA arm/disarm", "9.2"); err != nil {
		return fmt.Errorf("ha.ArmHA: %w", err)
	}
	if err := s.c.DoRequest(ctx, http.MethodPost, haStatusArmPath(), nil, nil); err != nil {
		return fmt.Errorf("ha.ArmHA: %w", err)
	}
	return nil
}

// DisarmHA disarms the HA stack cluster-wide (9.2+): the CRM stops applying
// state changes until the cluster is re-armed. The op drives
// POST /cluster/ha/status/disarm-ha (synchronous, no task), gated on the
// HAClusterSwitch capability like ArmHA.
//
// mode is required — the PVE API marks resource-mode mandatory, so the caller
// must choose between ResourceModeFreeze (state preserved) and
// ResourceModeIgnore (resources dropped from HA tracking); an empty mode is
// refused client-side. This is a deliberate deviation from DESIGN-0004's
// illustrative optional-parameter sketch, recorded in its Implementation
// Corrections (the apidoc mining post-dated the design).
func (s *Service) DisarmHA(ctx context.Context, mode ResourceMode) error {
	if err := s.caps.Require("HA arm/disarm", "9.2"); err != nil {
		return fmt.Errorf("ha.DisarmHA: %w", err)
	}
	if mode == "" {
		return fmt.Errorf("ha.DisarmHA: resource-mode: %w", svcutil.ErrMissingField)
	}
	body := url.Values{"resource-mode": {string(mode)}}
	if err := s.c.DoRequest(ctx, http.MethodPost, haStatusDisarmPath(), body, nil); err != nil {
		return fmt.Errorf("ha.DisarmHA: %w", err)
	}
	return nil
}
