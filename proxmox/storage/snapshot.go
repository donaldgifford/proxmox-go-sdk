package storage

import (
	"context"
	"fmt"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/pverr"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/tasks"
)

// VolumeSnapshot is one entry from a volume's snapshot listing. A volume-chain
// snapshot is a point-in-time layer in the volume's qcow2 chain. The type is
// retained for a future PVE release that may expose a storage-level snapshot
// endpoint; see VolumeSnapshots for why the ops are currently unsupported.
type VolumeSnapshot struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parent      string `json:"parent,omitempty"` // the snapshot this layer descends from.
	SnapTime    int64  `json:"snaptime,omitempty"`
}

// VolumeSnapshotSpec is the body of a volume-snapshot create. Name is required.
// Pass it by pointer. Retained alongside VolumeSnapshot; see VolumeSnapshots.
type VolumeSnapshotSpec struct {
	Name        string `json:"snapname"`
	Description string `json:"description,omitempty"`
	// Extra carries PVE parameters the SDK does not model.
	Extra map[string]string `json:"-"`
}

// VolumeSnapshots would list a volume's chain snapshots, but PVE exposes no
// storage-level snapshot REST endpoint, so it always returns pverr.ErrUnsupported.
//
// Snapshots-as-volume-chains are a real PVE 9.1 storage capability (see
// version.Capabilities.VolumeChainSnapshots) that brings snapshots to storage
// without native support — thick LVM, Directory, NFS, CIFS — by layering qcow2.
// But the mechanism has no standalone storage endpoint: verified against a live
// 9.2 node, the storage content API stops at /nodes/{node}/storage/{storage}/
// content/{volume} with no .../snapshot child. Snapshots are taken through the
// owning guest — qemu.CreateSnapshot / lxc.CreateSnapshot — and PVE builds the
// volume chain underneath on supported storage. For raw storage-plugin snapshot
// operations on an unattached volume there is no API at all; use the ssh
// side-channel. This mirrors storage.ExpandRAIDZ / ha.ArmHA, which are likewise
// stubbed because no REST endpoint exists.
func (*Service) VolumeSnapshots(_ context.Context, _, _, _ string) ([]VolumeSnapshot, error) {
	return nil, volumeSnapshotUnsupported("VolumeSnapshots")
}

// CreateVolumeSnapshot always returns pverr.ErrUnsupported: PVE has no
// storage-level volume-snapshot REST endpoint. Snapshot the owning guest with
// qemu.CreateSnapshot / lxc.CreateSnapshot instead (see VolumeSnapshots).
func (*Service) CreateVolumeSnapshot(_ context.Context, _, _, _ string, _ *VolumeSnapshotSpec) (tasks.Ref, error) {
	return tasks.Ref{}, volumeSnapshotUnsupported("CreateVolumeSnapshot")
}

// DeleteVolumeSnapshot always returns pverr.ErrUnsupported: PVE has no
// storage-level volume-snapshot REST endpoint. Drop the snapshot through the
// owning guest with qemu.DeleteSnapshot / lxc.DeleteSnapshot instead (see
// VolumeSnapshots).
func (*Service) DeleteVolumeSnapshot(_ context.Context, _, _, _, _ string) (tasks.Ref, error) {
	return tasks.Ref{}, volumeSnapshotUnsupported("DeleteVolumeSnapshot")
}

// volumeSnapshotUnsupported builds the shared ErrUnsupported error, naming the op
// and pointing callers at the guest snapshot API (the path that actually drives
// the 9.1 volume-chain mechanism) and the ssh side-channel.
func volumeSnapshotUnsupported(op string) error {
	return fmt.Errorf(
		"storage.%s: PVE exposes no storage-level volume-snapshot REST endpoint; "+
			"snapshot the owning guest with qemu/lxc.CreateSnapshot (the 9.1 "+
			"volume-chain mechanism runs underneath on supported storage), or use "+
			"the ssh side-channel for raw storage-plugin operations: %w",
		op, pverr.ErrUnsupported,
	)
}
