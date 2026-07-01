package ha

import (
	"context"
	"fmt"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/pverr"
)

// ArmHA arms the HA stack cluster-wide (9.2).
//
// API-shape caveat: PVE 9.2 introduced a cluster-wide HA arm/disarm concept, but
// no dedicated REST endpoint is confirmed in the apidoc — arming is historically
// a datacenter-GUI / pvecm action. Rather than fabricate a path that would 404
// against a real cluster, ArmHA returns a pverr.ErrUnsupported-wrapped error. Use
// the Proxmox GUI or CLI meanwhile; when PVE exposes the endpoint this becomes a
// real call without a signature change. The HAClusterSwitch (9.2) capability
// gate is available for that future implementation.
func (*Service) ArmHA(_ context.Context) error {
	return fmt.Errorf(
		"ha.ArmHA: cluster-wide HA arm has no confirmed PVE REST endpoint; "+
			"use the Proxmox GUI or pvecm: %w", pverr.ErrUnsupported,
	)
}

// DisarmHA disarms the HA stack cluster-wide (9.2). Same caveat as ArmHA: it
// returns pverr.ErrUnsupported until a REST endpoint is confirmed.
func (*Service) DisarmHA(_ context.Context) error {
	return fmt.Errorf(
		"ha.DisarmHA: cluster-wide HA disarm has no confirmed PVE REST endpoint; "+
			"use the Proxmox GUI or pvecm: %w", pverr.ErrUnsupported,
	)
}
