package sdn

import (
	"context"
	"fmt"
	"net/http"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/internal/svcutil"
)

// ListVNets returns every SDN VNet across all zones.
func (s *Service) ListVNets(ctx context.Context) ([]VNet, error) {
	var vnets []VNet
	if err := s.c.DoRequest(ctx, http.MethodGet, sdnVNetsPath(), nil, &vnets); err != nil {
		return nil, fmt.Errorf("sdn.ListVNets: %w", err)
	}
	return vnets, nil
}

// GetVNet returns one VNet by name.
func (s *Service) GetVNet(ctx context.Context, vnet string) (*VNet, error) {
	var v VNet
	if err := s.c.DoRequest(ctx, http.MethodGet, sdnVNetPath(vnet), nil, &v); err != nil {
		return nil, fmt.Errorf("sdn.GetVNet: %w", err)
	}
	return &v, nil
}

// CreateVNet defines a new VNet in a zone. The change is staged into the pending
// config; call ApplySDN to activate it. The write is synchronous (no task).
func (s *Service) CreateVNet(ctx context.Context, spec *VNetSpec) error {
	if spec == nil {
		return fmt.Errorf("sdn.CreateVNet: %w", svcutil.ErrNilSpec)
	}
	switch {
	case spec.VNet == "":
		return fmt.Errorf("sdn.CreateVNet: vnet: %w", svcutil.ErrMissingField)
	case spec.Zone == "":
		return fmt.Errorf("sdn.CreateVNet: zone: %w", svcutil.ErrMissingField)
	}
	body, err := svcutil.EncodeWithExtra(spec, spec.Extra)
	if err != nil {
		return fmt.Errorf("sdn.CreateVNet: %w", err)
	}
	if err := s.c.DoRequest(ctx, http.MethodPost, sdnVNetsPath(), body, nil); err != nil {
		return fmt.Errorf("sdn.CreateVNet: %w", err)
	}
	return nil
}

// UpdateVNet changes a staged VNet. The write is synchronous (no task); call
// ApplySDN to activate it.
func (s *Service) UpdateVNet(ctx context.Context, vnet string, update *VNetUpdate) error {
	if update == nil {
		return fmt.Errorf("sdn.UpdateVNet: %w", svcutil.ErrNilSpec)
	}
	if vnet == "" {
		return fmt.Errorf("sdn.UpdateVNet: vnet: %w", svcutil.ErrMissingField)
	}
	body, err := svcutil.EncodeWithExtra(update, update.Extra)
	if err != nil {
		return fmt.Errorf("sdn.UpdateVNet: %w", err)
	}
	if err := s.c.DoRequest(ctx, http.MethodPut, sdnVNetPath(vnet), body, nil); err != nil {
		return fmt.Errorf("sdn.UpdateVNet: %w", err)
	}
	return nil
}

// DeleteVNet removes a VNet from the pending config. The write is synchronous
// (no task); call ApplySDN to activate it.
func (s *Service) DeleteVNet(ctx context.Context, vnet string) error {
	if vnet == "" {
		return fmt.Errorf("sdn.DeleteVNet: vnet: %w", svcutil.ErrMissingField)
	}
	if err := s.c.DoRequest(ctx, http.MethodDelete, sdnVNetPath(vnet), nil, nil); err != nil {
		return fmt.Errorf("sdn.DeleteVNet: %w", err)
	}
	return nil
}

// ListSubnets returns every subnet defined under a VNet.
func (s *Service) ListSubnets(ctx context.Context, vnet string) ([]Subnet, error) {
	var subnets []Subnet
	if err := s.c.DoRequest(ctx, http.MethodGet, sdnSubnetsPath(vnet), nil, &subnets); err != nil {
		return nil, fmt.Errorf("sdn.ListSubnets: %w", err)
	}
	return subnets, nil
}

// GetSubnet returns one subnet (a CIDR) under a VNet.
func (s *Service) GetSubnet(ctx context.Context, vnet, subnet string) (*Subnet, error) {
	var sn Subnet
	if err := s.c.DoRequest(ctx, http.MethodGet, sdnSubnetPath(vnet, subnet), nil, &sn); err != nil {
		return nil, fmt.Errorf("sdn.GetSubnet: %w", err)
	}
	return &sn, nil
}

// CreateSubnet defines a new subnet under a VNet. The change is staged into the
// pending config; call ApplySDN to activate it. The write is synchronous (no
// task).
func (s *Service) CreateSubnet(ctx context.Context, vnet string, spec *SubnetSpec) error {
	if spec == nil {
		return fmt.Errorf("sdn.CreateSubnet: %w", svcutil.ErrNilSpec)
	}
	switch {
	case vnet == "":
		return fmt.Errorf("sdn.CreateSubnet: vnet: %w", svcutil.ErrMissingField)
	case spec.Subnet == "":
		return fmt.Errorf("sdn.CreateSubnet: subnet: %w", svcutil.ErrMissingField)
	}
	body, err := svcutil.EncodeWithExtra(spec, spec.Extra)
	if err != nil {
		return fmt.Errorf("sdn.CreateSubnet: %w", err)
	}
	if err := s.c.DoRequest(ctx, http.MethodPost, sdnSubnetsPath(vnet), body, nil); err != nil {
		return fmt.Errorf("sdn.CreateSubnet: %w", err)
	}
	return nil
}

// UpdateSubnet changes a staged subnet. The write is synchronous (no task); call
// ApplySDN to activate it.
func (s *Service) UpdateSubnet(ctx context.Context, vnet, subnet string, update *SubnetUpdate) error {
	if update == nil {
		return fmt.Errorf("sdn.UpdateSubnet: %w", svcutil.ErrNilSpec)
	}
	switch {
	case vnet == "":
		return fmt.Errorf("sdn.UpdateSubnet: vnet: %w", svcutil.ErrMissingField)
	case subnet == "":
		return fmt.Errorf("sdn.UpdateSubnet: subnet: %w", svcutil.ErrMissingField)
	}
	body, err := svcutil.EncodeWithExtra(update, update.Extra)
	if err != nil {
		return fmt.Errorf("sdn.UpdateSubnet: %w", err)
	}
	if err := s.c.DoRequest(ctx, http.MethodPut, sdnSubnetPath(vnet, subnet), body, nil); err != nil {
		return fmt.Errorf("sdn.UpdateSubnet: %w", err)
	}
	return nil
}

// DeleteSubnet removes a subnet from the pending config. The write is
// synchronous (no task); call ApplySDN to activate it.
func (s *Service) DeleteSubnet(ctx context.Context, vnet, subnet string) error {
	switch {
	case vnet == "":
		return fmt.Errorf("sdn.DeleteSubnet: vnet: %w", svcutil.ErrMissingField)
	case subnet == "":
		return fmt.Errorf("sdn.DeleteSubnet: subnet: %w", svcutil.ErrMissingField)
	}
	if err := s.c.DoRequest(ctx, http.MethodDelete, sdnSubnetPath(vnet, subnet), nil, nil); err != nil {
		return fmt.Errorf("sdn.DeleteSubnet: %w", err)
	}
	return nil
}
