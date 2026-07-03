package nodes

import (
	"context"
	"fmt"
	"net/http"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/internal/svcutil"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/tasks"
)

// ListInterfaces returns node's configured network interfaces.
func (s *Service) ListInterfaces(ctx context.Context, node string) ([]Interface, error) {
	var ifaces []Interface
	if err := s.c.DoRequest(ctx, http.MethodGet, nodeNetworkPath(node), nil, &ifaces); err != nil {
		return nil, fmt.Errorf("nodes.ListInterfaces: %w", err)
	}
	return ifaces, nil
}

// GetInterface returns one interface by name (e.g. "vmbr0").
func (s *Service) GetInterface(ctx context.Context, node, iface string) (*Interface, error) {
	var i Interface
	if err := s.c.DoRequest(ctx, http.MethodGet, nodeIfacePath(node, iface), nil, &i); err != nil {
		return nil, fmt.Errorf("nodes.GetInterface: %w", err)
	}
	return &i, nil
}

// CreateInterface stages a new interface into node's pending network config.
// Call ApplyNetworkConfig to activate it. The write is synchronous (no task).
func (s *Service) CreateInterface(ctx context.Context, node string, spec *InterfaceSpec) error {
	if spec == nil {
		return fmt.Errorf("nodes.CreateInterface: %w", svcutil.ErrNilSpec)
	}
	switch {
	case spec.Iface == "":
		return fmt.Errorf("nodes.CreateInterface: iface: %w", svcutil.ErrMissingField)
	case spec.Type == "":
		return fmt.Errorf("nodes.CreateInterface: type: %w", svcutil.ErrMissingField)
	}
	body, err := svcutil.EncodeWithExtra(spec, spec.Extra)
	if err != nil {
		return fmt.Errorf("nodes.CreateInterface: %w", err)
	}
	if err := s.c.DoRequest(ctx, http.MethodPost, nodeNetworkPath(node), body, nil); err != nil {
		return fmt.Errorf("nodes.CreateInterface: %w", err)
	}
	return nil
}

// UpdateInterface changes a staged interface. The write is synchronous (no
// task); call ApplyNetworkConfig to activate it.
func (s *Service) UpdateInterface(ctx context.Context, node, iface string, update *InterfaceUpdate) error {
	if update == nil {
		return fmt.Errorf("nodes.UpdateInterface: %w", svcutil.ErrNilSpec)
	}
	if iface == "" {
		return fmt.Errorf("nodes.UpdateInterface: iface: %w", svcutil.ErrMissingField)
	}
	body, err := svcutil.EncodeWithExtra(update, update.Extra)
	if err != nil {
		return fmt.Errorf("nodes.UpdateInterface: %w", err)
	}
	if err := s.c.DoRequest(ctx, http.MethodPut, nodeIfacePath(node, iface), body, nil); err != nil {
		return fmt.Errorf("nodes.UpdateInterface: %w", err)
	}
	return nil
}

// DeleteInterface removes an interface from the pending network config. The
// write is synchronous (no task); call ApplyNetworkConfig to activate it.
func (s *Service) DeleteInterface(ctx context.Context, node, iface string) error {
	if iface == "" {
		return fmt.Errorf("nodes.DeleteInterface: iface: %w", svcutil.ErrMissingField)
	}
	if err := s.c.DoRequest(ctx, http.MethodDelete, nodeIfacePath(node, iface), nil, nil); err != nil {
		return fmt.Errorf("nodes.DeleteInterface: %w", err)
	}
	return nil
}

// ApplyNetworkConfig writes node's pending network changes to the live config
// (PUT /nodes/{node}/network). PVE may answer either synchronously (no task) or
// with a reload worker; when a worker is started the returned tasks.Ref is
// non-zero and the caller awaits it, otherwise the Ref is zero (check
// tasks.Ref.IsZero).
func (s *Service) ApplyNetworkConfig(ctx context.Context, node string) (tasks.Ref, error) {
	var upid *string
	if err := s.c.DoRequest(ctx, http.MethodPut, nodeNetworkPath(node), nil, &upid); err != nil {
		return tasks.Ref{}, fmt.Errorf("nodes.ApplyNetworkConfig: %w", err)
	}
	if upid == nil || *upid == "" {
		return tasks.Ref{}, nil // synchronous apply: no task to await.
	}
	return svcutil.TaskRef("nodes.ApplyNetworkConfig", *upid)
}
