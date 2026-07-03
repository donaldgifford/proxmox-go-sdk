package lxc

import (
	"context"
	"fmt"
	"net/http"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/internal/svcutil"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/tasks"
)

// List returns the container summary list for the service's node.
func (s *Service) List(ctx context.Context) ([]Container, error) {
	var cts []Container
	if err := s.c.DoRequest(ctx, http.MethodGet, s.lxcPath(), nil, &cts); err != nil {
		return nil, fmt.Errorf("lxc.List: %w", err)
	}
	return cts, nil
}

// Get returns the current runtime status of a container.
func (s *Service) Get(ctx context.Context, vmid int) (*ContainerStatus, error) {
	var st ContainerStatus
	if err := s.c.DoRequest(ctx, http.MethodGet, s.ctPath(vmid)+"/status/current", nil, &st); err != nil {
		return nil, fmt.Errorf("lxc.Get: %w", err)
	}
	return &st, nil
}

// Config returns the full container configuration, with unmodelled keys
// preserved in Config.Extra.
func (s *Service) Config(ctx context.Context, vmid int) (*Config, error) {
	var cfg Config
	if err := s.c.DoRequest(ctx, http.MethodGet, s.ctPath(vmid)+"/config", nil, &cfg); err != nil {
		return nil, fmt.Errorf("lxc.Config: %w", err)
	}
	return &cfg, nil
}

// SetConfig applies a configuration update. PVE answers synchronous changes with
// no task (the returned Ref is the zero value); changes that schedule a worker
// return a Ref the caller awaits with the client's task service.
func (s *Service) SetConfig(ctx context.Context, vmid int, update *ConfigUpdate) (tasks.Ref, error) {
	if update == nil {
		return tasks.Ref{}, fmt.Errorf("lxc.SetConfig: %w", svcutil.ErrNilSpec)
	}
	body, err := svcutil.EncodeWithExtra(update, update.Extra)
	if err != nil {
		return tasks.Ref{}, fmt.Errorf("lxc.SetConfig: %w", err)
	}
	var upid string // stays empty when PVE returns null.
	if derr := s.c.DoRequest(ctx, http.MethodPut, s.ctPath(vmid)+"/config", body, &upid); derr != nil {
		return tasks.Ref{}, fmt.Errorf("lxc.SetConfig: %w", derr)
	}
	if upid == "" {
		return tasks.Ref{}, nil
	}
	return svcutil.TaskRef("lxc.SetConfig", upid)
}

// Create provisions a new container and returns the creation task.
func (s *Service) Create(ctx context.Context, spec *CreateSpec) (tasks.Ref, error) {
	if spec == nil {
		return tasks.Ref{}, fmt.Errorf("lxc.Create: %w", svcutil.ErrNilSpec)
	}
	if spec.OSTemplate == "" {
		return tasks.Ref{}, fmt.Errorf("lxc.Create: ostemplate: %w", svcutil.ErrMissingField)
	}
	body, err := svcutil.EncodeWithExtra(spec, spec.Extra)
	if err != nil {
		return tasks.Ref{}, fmt.Errorf("lxc.Create: %w", err)
	}
	var upid string
	if derr := s.c.DoRequest(ctx, http.MethodPost, s.lxcPath(), body, &upid); derr != nil {
		return tasks.Ref{}, fmt.Errorf("lxc.Create: %w", derr)
	}
	return svcutil.TaskRef("lxc.Create", upid)
}

// Clone clones an existing container into a new one and returns the clone task.
func (s *Service) Clone(ctx context.Context, vmid int, spec *CloneSpec) (tasks.Ref, error) {
	if spec == nil {
		return tasks.Ref{}, fmt.Errorf("lxc.Clone: %w", svcutil.ErrNilSpec)
	}
	body, err := svcutil.EncodeWithExtra(spec, spec.Extra)
	if err != nil {
		return tasks.Ref{}, fmt.Errorf("lxc.Clone: %w", err)
	}
	var upid string
	if derr := s.c.DoRequest(ctx, http.MethodPost, s.ctPath(vmid)+"/clone", body, &upid); derr != nil {
		return tasks.Ref{}, fmt.Errorf("lxc.Clone: %w", derr)
	}
	return svcutil.TaskRef("lxc.Clone", upid)
}

// Delete destroys a container and returns the destroy task.
func (s *Service) Delete(ctx context.Context, vmid int) (tasks.Ref, error) {
	var upid string
	if err := s.c.DoRequest(ctx, http.MethodDelete, s.ctPath(vmid), nil, &upid); err != nil {
		return tasks.Ref{}, fmt.Errorf("lxc.Delete: %w", err)
	}
	return svcutil.TaskRef("lxc.Delete", upid)
}
