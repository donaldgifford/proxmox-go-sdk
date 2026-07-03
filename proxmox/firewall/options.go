package firewall

import (
	"context"
	"fmt"
	"net/http"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/internal/svcutil"
)

// GetOptions returns the scoped firewall options block.
func (s *Service) GetOptions(ctx context.Context) (*Options, error) {
	var o Options
	if err := s.c.DoRequest(ctx, http.MethodGet, s.optionsPath(), nil, &o); err != nil {
		return nil, fmt.Errorf("firewall.GetOptions: %w", err)
	}
	return &o, nil
}

// SetOptions changes the scoped firewall options. The write is synchronous (no
// task).
func (s *Service) SetOptions(ctx context.Context, update *OptionsUpdate) error {
	if update == nil {
		return fmt.Errorf("firewall.SetOptions: %w", svcutil.ErrNilSpec)
	}
	body, err := svcutil.EncodeWithExtra(update, update.Extra)
	if err != nil {
		return fmt.Errorf("firewall.SetOptions: %w", err)
	}
	if err := s.c.DoRequest(ctx, http.MethodPut, s.optionsPath(), body, nil); err != nil {
		return fmt.Errorf("firewall.SetOptions: %w", err)
	}
	return nil
}
