package access

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/internal/svcutil"
)

// ListUsers returns every user.
func (s *Service) ListUsers(ctx context.Context) ([]User, error) {
	var users []User
	if err := s.c.DoRequest(ctx, http.MethodGet, usersPath(), nil, &users); err != nil {
		return nil, fmt.Errorf("access.ListUsers: %w", err)
	}
	return users, nil
}

// GetUser returns one user by userid (e.g. "alice@pve").
func (s *Service) GetUser(ctx context.Context, userid string) (*User, error) {
	if userid == "" {
		return nil, fmt.Errorf("access.GetUser: userid: %w", svcutil.ErrMissingField)
	}
	var u User
	if err := s.c.DoRequest(ctx, http.MethodGet, userPath(userid), nil, &u); err != nil {
		return nil, fmt.Errorf("access.GetUser: %w", err)
	}
	return &u, nil
}

// CreateUser creates a user. The write is synchronous (no task).
func (s *Service) CreateUser(ctx context.Context, spec *UserSpec) error {
	if spec == nil {
		return fmt.Errorf("access.CreateUser: %w", svcutil.ErrNilSpec)
	}
	if spec.UserID == "" {
		return fmt.Errorf("access.CreateUser: userid: %w", svcutil.ErrMissingField)
	}
	body, err := svcutil.EncodeWithExtra(spec, spec.Extra)
	if err != nil {
		return fmt.Errorf("access.CreateUser: %w", err)
	}
	if len(spec.Groups) > 0 {
		body.Set("groups", strings.Join(spec.Groups, ","))
	}
	if err := s.c.DoRequest(ctx, http.MethodPost, usersPath(), body, nil); err != nil {
		return fmt.Errorf("access.CreateUser: %w", err)
	}
	return nil
}

// UpdateUser changes a user. The write is synchronous (no task).
func (s *Service) UpdateUser(ctx context.Context, userid string, update *UserUpdate) error {
	if update == nil {
		return fmt.Errorf("access.UpdateUser: %w", svcutil.ErrNilSpec)
	}
	if userid == "" {
		return fmt.Errorf("access.UpdateUser: userid: %w", svcutil.ErrMissingField)
	}
	body, err := svcutil.EncodeWithExtra(update, update.Extra)
	if err != nil {
		return fmt.Errorf("access.UpdateUser: %w", err)
	}
	if len(update.Groups) > 0 {
		body.Set("groups", strings.Join(update.Groups, ","))
	}
	if err := s.c.DoRequest(ctx, http.MethodPut, userPath(userid), body, nil); err != nil {
		return fmt.Errorf("access.UpdateUser: %w", err)
	}
	return nil
}

// DeleteUser removes a user. The write is synchronous (no task).
func (s *Service) DeleteUser(ctx context.Context, userid string) error {
	if userid == "" {
		return fmt.Errorf("access.DeleteUser: userid: %w", svcutil.ErrMissingField)
	}
	if err := s.c.DoRequest(ctx, http.MethodDelete, userPath(userid), nil, nil); err != nil {
		return fmt.Errorf("access.DeleteUser: %w", err)
	}
	return nil
}
