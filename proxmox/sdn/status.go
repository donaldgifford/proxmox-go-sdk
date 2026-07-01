package sdn

import (
	"context"
	"fmt"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/pverr"
)

// SDNZoneStatus is the runtime status of one SDN zone — per-node reachability
// and the zone's overall health. It is the shape SDNStatus will return once a
// PVE REST endpoint is confirmed; Extra keeps the read lossless meanwhile.
type SDNZoneStatus struct {
	Zone   string
	Status string
	// Extra carries status keys the SDK does not model.
	Extra map[string]string
}

// VNetStatus is the runtime status of one VNet — connected guest NICs and, for
// EVPN zones, learned MAC/IP entries. It is the shape VNetStatus will return
// once a PVE REST endpoint is confirmed.
type VNetStatus struct {
	VNet   string
	Status string
	// Extra carries status keys the SDK does not model.
	Extra map[string]string
}

// SDNStatus enumerates the live status of the SDN zones across the cluster.
//
// API-shape caveat: unlike the SDN *config* endpoints, no aggregate SDN-status
// REST endpoint is confirmed in the 9.x apidoc. Rather than fabricate a path
// that would 404 against a real cluster, SDNStatus returns a
// pverr.ErrUnsupported-wrapped error. The return type is fixed so this becomes a
// real call without a signature change once PVE exposes the endpoint; read
// status from the Proxmox GUI meanwhile.
func (*Service) SDNStatus(_ context.Context) ([]SDNZoneStatus, error) {
	return nil, fmt.Errorf(
		"sdn.SDNStatus: SDN status has no confirmed PVE REST endpoint; "+
			"use the Proxmox GUI: %w", pverr.ErrUnsupported,
	)
}

// VNetStatus returns the live status of one VNet. Same caveat as SDNStatus: no
// VNet-status REST endpoint is confirmed, so it returns pverr.ErrUnsupported
// until one is.
func (*Service) VNetStatus(_ context.Context, _ string) (*VNetStatus, error) {
	return nil, fmt.Errorf(
		"sdn.VNetStatus: VNet status has no confirmed PVE REST endpoint; "+
			"use the Proxmox GUI: %w", pverr.ErrUnsupported,
	)
}
