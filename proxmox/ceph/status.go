package ceph

import (
	"context"
	"fmt"
	"net/http"
)

// GetStatus returns the live Ceph cluster status (health and maps), queried via
// node. Reads are lossless — unmodelled keys land in Extra.
func (s *Service) GetStatus(ctx context.Context, node string) (*Status, error) {
	var st Status
	if err := s.c.DoRequest(ctx, http.MethodGet, nodeCephStatusPath(node), nil, &st); err != nil {
		return nil, fmt.Errorf("ceph.GetStatus: %w", err)
	}
	return &st, nil
}

// GetClusterConfig returns the cluster's ceph.conf as text (queried via node).
// PVE serves this endpoint as a plain string, so it is returned verbatim rather
// than parsed.
func (s *Service) GetClusterConfig(ctx context.Context, node string) (string, error) {
	var cfg string
	if err := s.c.DoRequest(ctx, http.MethodGet, nodeCephConfigPath(node), nil, &cfg); err != nil {
		return "", fmt.Errorf("ceph.GetClusterConfig: %w", err)
	}
	return cfg, nil
}
