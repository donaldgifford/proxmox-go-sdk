package cluster

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// ListResources returns the cluster resource inventory. Pass WithResourceType to
// filter to one kind (vm, storage, node, sdn); with no options every resource is
// returned.
func (s *Service) ListResources(ctx context.Context, opts ...ResourceFilter) ([]Resource, error) {
	var q resourceQuery
	for _, opt := range opts {
		opt(&q)
	}
	path := clusterResourcesPath()
	if q.typ != "" {
		path += "?" + url.Values{"type": {string(q.typ)}}.Encode()
	}
	var resources []Resource
	if err := s.c.DoRequest(ctx, http.MethodGet, path, nil, &resources); err != nil {
		return nil, fmt.Errorf("cluster.ListResources: %w", err)
	}
	return resources, nil
}
