package storage

import (
	"context"
	"fmt"
	"net/http"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/internal/svcutil"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/tasks"
)

// VolumeSnapshot is one entry from the volume's snapshot listing. A volume-chain
// snapshot is a point-in-time layer in the volume's qcow2 chain.
type VolumeSnapshot struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parent      string `json:"parent,omitempty"` // the snapshot this layer descends from.
	SnapTime    int64  `json:"snaptime,omitempty"`
}

// VolumeSnapshotSpec is the body of a volume-snapshot create. Name is required.
// Pass it by pointer.
type VolumeSnapshotSpec struct {
	Name        string `json:"snapname"`
	Description string `json:"description,omitempty"`
	// Extra carries PVE parameters the SDK does not model.
	Extra map[string]string `json:"-"`
}

// VolumeSnapshots lists a volume's chain snapshots.
//
// Snapshots-as-volume-chains are a PVE 9.1 capability (see
// version.Capabilities.VolumeChainSnapshots) that brings snapshots to storage
// without native support — thick LVM, Directory, NFS, CIFS — by layering qcow2.
// Storage with native snapshots (ZFS, btrfs, LVM-thin) does not need it.
//
// API-shape caveat: the standalone storage-level volume-snapshot endpoint is not
// confirmed against a live 9.x node (no apidoc ships in this repo). The version
// gate is firm; the request path may need adjustment once a node is reachable.
func (s *Service) VolumeSnapshots(ctx context.Context, node, storage, volid string) ([]VolumeSnapshot, error) {
	if err := s.caps.Require("volume-chain snapshots", "9.1"); err != nil {
		return nil, fmt.Errorf("storage.VolumeSnapshots: %w", err)
	}
	var snaps []VolumeSnapshot
	if err := s.c.DoRequest(ctx, http.MethodGet, volumeSnapshotsPath(node, storage, volid), nil, &snaps); err != nil {
		return nil, fmt.Errorf("storage.VolumeSnapshots: %w", err)
	}
	return snaps, nil
}

// CreateVolumeSnapshot snapshots a volume and returns the snapshot task. Gated
// on the 9.1 VolumeChainSnapshots capability (see VolumeSnapshots).
func (s *Service) CreateVolumeSnapshot(ctx context.Context, node, storage, volid string, spec *VolumeSnapshotSpec) (tasks.Ref, error) {
	if spec == nil {
		return tasks.Ref{}, fmt.Errorf("storage.CreateVolumeSnapshot: %w", svcutil.ErrNilSpec)
	}
	if err := s.caps.Require("volume-chain snapshots", "9.1"); err != nil {
		return tasks.Ref{}, fmt.Errorf("storage.CreateVolumeSnapshot: %w", err)
	}
	if spec.Name == "" {
		return tasks.Ref{}, fmt.Errorf("storage.CreateVolumeSnapshot: snapshot name: %w", svcutil.ErrMissingField)
	}
	body, err := svcutil.EncodeWithExtra(spec, spec.Extra)
	if err != nil {
		return tasks.Ref{}, fmt.Errorf("storage.CreateVolumeSnapshot: %w", err)
	}
	var upid string
	if derr := s.c.DoRequest(ctx, http.MethodPost, volumeSnapshotsPath(node, storage, volid), body, &upid); derr != nil {
		return tasks.Ref{}, fmt.Errorf("storage.CreateVolumeSnapshot: %w", derr)
	}
	return svcutil.TaskRef("storage.CreateVolumeSnapshot", upid)
}

// DeleteVolumeSnapshot removes a volume-chain snapshot and returns the deletion
// task. Gated on the 9.1 VolumeChainSnapshots capability (see VolumeSnapshots).
func (s *Service) DeleteVolumeSnapshot(ctx context.Context, node, storage, volid, snapname string) (tasks.Ref, error) {
	if err := s.caps.Require("volume-chain snapshots", "9.1"); err != nil {
		return tasks.Ref{}, fmt.Errorf("storage.DeleteVolumeSnapshot: %w", err)
	}
	if snapname == "" {
		return tasks.Ref{}, fmt.Errorf("storage.DeleteVolumeSnapshot: snapshot name: %w", svcutil.ErrMissingField)
	}
	var upid string
	if err := s.c.DoRequest(ctx, http.MethodDelete, volumeSnapshotPath(node, storage, volid, snapname), nil, &upid); err != nil {
		return tasks.Ref{}, fmt.Errorf("storage.DeleteVolumeSnapshot: %w", err)
	}
	return svcutil.TaskRef("storage.DeleteVolumeSnapshot", upid)
}
