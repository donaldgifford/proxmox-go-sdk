package sdn

import (
	"context"
	"fmt"
	"net/http"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/internal/svcutil"
)

// ListZones returns every SDN zone.
func (s *Service) ListZones(ctx context.Context) ([]Zone, error) {
	var zones []Zone
	if err := s.c.DoRequest(ctx, http.MethodGet, sdnZonesPath(), nil, &zones); err != nil {
		return nil, fmt.Errorf("sdn.ListZones: %w", err)
	}
	return zones, nil
}

// GetZone returns one zone by name.
func (s *Service) GetZone(ctx context.Context, zone string) (*Zone, error) {
	var z Zone
	if err := s.c.DoRequest(ctx, http.MethodGet, sdnZonePath(zone), nil, &z); err != nil {
		return nil, fmt.Errorf("sdn.GetZone: %w", err)
	}
	return &z, nil
}

// CreateZone defines a new SDN zone. The change is staged into the pending
// config; call ApplySDN to activate it. The write is synchronous (no task).
func (s *Service) CreateZone(ctx context.Context, spec *ZoneSpec) error {
	if spec == nil {
		return fmt.Errorf("sdn.CreateZone: %w", svcutil.ErrNilSpec)
	}
	switch {
	case spec.Zone == "":
		return fmt.Errorf("sdn.CreateZone: zone: %w", svcutil.ErrMissingField)
	case spec.Type == "":
		return fmt.Errorf("sdn.CreateZone: type: %w", svcutil.ErrMissingField)
	}
	body, err := svcutil.EncodeWithExtra(spec, spec.Extra)
	if err != nil {
		return fmt.Errorf("sdn.CreateZone: %w", err)
	}
	if err := s.c.DoRequest(ctx, http.MethodPost, sdnZonesPath(), body, nil); err != nil {
		return fmt.Errorf("sdn.CreateZone: %w", err)
	}
	return nil
}

// UpdateZone changes a staged zone. The write is synchronous (no task); call
// ApplySDN to activate it.
func (s *Service) UpdateZone(ctx context.Context, zone string, update *ZoneUpdate) error {
	if update == nil {
		return fmt.Errorf("sdn.UpdateZone: %w", svcutil.ErrNilSpec)
	}
	if zone == "" {
		return fmt.Errorf("sdn.UpdateZone: zone: %w", svcutil.ErrMissingField)
	}
	body, err := svcutil.EncodeWithExtra(update, update.Extra)
	if err != nil {
		return fmt.Errorf("sdn.UpdateZone: %w", err)
	}
	if err := s.c.DoRequest(ctx, http.MethodPut, sdnZonePath(zone), body, nil); err != nil {
		return fmt.Errorf("sdn.UpdateZone: %w", err)
	}
	return nil
}

// DeleteZone removes a zone from the pending config. The write is synchronous
// (no task); call ApplySDN to activate it.
func (s *Service) DeleteZone(ctx context.Context, zone string) error {
	if zone == "" {
		return fmt.Errorf("sdn.DeleteZone: zone: %w", svcutil.ErrMissingField)
	}
	if err := s.c.DoRequest(ctx, http.MethodDelete, sdnZonePath(zone), nil, nil); err != nil {
		return fmt.Errorf("sdn.DeleteZone: %w", err)
	}
	return nil
}
