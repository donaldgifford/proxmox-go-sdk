package ceph

import (
	"context"
	"fmt"
	"net/http"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/internal/svcutil"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/tasks"
)

// ListPools returns the cluster's Ceph pools (queried via node).
func (s *Service) ListPools(ctx context.Context, node string) ([]Pool, error) {
	var pools []Pool
	if err := s.c.DoRequest(ctx, http.MethodGet, nodeCephPoolsPath(node), nil, &pools); err != nil {
		return nil, fmt.Errorf("ceph.ListPools: %w", err)
	}
	return pools, nil
}

// GetPool returns one pool's configuration by name.
func (s *Service) GetPool(ctx context.Context, node, name string) (*Pool, error) {
	if name == "" {
		return nil, fmt.Errorf("ceph.GetPool: name: %w", svcutil.ErrMissingField)
	}
	var p Pool
	if err := s.c.DoRequest(ctx, http.MethodGet, nodeCephPoolPath(node, name), nil, &p); err != nil {
		return nil, fmt.Errorf("ceph.GetPool: %w", err)
	}
	if p.Name == "" {
		p.Name = name
	}
	return &p, nil
}

// CreatePool creates a Ceph pool. Name is required. Pool creation runs as a
// worker; the returned tasks.Ref is awaited for completion.
func (s *Service) CreatePool(ctx context.Context, node string, spec *PoolSpec) (tasks.Ref, error) {
	if spec == nil {
		return tasks.Ref{}, fmt.Errorf("ceph.CreatePool: %w", svcutil.ErrNilSpec)
	}
	if spec.Name == "" {
		return tasks.Ref{}, fmt.Errorf("ceph.CreatePool: name: %w", svcutil.ErrMissingField)
	}
	body, err := svcutil.EncodeWithExtra(spec, spec.Extra)
	if err != nil {
		return tasks.Ref{}, fmt.Errorf("ceph.CreatePool: %w", err)
	}
	var upid string
	if err := s.c.DoRequest(ctx, http.MethodPost, nodeCephPoolsPath(node), body, &upid); err != nil {
		return tasks.Ref{}, fmt.Errorf("ceph.CreatePool: %w", err)
	}
	return svcutil.TaskRef("ceph.CreatePool", upid)
}

// DeletePool destroys a Ceph pool (and its data). It runs as a worker; the
// returned tasks.Ref is awaited for completion.
func (s *Service) DeletePool(ctx context.Context, node, name string) (tasks.Ref, error) {
	if name == "" {
		return tasks.Ref{}, fmt.Errorf("ceph.DeletePool: name: %w", svcutil.ErrMissingField)
	}
	var upid string
	if err := s.c.DoRequest(ctx, http.MethodDelete, nodeCephPoolPath(node, name), nil, &upid); err != nil {
		return tasks.Ref{}, fmt.Errorf("ceph.DeletePool: %w", err)
	}
	return svcutil.TaskRef("ceph.DeletePool", upid)
}
