package qemu

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/tasks"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/types"
)

// Snapshot is one entry from GET /nodes/{node}/qemu/{vmid}/snapshot. PVE also
// returns a synthetic "current" entry representing the live state.
type Snapshot struct {
	Name        string        `json:"name"`
	Description string        `json:"description,omitempty"`
	Parent      string        `json:"parent,omitempty"`
	SnapTime    int64         `json:"snaptime,omitempty"` // Unix seconds.
	VMState     types.PVEBool `json:"vmstate,omitempty"`  // RAM/CPU state captured.
}

// SnapshotSpec is the body of POST /nodes/{node}/qemu/{vmid}/snapshot. Name is
// required. Set VMState to capture the live RAM/CPU state alongside the disks.
//
// Snapshotting a VM's TPM state on file-based storage (NFS/CIFS/directory)
// requires PVE 9.1+; check version.Capabilities.TPMStateSnapshots before relying
// on it. Pass this spec to CreateSnapshot by pointer.
type SnapshotSpec struct {
	Name        string        `json:"snapname"`
	Description string        `json:"description,omitempty"`
	VMState     types.PVEBool `json:"vmstate,omitempty"`
	// Extra carries PVE parameters the SDK does not model.
	Extra map[string]string `json:"-"`
}

// snapConfig is the opaque target the snapshot WithX options write to, keeping
// the form encoding out of the public signatures.
type snapConfig struct{ vals url.Values }

func newSnapConfig() snapConfig { return snapConfig{vals: url.Values{}} }

// RollbackOption configures RollbackSnapshot.
type RollbackOption func(*snapConfig)

// WithStartAfterRollback starts the VM once the rollback completes.
func WithStartAfterRollback() RollbackOption {
	return func(c *snapConfig) { c.vals.Set("start", "1") }
}

// Snapshots lists a VM's snapshots, including the synthetic "current" entry PVE
// appends for the live state.
func (s *Service) Snapshots(ctx context.Context, vmid int) ([]Snapshot, error) {
	var snaps []Snapshot
	if err := s.c.DoRequest(ctx, http.MethodGet, s.vmPath(vmid)+"/snapshot", nil, &snaps); err != nil {
		return nil, fmt.Errorf("qemu.Snapshots: %w", err)
	}
	return snaps, nil
}

// CreateSnapshot takes a snapshot of a VM and returns the snapshot task.
func (s *Service) CreateSnapshot(ctx context.Context, vmid int, spec *SnapshotSpec) (tasks.Ref, error) {
	if spec == nil {
		return tasks.Ref{}, fmt.Errorf("qemu.CreateSnapshot: %w", errNilSpec)
	}
	if spec.Name == "" {
		return tasks.Ref{}, fmt.Errorf("qemu.CreateSnapshot: snapshot name: %w", errMissingField)
	}
	body, err := encodeWithExtra(spec, spec.Extra)
	if err != nil {
		return tasks.Ref{}, fmt.Errorf("qemu.CreateSnapshot: %w", err)
	}
	var upid string
	if derr := s.c.DoRequest(ctx, http.MethodPost, s.vmPath(vmid)+"/snapshot", body, &upid); derr != nil {
		return tasks.Ref{}, fmt.Errorf("qemu.CreateSnapshot: %w", derr)
	}
	return toRef("qemu.CreateSnapshot", upid)
}

// RollbackSnapshot reverts a VM to the named snapshot and returns the rollback
// task. By default the VM is left in the state the snapshot captured; use
// WithStartAfterRollback to start it afterwards.
func (s *Service) RollbackSnapshot(ctx context.Context, vmid int, name string, opts ...RollbackOption) (tasks.Ref, error) {
	if name == "" {
		return tasks.Ref{}, fmt.Errorf("qemu.RollbackSnapshot: snapshot name: %w", errMissingField)
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
	path := s.vmPath(vmid) + "/snapshot/" + name + "/rollback"
	if err := s.c.DoRequest(ctx, http.MethodPost, path, body, &upid); err != nil {
		return tasks.Ref{}, fmt.Errorf("qemu.RollbackSnapshot: %w", err)
	}
	return toRef("qemu.RollbackSnapshot", upid)
}

// DeleteSnapshot removes the named snapshot and returns the deletion task.
func (s *Service) DeleteSnapshot(ctx context.Context, vmid int, name string) (tasks.Ref, error) {
	if name == "" {
		return tasks.Ref{}, fmt.Errorf("qemu.DeleteSnapshot: snapshot name: %w", errMissingField)
	}
	var upid string
	if err := s.c.DoRequest(ctx, http.MethodDelete, s.vmPath(vmid)+"/snapshot/"+name, nil, &upid); err != nil {
		return tasks.Ref{}, fmt.Errorf("qemu.DeleteSnapshot: %w", err)
	}
	return toRef("qemu.DeleteSnapshot", upid)
}
