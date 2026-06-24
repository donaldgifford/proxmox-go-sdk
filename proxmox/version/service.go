package version

import (
	"context"
	"fmt"
	"net/http"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/api"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/pverr"
)

// ProxmoxVersion is the raw payload of GET /version.
type ProxmoxVersion struct {
	// Version is the full release, e.g. "9.0.3".
	Version string `json:"version"`
	// Release is the major.minor line, e.g. "9.0".
	Release string `json:"release"`
	// RepoID identifies the package repository build.
	RepoID string `json:"repoid"`
	// Console is the configured console viewer (e.g. "xtermjs"); optional.
	Console string `json:"console,omitempty"`
}

// Service reads PVE version information over an api.Client.
type Service struct {
	c api.Client
}

// NewService returns a version Service bound to c.
func NewService(c api.Client) *Service {
	return &Service{c: c}
}

// Get fetches GET /version.
func (s *Service) Get(ctx context.Context) (ProxmoxVersion, error) {
	var v ProxmoxVersion
	if err := s.c.DoRequest(ctx, http.MethodGet, "/version", nil, &v); err != nil {
		return ProxmoxVersion{}, err
	}
	return v, nil
}

// Capabilities fetches /version and parses it into a Capabilities snapshot,
// rejecting any release below MinimumProxmoxVersion with pverr.ErrUnsupported.
// NewClient calls this once at construction.
func (s *Service) Capabilities(ctx context.Context) (Capabilities, error) {
	v, err := s.Get(ctx)
	if err != nil {
		return Capabilities{}, err
	}
	caps, err := Parse(v.Version)
	if err != nil {
		return Capabilities{}, err
	}
	if !caps.MeetsMinimum() {
		return Capabilities{}, fmt.Errorf(
			"proxmox %s is below the supported minimum %s: %w",
			caps, MinimumProxmoxVersion, pverr.ErrUnsupported,
		)
	}
	return caps, nil
}
