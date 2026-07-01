package access

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/internal/svcutil"
)

// ListRoles returns every role and its privileges.
func (s *Service) ListRoles(ctx context.Context) ([]Role, error) {
	var roles []Role
	if err := s.c.DoRequest(ctx, http.MethodGet, rolesPath(), nil, &roles); err != nil {
		return nil, fmt.Errorf("access.ListRoles: %w", err)
	}
	return roles, nil
}

// GetRole returns one role's privileges. PVE returns these as a privilege→1
// object for a single role; Role.UnmarshalJSON normalises it to Privs (RoleID is
// not echoed by that endpoint, so it stays empty — the caller already has it).
func (s *Service) GetRole(ctx context.Context, roleid string) (*Role, error) {
	if roleid == "" {
		return nil, fmt.Errorf("access.GetRole: roleid: %w", svcutil.ErrMissingField)
	}
	var r Role
	if err := s.c.DoRequest(ctx, http.MethodGet, rolePath(roleid), nil, &r); err != nil {
		return nil, fmt.Errorf("access.GetRole: %w", err)
	}
	if r.RoleID == "" {
		r.RoleID = roleid
	}
	return &r, nil
}

// CreateRole creates a role with the given privileges. The write is synchronous
// (no task).
func (s *Service) CreateRole(ctx context.Context, spec *RoleSpec) error {
	if spec == nil {
		return fmt.Errorf("access.CreateRole: %w", svcutil.ErrNilSpec)
	}
	if spec.RoleID == "" {
		return fmt.Errorf("access.CreateRole: roleid: %w", svcutil.ErrMissingField)
	}
	body, err := svcutil.EncodeWithExtra(spec, spec.Extra)
	if err != nil {
		return fmt.Errorf("access.CreateRole: %w", err)
	}
	if len(spec.Privs) > 0 {
		body.Set("privs", strings.Join(spec.Privs, ","))
	}
	if err := s.c.DoRequest(ctx, http.MethodPost, rolesPath(), body, nil); err != nil {
		return fmt.Errorf("access.CreateRole: %w", err)
	}
	return nil
}

// UpdateRole sets or appends a role's privileges (set RoleUpdate.Append to add
// rather than replace). The write is synchronous (no task).
func (s *Service) UpdateRole(ctx context.Context, roleid string, update *RoleUpdate) error {
	if update == nil {
		return fmt.Errorf("access.UpdateRole: %w", svcutil.ErrNilSpec)
	}
	if roleid == "" {
		return fmt.Errorf("access.UpdateRole: roleid: %w", svcutil.ErrMissingField)
	}
	body, err := svcutil.EncodeWithExtra(update, update.Extra)
	if err != nil {
		return fmt.Errorf("access.UpdateRole: %w", err)
	}
	if len(update.Privs) > 0 {
		body.Set("privs", strings.Join(update.Privs, ","))
	}
	if err := s.c.DoRequest(ctx, http.MethodPut, rolePath(roleid), body, nil); err != nil {
		return fmt.Errorf("access.UpdateRole: %w", err)
	}
	return nil
}

// DeleteRole removes a role. The write is synchronous (no task).
func (s *Service) DeleteRole(ctx context.Context, roleid string) error {
	if roleid == "" {
		return fmt.Errorf("access.DeleteRole: roleid: %w", svcutil.ErrMissingField)
	}
	if err := s.c.DoRequest(ctx, http.MethodDelete, rolePath(roleid), nil, nil); err != nil {
		return fmt.Errorf("access.DeleteRole: %w", err)
	}
	return nil
}
