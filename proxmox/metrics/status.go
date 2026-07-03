package metrics

import (
	"context"
	"fmt"
	"net/http"
)

// GetNodeStatus returns node's live health block (uptime, CPU, memory, kernel
// and PVE versions). Reads are lossless — unmodelled keys land in Extra.
func (s *Service) GetNodeStatus(ctx context.Context, node string) (*NodeStatus, error) {
	if node == "" {
		return nil, fmt.Errorf("metrics.GetNodeStatus: node: %w", errMissingField)
	}
	var st NodeStatus
	if err := s.c.DoRequest(ctx, http.MethodGet, nodeStatusPath(node), nil, &st); err != nil {
		return nil, fmt.Errorf("metrics.GetNodeStatus: %w", err)
	}
	return &st, nil
}
