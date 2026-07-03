package cluster

import (
	"context"
	"fmt"
	"net/http"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/internal/svcutil"
)

// GetOptions returns the datacenter options block.
func (s *Service) GetOptions(ctx context.Context) (*Options, error) {
	var o Options
	if err := s.c.DoRequest(ctx, http.MethodGet, clusterOptionsPath(), nil, &o); err != nil {
		return nil, fmt.Errorf("cluster.GetOptions: %w", err)
	}
	return &o, nil
}

// SetOptions changes the datacenter options. The write is synchronous (no task).
func (s *Service) SetOptions(ctx context.Context, update *OptionsUpdate) error {
	if update == nil {
		return fmt.Errorf("cluster.SetOptions: %w", svcutil.ErrNilSpec)
	}
	body, err := svcutil.EncodeWithExtra(update, update.Extra)
	if err != nil {
		return fmt.Errorf("cluster.SetOptions: %w", err)
	}
	if err := s.c.DoRequest(ctx, http.MethodPut, clusterOptionsPath(), body, nil); err != nil {
		return fmt.Errorf("cluster.SetOptions: %w", err)
	}
	return nil
}
