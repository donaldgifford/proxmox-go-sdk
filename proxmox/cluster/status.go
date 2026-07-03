package cluster

import (
	"context"
	"fmt"
	"net/http"
)

// GetStatus returns the /cluster/status entries: one entry with Type "cluster"
// (name, member count, quorum) and one Type "node" entry per member.
func (s *Service) GetStatus(ctx context.Context) ([]StatusEntry, error) {
	var entries []StatusEntry
	if err := s.c.DoRequest(ctx, http.MethodGet, clusterStatusPath(), nil, &entries); err != nil {
		return nil, fmt.Errorf("cluster.GetStatus: %w", err)
	}
	return entries, nil
}
