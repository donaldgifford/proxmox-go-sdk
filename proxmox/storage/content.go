package storage

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// contentQuery is the opaque target ListContent's options write to, keeping the
// url.Values query out of the public signature.
type contentQuery struct{ vals url.Values }

// ListContentOption filters a content listing.
type ListContentOption func(*contentQuery)

// WithContentType restricts the listing to one content type ("iso", "images",
// "backup", "vztmpl", "snippets"). Without it, every content type is returned.
func WithContentType(contentType string) ListContentOption {
	return func(q *contentQuery) { q.vals.Set("content", contentType) }
}

// WithVMID restricts the listing to volumes owned by the given guest.
func WithVMID(vmid int) ListContentOption {
	return func(q *contentQuery) { q.vals.Set("vmid", fmt.Sprintf("%d", vmid)) }
}

// ListContent lists the stored objects on a storage
// (GET /nodes/{node}/storage/{storage}/content), optionally filtered.
func (s *Service) ListContent(ctx context.Context, node, storage string, opts ...ListContentOption) ([]Content, error) {
	q := contentQuery{vals: url.Values{}}
	for _, opt := range opts {
		opt(&q)
	}
	path := nodeContentPath(node, storage)
	if enc := q.vals.Encode(); enc != "" {
		path += "?" + enc
	}
	var content []Content
	if err := s.c.DoRequest(ctx, http.MethodGet, path, nil, &content); err != nil {
		return nil, fmt.Errorf("storage.ListContent: %w", err)
	}
	return content, nil
}

// GetVolume returns one stored object by its volid
// (GET /nodes/{node}/storage/{storage}/content/{volid}).
func (s *Service) GetVolume(ctx context.Context, node, storage, volid string) (*Content, error) {
	var c Content
	if err := s.c.DoRequest(ctx, http.MethodGet, nodeVolumePath(node, storage, volid), nil, &c); err != nil {
		return nil, fmt.Errorf("storage.GetVolume: %w", err)
	}
	if c.Volid == "" {
		c.Volid = volid // PVE returns volume attributes without echoing the volid.
	}
	return &c, nil
}
