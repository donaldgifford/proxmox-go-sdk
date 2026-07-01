package access

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/internal/svcutil"
)

// ListACLs returns every ACL entry.
func (s *Service) ListACLs(ctx context.Context) ([]ACLEntry, error) {
	var acls []ACLEntry
	if err := s.c.DoRequest(ctx, http.MethodGet, aclPath(), nil, &acls); err != nil {
		return nil, fmt.Errorf("access.ListACLs: %w", err)
	}
	return acls, nil
}

// SetACL grants the spec's Roles on Path to its Users/Groups/Tokens, or revokes
// them when spec.Delete is set. PVE models both as PUT /access/acl. The write is
// synchronous (no task).
func (s *Service) SetACL(ctx context.Context, spec *ACLSpec) error {
	if spec == nil {
		return fmt.Errorf("access.SetACL: %w", svcutil.ErrNilSpec)
	}
	switch {
	case spec.Path == "":
		return fmt.Errorf("access.SetACL: path: %w", svcutil.ErrMissingField)
	case len(spec.Roles) == 0:
		return fmt.Errorf("access.SetACL: roles: %w", svcutil.ErrMissingField)
	case len(spec.Users) == 0 && len(spec.Groups) == 0 && len(spec.Tokens) == 0:
		return fmt.Errorf("access.SetACL: one of users/groups/tokens: %w", svcutil.ErrMissingField)
	}
	body, err := svcutil.EncodeWithExtra(spec, spec.Extra)
	if err != nil {
		return fmt.Errorf("access.SetACL: %w", err)
	}
	body.Set("roles", strings.Join(spec.Roles, ","))
	if len(spec.Users) > 0 {
		body.Set("users", strings.Join(spec.Users, ","))
	}
	if len(spec.Groups) > 0 {
		body.Set("groups", strings.Join(spec.Groups, ","))
	}
	if len(spec.Tokens) > 0 {
		body.Set("tokens", strings.Join(spec.Tokens, ","))
	}
	if err := s.c.DoRequest(ctx, http.MethodPut, aclPath(), body, nil); err != nil {
		return fmt.Errorf("access.SetACL: %w", err)
	}
	return nil
}
