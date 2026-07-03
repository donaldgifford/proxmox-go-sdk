package qemu

import (
	"context"
	"fmt"
	"net/http"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/internal/svcutil"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/tasks"
)

// List returns the VM summary list for the service's node.
func (s *Service) List(ctx context.Context) ([]VM, error) {
	var vms []VM
	if err := s.c.DoRequest(ctx, http.MethodGet, s.qemuPath(), nil, &vms); err != nil {
		return nil, fmt.Errorf("qemu.List: %w", err)
	}
	return vms, nil
}

// Get returns the current runtime status of a VM.
func (s *Service) Get(ctx context.Context, vmid int) (*VMStatus, error) {
	var st VMStatus
	if err := s.c.DoRequest(ctx, http.MethodGet, s.vmPath(vmid)+"/status/current", nil, &st); err != nil {
		return nil, fmt.Errorf("qemu.Get: %w", err)
	}
	return &st, nil
}

// Config returns the full VM configuration, with unmodelled keys preserved in
// Config.Extra.
func (s *Service) Config(ctx context.Context, vmid int) (*Config, error) {
	var cfg Config
	if err := s.c.DoRequest(ctx, http.MethodGet, s.vmPath(vmid)+"/config", nil, &cfg); err != nil {
		return nil, fmt.Errorf("qemu.Config: %w", err)
	}
	return &cfg, nil
}

// SetConfig applies a configuration update. PVE answers synchronous changes with
// no task (the returned Ref is the zero value); changes that schedule a worker
// return a Ref the caller awaits with the client's task service.
func (s *Service) SetConfig(ctx context.Context, vmid int, update *ConfigUpdate) (tasks.Ref, error) {
	if update == nil {
		return tasks.Ref{}, fmt.Errorf("qemu.SetConfig: %w", svcutil.ErrNilSpec)
	}
	body, err := svcutil.EncodeWithExtra(update, update.Extra)
	if err != nil {
		return tasks.Ref{}, fmt.Errorf("qemu.SetConfig: %w", err)
	}
	var upid string // stays empty when PVE returns null.
	if derr := s.c.DoRequest(ctx, http.MethodPut, s.vmPath(vmid)+"/config", body, &upid); derr != nil {
		return tasks.Ref{}, fmt.Errorf("qemu.SetConfig: %w", derr)
	}
	if upid == "" {
		return tasks.Ref{}, nil
	}
	return svcutil.TaskRef("qemu.SetConfig", upid)
}

// Create provisions a new VM and returns the creation task.
func (s *Service) Create(ctx context.Context, spec *CreateSpec) (tasks.Ref, error) {
	if spec == nil {
		return tasks.Ref{}, fmt.Errorf("qemu.Create: %w", svcutil.ErrNilSpec)
	}
	body, err := svcutil.EncodeWithExtra(spec, spec.Extra)
	if err != nil {
		return tasks.Ref{}, fmt.Errorf("qemu.Create: %w", err)
	}
	var upid string
	if derr := s.c.DoRequest(ctx, http.MethodPost, s.qemuPath(), body, &upid); derr != nil {
		return tasks.Ref{}, fmt.Errorf("qemu.Create: %w", derr)
	}
	return svcutil.TaskRef("qemu.Create", upid)
}

// Clone clones an existing VM into a new one and returns the clone task.
func (s *Service) Clone(ctx context.Context, vmid int, spec *CloneSpec) (tasks.Ref, error) {
	if spec == nil {
		return tasks.Ref{}, fmt.Errorf("qemu.Clone: %w", svcutil.ErrNilSpec)
	}
	body, err := svcutil.EncodeWithExtra(spec, spec.Extra)
	if err != nil {
		return tasks.Ref{}, fmt.Errorf("qemu.Clone: %w", err)
	}
	var upid string
	if derr := s.c.DoRequest(ctx, http.MethodPost, s.vmPath(vmid)+"/clone", body, &upid); derr != nil {
		return tasks.Ref{}, fmt.Errorf("qemu.Clone: %w", derr)
	}
	return svcutil.TaskRef("qemu.Clone", upid)
}

// Delete destroys a VM and returns the destroy task.
func (s *Service) Delete(ctx context.Context, vmid int) (tasks.Ref, error) {
	var upid string
	if err := s.c.DoRequest(ctx, http.MethodDelete, s.vmPath(vmid), nil, &upid); err != nil {
		return tasks.Ref{}, fmt.Errorf("qemu.Delete: %w", err)
	}
	return svcutil.TaskRef("qemu.Delete", upid)
}
