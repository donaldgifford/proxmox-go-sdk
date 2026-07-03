package storage

import (
	"context"
	"fmt"
	"net/http"
)

// ListDatastores returns the cluster's storage configuration (GET /storage).
func (s *Service) ListDatastores(ctx context.Context) ([]Datastore, error) {
	var ds []Datastore
	if err := s.c.DoRequest(ctx, http.MethodGet, datastoresPath(), nil, &ds); err != nil {
		return nil, fmt.Errorf("storage.ListDatastores: %w", err)
	}
	return ds, nil
}

// GetDatastore returns the configuration of one storage (GET /storage/{storage}).
func (s *Service) GetDatastore(ctx context.Context, storage string) (*Datastore, error) {
	var d Datastore
	if err := s.c.DoRequest(ctx, http.MethodGet, datastorePath(storage), nil, &d); err != nil {
		return nil, fmt.Errorf("storage.GetDatastore: %w", err)
	}
	return &d, nil
}

// ListNodeStorage returns the activation and usage status of every storage
// visible from node (GET /nodes/{node}/storage).
func (s *Service) ListNodeStorage(ctx context.Context, node string) ([]StorageStatus, error) {
	var st []StorageStatus
	if err := s.c.DoRequest(ctx, http.MethodGet, nodeStoragesPath(node), nil, &st); err != nil {
		return nil, fmt.Errorf("storage.ListNodeStorage: %w", err)
	}
	return st, nil
}

// NodeStorageStatus returns the usage status of one storage on node
// (GET /nodes/{node}/storage/{storage}/status).
func (s *Service) NodeStorageStatus(ctx context.Context, node, storage string) (*StorageStatus, error) {
	var st StorageStatus
	if err := s.c.DoRequest(ctx, http.MethodGet, nodeStoragePath(node, storage)+"/status", nil, &st); err != nil {
		return nil, fmt.Errorf("storage.NodeStorageStatus: %w", err)
	}
	return &st, nil
}
