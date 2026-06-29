package lxc

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/internal/svcutil"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/tasks"
)

// Snapshot is one entry from GET /nodes/{node}/lxc/{vmid}/snapshot. PVE also
// returns a synthetic "current" entry representing the live state. Unlike a VM
// snapshot there is no captured RAM/CPU state — a container snapshot is purely a
// point-in-time copy of the rootfs and mount-point volumes.
type Snapshot struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parent      string `json:"parent,omitempty"`
	SnapTime    int64  `json:"snaptime,omitempty"` // Unix seconds.
}

// SnapshotSpec is the body of POST /nodes/{node}/lxc/{vmid}/snapshot. Name is
// required. Pass this spec to CreateSnapshot by pointer.
//
// Container snapshots require a backing store that supports them — ZFS, btrfs,
// or LVM-thin. On directory or plain-LVM storage PVE rejects the request and the
// SDK surfaces the error through the pverr taxonomy; the SDK cannot pre-validate
// the backing store, so the constraint is enforced server-side.
type SnapshotSpec struct {
	Name        string `json:"snapname"`
	Description string `json:"description,omitempty"`
	// Extra carries PVE parameters the SDK does not model.
	Extra map[string]string `json:"-"`
}

// snapConfig is the opaque target the snapshot WithX options write to, keeping
// the form encoding out of the public signatures.
type snapConfig struct{ vals url.Values }

func newSnapConfig() snapConfig { return snapConfig{vals: url.Values{}} }

// RollbackOption configures RollbackSnapshot.
type RollbackOption func(*snapConfig)

// WithStartAfterRollback starts the container once the rollback completes.
func WithStartAfterRollback() RollbackOption {
	return func(c *snapConfig) { c.vals.Set("start", "1") }
}

// DeleteSnapshotOption configures DeleteSnapshot.
type DeleteSnapshotOption func(*snapConfig)

// WithForceDeleteSnapshot removes the snapshot from the container config even if
// freeing its backing volume fails — useful when the snapshot's backing store is
// already gone.
func WithForceDeleteSnapshot() DeleteSnapshotOption {
	return func(c *snapConfig) { c.vals.Set("force", "1") }
}

// Snapshots lists a container's snapshots, including the synthetic "current"
// entry PVE appends for the live state.
func (s *Service) Snapshots(ctx context.Context, vmid int) ([]Snapshot, error) {
	var snaps []Snapshot
	if err := s.c.DoRequest(ctx, http.MethodGet, s.ctPath(vmid)+"/snapshot", nil, &snaps); err != nil {
		return nil, fmt.Errorf("lxc.Snapshots: %w", err)
	}
	return snaps, nil
}

// CreateSnapshot takes a snapshot of a container and returns the snapshot task.
// The container's rootfs must live on a snapshot-capable backing store (see
// SnapshotSpec).
func (s *Service) CreateSnapshot(ctx context.Context, vmid int, spec *SnapshotSpec) (tasks.Ref, error) {
	if spec == nil {
		return tasks.Ref{}, fmt.Errorf("lxc.CreateSnapshot: %w", svcutil.ErrNilSpec)
	}
	if spec.Name == "" {
		return tasks.Ref{}, fmt.Errorf("lxc.CreateSnapshot: snapshot name: %w", svcutil.ErrMissingField)
	}
	body, err := svcutil.EncodeWithExtra(spec, spec.Extra)
	if err != nil {
		return tasks.Ref{}, fmt.Errorf("lxc.CreateSnapshot: %w", err)
	}
	var upid string
	if derr := s.c.DoRequest(ctx, http.MethodPost, s.ctPath(vmid)+"/snapshot", body, &upid); derr != nil {
		return tasks.Ref{}, fmt.Errorf("lxc.CreateSnapshot: %w", derr)
	}
	return svcutil.TaskRef("lxc.CreateSnapshot", upid)
}

// RollbackSnapshot reverts a container to the named snapshot and returns the
// rollback task. By default the container is left stopped; use
// WithStartAfterRollback to start it afterwards.
func (s *Service) RollbackSnapshot(ctx context.Context, vmid int, name string, opts ...RollbackOption) (tasks.Ref, error) {
	if name == "" {
		return tasks.Ref{}, fmt.Errorf("lxc.RollbackSnapshot: snapshot name: %w", svcutil.ErrMissingField)
	}
	cfg := newSnapConfig()
	for _, opt := range opts {
		opt(&cfg)
	}
	var body any
	if len(cfg.vals) > 0 {
		body = cfg.vals
	}
	var upid string
	path := s.ctPath(vmid) + "/snapshot/" + name + "/rollback"
	if err := s.c.DoRequest(ctx, http.MethodPost, path, body, &upid); err != nil {
		return tasks.Ref{}, fmt.Errorf("lxc.RollbackSnapshot: %w", err)
	}
	return svcutil.TaskRef("lxc.RollbackSnapshot", upid)
}

// DeleteSnapshot removes the named snapshot and returns the deletion task. Use
// WithForceDeleteSnapshot to drop a snapshot whose backing volume is already
// gone.
func (s *Service) DeleteSnapshot(ctx context.Context, vmid int, name string, opts ...DeleteSnapshotOption) (tasks.Ref, error) {
	if name == "" {
		return tasks.Ref{}, fmt.Errorf("lxc.DeleteSnapshot: snapshot name: %w", svcutil.ErrMissingField)
	}
	cfg := newSnapConfig()
	for _, opt := range opts {
		opt(&cfg)
	}
	var body any
	if len(cfg.vals) > 0 {
		body = cfg.vals
	}
	var upid string
	if err := s.c.DoRequest(ctx, http.MethodDelete, s.ctPath(vmid)+"/snapshot/"+name, body, &upid); err != nil {
		return tasks.Ref{}, fmt.Errorf("lxc.DeleteSnapshot: %w", err)
	}
	return svcutil.TaskRef("lxc.DeleteSnapshot", upid)
}
