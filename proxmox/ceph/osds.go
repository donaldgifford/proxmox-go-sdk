package ceph

import (
	"context"
	"fmt"
	"net/http"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/internal/svcutil"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/tasks"
)

// ListOSDs returns the CRUSH/OSD tree (queried via node).
func (s *Service) ListOSDs(ctx context.Context, node string) (*OSDTree, error) {
	var tree OSDTree
	if err := s.c.DoRequest(ctx, http.MethodGet, nodeCephOSDsPath(node), nil, &tree); err != nil {
		return nil, fmt.Errorf("ceph.ListOSDs: %w", err)
	}
	return &tree, nil
}

// CreateOSD creates an OSD on a block device (OSDSpec.DevPath). It runs as a
// worker; the returned tasks.Ref is awaited for completion.
func (s *Service) CreateOSD(ctx context.Context, node string, spec *OSDSpec) (tasks.Ref, error) {
	if spec == nil {
		return tasks.Ref{}, fmt.Errorf("ceph.CreateOSD: %w", svcutil.ErrNilSpec)
	}
	if spec.DevPath == "" {
		return tasks.Ref{}, fmt.Errorf("ceph.CreateOSD: dev: %w", svcutil.ErrMissingField)
	}
	body, err := svcutil.EncodeWithExtra(spec, spec.Extra)
	if err != nil {
		return tasks.Ref{}, fmt.Errorf("ceph.CreateOSD: %w", err)
	}
	var upid string
	if err := s.c.DoRequest(ctx, http.MethodPost, nodeCephOSDsPath(node), body, &upid); err != nil {
		return tasks.Ref{}, fmt.Errorf("ceph.CreateOSD: %w", err)
	}
	return svcutil.TaskRef("ceph.CreateOSD", upid)
}

// DestroyOSD removes an OSD by id. It runs as a worker; the returned tasks.Ref
// is awaited for completion.
func (s *Service) DestroyOSD(ctx context.Context, node string, osdID int) (tasks.Ref, error) {
	var upid string
	if err := s.c.DoRequest(ctx, http.MethodDelete, nodeCephOSDPath(node, osdID), nil, &upid); err != nil {
		return tasks.Ref{}, fmt.Errorf("ceph.DestroyOSD: %w", err)
	}
	return svcutil.TaskRef("ceph.DestroyOSD", upid)
}
