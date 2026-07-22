package ha

import (
	"context"
	"fmt"
	"net/http"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/pverr"
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

// DisarmHA disarms the HA stack cluster-wide (9.2). Same caveat as ArmHA once
// had: it returns pverr.ErrUnsupported until the disarm op lands (task 2 of
// IMPL-0005 replaces this stub with the real POST …/disarm-ha).
func (*Service) DisarmHA(_ context.Context) error {
	return fmt.Errorf(
		"ha.DisarmHA: cluster-wide HA disarm has no confirmed PVE REST endpoint; "+
			"use the Proxmox GUI or pvecm: %w", pverr.ErrUnsupported,
	)
}
