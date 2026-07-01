package ha

import (
	"context"
	"fmt"
	"net/http"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/internal/svcutil"
)

// ListResources returns every guest under HA management.
func (s *Service) ListResources(ctx context.Context) ([]HAResource, error) {
	var res []HAResource
	if err := s.c.DoRequest(ctx, http.MethodGet, haResourcesPath(), nil, &res); err != nil {
		return nil, fmt.Errorf("ha.ListResources: %w", err)
	}
	return res, nil
}

// GetResource returns one HA resource by SID (e.g. "vm:100").
func (s *Service) GetResource(ctx context.Context, sid string) (*HAResource, error) {
	var res HAResource
	if err := s.c.DoRequest(ctx, http.MethodGet, haResourcePath(sid), nil, &res); err != nil {
		return nil, fmt.Errorf("ha.GetResource: %w", err)
	}
	return &res, nil
}

// AddResource places a guest under HA management. The write is synchronous in
// PVE (no task), so it returns only an error.
func (s *Service) AddResource(ctx context.Context, spec *HAResourceSpec) error {
	if spec == nil {
		return fmt.Errorf("ha.AddResource: %w", svcutil.ErrNilSpec)
	}
	if spec.SID == "" {
		return fmt.Errorf("ha.AddResource: sid: %w", svcutil.ErrMissingField)
	}
	body, err := svcutil.EncodeWithExtra(spec, spec.Extra)
	if err != nil {
		return fmt.Errorf("ha.AddResource: %w", err)
	}
	if err := s.c.DoRequest(ctx, http.MethodPost, haResourcesPath(), body, nil); err != nil {
		return fmt.Errorf("ha.AddResource: %w", err)
	}
	return nil
}

// UpdateResource changes an HA resource's state or restart/relocate limits. The
// write is synchronous (no task).
func (s *Service) UpdateResource(ctx context.Context, sid string, update *HAResourceUpdate) error {
	if update == nil {
		return fmt.Errorf("ha.UpdateResource: %w", svcutil.ErrNilSpec)
	}
	if sid == "" {
		return fmt.Errorf("ha.UpdateResource: sid: %w", svcutil.ErrMissingField)
	}
	body, err := svcutil.EncodeWithExtra(update, update.Extra)
	if err != nil {
		return fmt.Errorf("ha.UpdateResource: %w", err)
	}
	if err := s.c.DoRequest(ctx, http.MethodPut, haResourcePath(sid), body, nil); err != nil {
		return fmt.Errorf("ha.UpdateResource: %w", err)
	}
	return nil
}

// RemoveResource takes a guest out of HA management. The write is synchronous
// (no task); the guest itself is untouched.
func (s *Service) RemoveResource(ctx context.Context, sid string) error {
	if sid == "" {
		return fmt.Errorf("ha.RemoveResource: sid: %w", svcutil.ErrMissingField)
	}
	if err := s.c.DoRequest(ctx, http.MethodDelete, haResourcePath(sid), nil, nil); err != nil {
		return fmt.Errorf("ha.RemoveResource: %w", err)
	}
	return nil
}
