package ceph

import (
	"context"
	"fmt"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/pverr"
)

// MirrorMode selects the RBD mirroring granularity (see MirrorSpec).
type MirrorMode string

// The RBD mirroring modes Ceph supports.
const (
	MirrorModePool  MirrorMode = "pool"  // mirror every image in the pool.
	MirrorModeImage MirrorMode = "image" // mirror only explicitly-enabled images.
)

// MirrorStatus is the forward-compatible shape of an RBD mirror status. It is
// defined so GetMirrorStatus has a stable signature; see that method for why it
// currently returns pverr.ErrUnsupported.
type MirrorStatus struct {
	Pool    string            `json:"pool,omitempty"`
	State   string            `json:"state,omitempty"`
	Daemons int               `json:"daemons,omitempty"`
	Extra   map[string]string `json:"-"`
}

// MirrorSpec is the forward-compatible body for enabling RBD mirroring on a
// pool. Pass it by pointer.
type MirrorSpec struct {
	Pool string     `json:"pool"`
	Mode MirrorMode `json:"mode,omitempty"`
	// Extra carries parameters the SDK does not model.
	Extra map[string]string `json:"-"`
}

// GetMirrorStatus would return a pool's RBD-mirroring status.
//
// RBD mirroring is a Ceph-side feature driven by the `rbd mirror` CLI; Proxmox
// VE 9.x (Squid) exposes no confirmed REST endpoint for it. Rather than
// fabricate a /nodes/{node}/ceph/mirror path that would 404 against a real
// cluster, GetMirrorStatus returns a pverr.ErrUnsupported-wrapped error (like
// ha.ArmHA and metrics.GetOTelConfig). Drive mirroring over the SSH side-channel
// (c.SSH().Exec with `rbd mirror`) meanwhile; when PVE exposes the endpoint this
// becomes a real call without a signature change.
func (*Service) GetMirrorStatus(_ context.Context, _, _ string) (*MirrorStatus, error) {
	return nil, fmt.Errorf(
		"ceph.GetMirrorStatus: RBD mirroring has no confirmed PVE REST endpoint; "+
			"use the rbd CLI over SSH: %w", pverr.ErrUnsupported,
	)
}

// EnableMirroring would enable RBD mirroring on a pool. Same caveat as
// GetMirrorStatus: no PVE REST endpoint exists, so it returns
// pverr.ErrUnsupported.
func (*Service) EnableMirroring(_ context.Context, _ string, _ *MirrorSpec) error {
	return fmt.Errorf(
		"ceph.EnableMirroring: RBD mirroring has no confirmed PVE REST endpoint; "+
			"use the rbd CLI over SSH: %w", pverr.ErrUnsupported,
	)
}

// DisableMirroring would disable RBD mirroring on a pool. Same caveat as
// GetMirrorStatus: no PVE REST endpoint exists, so it returns
// pverr.ErrUnsupported.
func (*Service) DisableMirroring(_ context.Context, _, _ string) error {
	return fmt.Errorf(
		"ceph.DisableMirroring: RBD mirroring has no confirmed PVE REST endpoint; "+
			"use the rbd CLI over SSH: %w", pverr.ErrUnsupported,
	)
}
