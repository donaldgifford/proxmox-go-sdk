package metrics

import (
	"context"
	"fmt"
	"net/http"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/internal/svcutil"
)

// ListMetricServers returns the configured external metric targets.
func (s *Service) ListMetricServers(ctx context.Context) ([]MetricServer, error) {
	var servers []MetricServer
	if err := s.c.DoRequest(ctx, http.MethodGet, metricsServersPath(), nil, &servers); err != nil {
		return nil, fmt.Errorf("metrics.ListMetricServers: %w", err)
	}
	return servers, nil
}

// GetMetricServer returns one metric target by id.
func (s *Service) GetMetricServer(ctx context.Context, id string) (*MetricServer, error) {
	if id == "" {
		return nil, fmt.Errorf("metrics.GetMetricServer: id: %w", svcutil.ErrMissingField)
	}
	var srv MetricServer
	if err := s.c.DoRequest(ctx, http.MethodGet, metricsServerPath(id), nil, &srv); err != nil {
		return nil, fmt.Errorf("metrics.GetMetricServer: %w", err)
	}
	return &srv, nil
}

// CreateMetricServer registers an external metric target. ID, Type, Server, and
// Port are required. The write is synchronous (no task).
func (s *Service) CreateMetricServer(ctx context.Context, spec *MetricServerSpec) error {
	if spec == nil {
		return fmt.Errorf("metrics.CreateMetricServer: %w", svcutil.ErrNilSpec)
	}
	switch {
	case spec.ID == "":
		return fmt.Errorf("metrics.CreateMetricServer: id: %w", svcutil.ErrMissingField)
	case spec.Type == "":
		return fmt.Errorf("metrics.CreateMetricServer: type: %w", svcutil.ErrMissingField)
	case spec.Server == "":
		return fmt.Errorf("metrics.CreateMetricServer: server: %w", svcutil.ErrMissingField)
	}
	body, err := svcutil.EncodeWithExtra(spec, spec.Extra)
	if err != nil {
		return fmt.Errorf("metrics.CreateMetricServer: %w", err)
	}
	if err := s.c.DoRequest(ctx, http.MethodPost, metricsServerPath(spec.ID), body, nil); err != nil {
		return fmt.Errorf("metrics.CreateMetricServer: %w", err)
	}
	return nil
}

// UpdateMetricServer changes a metric target. The write is synchronous (no task).
func (s *Service) UpdateMetricServer(ctx context.Context, id string, update *MetricServerUpdate) error {
	if update == nil {
		return fmt.Errorf("metrics.UpdateMetricServer: %w", svcutil.ErrNilSpec)
	}
	if id == "" {
		return fmt.Errorf("metrics.UpdateMetricServer: id: %w", svcutil.ErrMissingField)
	}
	body, err := svcutil.EncodeWithExtra(update, update.Extra)
	if err != nil {
		return fmt.Errorf("metrics.UpdateMetricServer: %w", err)
	}
	if err := s.c.DoRequest(ctx, http.MethodPut, metricsServerPath(id), body, nil); err != nil {
		return fmt.Errorf("metrics.UpdateMetricServer: %w", err)
	}
	return nil
}

// DeleteMetricServer removes a metric target. The write is synchronous (no task).
func (s *Service) DeleteMetricServer(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("metrics.DeleteMetricServer: id: %w", svcutil.ErrMissingField)
	}
	if err := s.c.DoRequest(ctx, http.MethodDelete, metricsServerPath(id), nil, nil); err != nil {
		return fmt.Errorf("metrics.DeleteMetricServer: %w", err)
	}
	return nil
}
