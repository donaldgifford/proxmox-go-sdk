package access

import (
	"context"
	"fmt"
	"net/http"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/internal/svcutil"
)

// ListGroups returns every group.
func (s *Service) ListGroups(ctx context.Context) ([]Group, error) {
	var groups []Group
	if err := s.c.DoRequest(ctx, http.MethodGet, groupsPath(), nil, &groups); err != nil {
		return nil, fmt.Errorf("access.ListGroups: %w", err)
	}
	return groups, nil
}

// GetGroup returns one group (including its members) by id.
func (s *Service) GetGroup(ctx context.Context, groupid string) (*Group, error) {
	if groupid == "" {
		return nil, fmt.Errorf("access.GetGroup: groupid: %w", svcutil.ErrMissingField)
	}
	var g Group
	if err := s.c.DoRequest(ctx, http.MethodGet, groupPath(groupid), nil, &g); err != nil {
		return nil, fmt.Errorf("access.GetGroup: %w", err)
	}
	return &g, nil
}

// CreateGroup creates a group. The write is synchronous (no task).
func (s *Service) CreateGroup(ctx context.Context, spec *GroupSpec) error {
	if spec == nil {
		return fmt.Errorf("access.CreateGroup: %w", svcutil.ErrNilSpec)
	}
	if spec.GroupID == "" {
		return fmt.Errorf("access.CreateGroup: groupid: %w", svcutil.ErrMissingField)
	}
	body, err := svcutil.EncodeWithExtra(spec, spec.Extra)
	if err != nil {
		return fmt.Errorf("access.CreateGroup: %w", err)
	}
	if err := s.c.DoRequest(ctx, http.MethodPost, groupsPath(), body, nil); err != nil {
		return fmt.Errorf("access.CreateGroup: %w", err)
	}
	return nil
}

// UpdateGroup changes a group's comment. The write is synchronous (no task).
func (s *Service) UpdateGroup(ctx context.Context, groupid string, update *GroupUpdate) error {
	if update == nil {
		return fmt.Errorf("access.UpdateGroup: %w", svcutil.ErrNilSpec)
	}
	if groupid == "" {
		return fmt.Errorf("access.UpdateGroup: groupid: %w", svcutil.ErrMissingField)
	}
	body, err := svcutil.EncodeWithExtra(update, update.Extra)
	if err != nil {
		return fmt.Errorf("access.UpdateGroup: %w", err)
	}
	if err := s.c.DoRequest(ctx, http.MethodPut, groupPath(groupid), body, nil); err != nil {
		return fmt.Errorf("access.UpdateGroup: %w", err)
	}
	return nil
}

// DeleteGroup removes a group. The write is synchronous (no task).
func (s *Service) DeleteGroup(ctx context.Context, groupid string) error {
	if groupid == "" {
		return fmt.Errorf("access.DeleteGroup: groupid: %w", svcutil.ErrMissingField)
	}
	if err := s.c.DoRequest(ctx, http.MethodDelete, groupPath(groupid), nil, nil); err != nil {
		return fmt.Errorf("access.DeleteGroup: %w", err)
	}
	return nil
}
