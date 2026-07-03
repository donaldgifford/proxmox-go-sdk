package sdn

import (
	"context"
	"fmt"
	"net/http"
)

// ApplySDN commits the pending SDN configuration cluster-wide
// (PUT /cluster/sdn). Zone, VNet, and subnet changes are staged until applied;
// this activates them across every node. The write is synchronous (no task).
func (s *Service) ApplySDN(ctx context.Context) error {
	if err := s.c.DoRequest(ctx, http.MethodPut, sdnPath(), nil, nil); err != nil {
		return fmt.Errorf("sdn.ApplySDN: %w", err)
	}
	return nil
}
