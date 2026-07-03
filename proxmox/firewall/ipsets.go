package firewall

import (
	"context"
	"fmt"
	"net/http"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/internal/svcutil"
)

// ListIPSets returns the scoped IPSets (names + comments), without their
// entries; use ListIPSetEntries for a set's contents.
func (s *Service) ListIPSets(ctx context.Context) ([]IPSet, error) {
	var sets []IPSet
	if err := s.c.DoRequest(ctx, http.MethodGet, s.ipsetsPath(), nil, &sets); err != nil {
		return nil, fmt.Errorf("firewall.ListIPSets: %w", err)
	}
	return sets, nil
}

// CreateIPSet defines a new empty IPSet. The write is synchronous (no task).
func (s *Service) CreateIPSet(ctx context.Context, spec *IPSetSpec) error {
	if spec == nil {
		return fmt.Errorf("firewall.CreateIPSet: %w", svcutil.ErrNilSpec)
	}
	if spec.Name == "" {
		return fmt.Errorf("firewall.CreateIPSet: name: %w", svcutil.ErrMissingField)
	}
	body, err := svcutil.EncodeWithExtra(spec, spec.Extra)
	if err != nil {
		return fmt.Errorf("firewall.CreateIPSet: %w", err)
	}
	if err := s.c.DoRequest(ctx, http.MethodPost, s.ipsetsPath(), body, nil); err != nil {
		return fmt.Errorf("firewall.CreateIPSet: %w", err)
	}
	return nil
}

// RenameIPSet renames an IPSet (PVE renames by POSTing the new name to the
// collection with a "rename" of the old name). It requires PVE 9.1, where the
// firewall gained overlapping-IPSet support; below that this returns a
// pverr.ErrUnsupported-wrapped error before making any request. The write is
// synchronous (no task).
func (s *Service) RenameIPSet(ctx context.Context, name, newName string) error {
	switch {
	case name == "":
		return fmt.Errorf("firewall.RenameIPSet: name: %w", svcutil.ErrMissingField)
	case newName == "":
		return fmt.Errorf("firewall.RenameIPSet: newName: %w", svcutil.ErrMissingField)
	}
	if err := s.caps.Require("firewall IPSet rename", "9.1"); err != nil {
		return fmt.Errorf("firewall.RenameIPSet: %w", err)
	}
	body := map[string]string{"name": newName, "rename": name}
	if err := s.c.DoRequest(ctx, http.MethodPost, s.ipsetsPath(), body, nil); err != nil {
		return fmt.Errorf("firewall.RenameIPSet: %w", err)
	}
	return nil
}

// DeleteIPSet removes an IPSet. The write is synchronous (no task).
func (s *Service) DeleteIPSet(ctx context.Context, name string) error {
	if name == "" {
		return fmt.Errorf("firewall.DeleteIPSet: name: %w", svcutil.ErrMissingField)
	}
	if err := s.c.DoRequest(ctx, http.MethodDelete, s.ipsetPath(name), nil, nil); err != nil {
		return fmt.Errorf("firewall.DeleteIPSet: %w", err)
	}
	return nil
}

// ListIPSetEntries returns the CIDRs in an IPSet.
func (s *Service) ListIPSetEntries(ctx context.Context, name string) ([]IPSetEntry, error) {
	if name == "" {
		return nil, fmt.Errorf("firewall.ListIPSetEntries: name: %w", svcutil.ErrMissingField)
	}
	var entries []IPSetEntry
	if err := s.c.DoRequest(ctx, http.MethodGet, s.ipsetPath(name), nil, &entries); err != nil {
		return nil, fmt.Errorf("firewall.ListIPSetEntries: %w", err)
	}
	return entries, nil
}

// AddIPSetEntry adds a CIDR to an IPSet. The write is synchronous (no task).
func (s *Service) AddIPSetEntry(ctx context.Context, name string, entry *IPSetEntrySpec) error {
	if entry == nil {
		return fmt.Errorf("firewall.AddIPSetEntry: %w", svcutil.ErrNilSpec)
	}
	switch {
	case name == "":
		return fmt.Errorf("firewall.AddIPSetEntry: name: %w", svcutil.ErrMissingField)
	case entry.CIDR == "":
		return fmt.Errorf("firewall.AddIPSetEntry: cidr: %w", svcutil.ErrMissingField)
	}
	body, err := svcutil.EncodeWithExtra(entry, entry.Extra)
	if err != nil {
		return fmt.Errorf("firewall.AddIPSetEntry: %w", err)
	}
	if err := s.c.DoRequest(ctx, http.MethodPost, s.ipsetPath(name), body, nil); err != nil {
		return fmt.Errorf("firewall.AddIPSetEntry: %w", err)
	}
	return nil
}

// DeleteIPSetEntry removes a CIDR from an IPSet. The write is synchronous (no
// task).
func (s *Service) DeleteIPSetEntry(ctx context.Context, name, cidr string) error {
	switch {
	case name == "":
		return fmt.Errorf("firewall.DeleteIPSetEntry: name: %w", svcutil.ErrMissingField)
	case cidr == "":
		return fmt.Errorf("firewall.DeleteIPSetEntry: cidr: %w", svcutil.ErrMissingField)
	}
	if err := s.c.DoRequest(ctx, http.MethodDelete, s.ipsetEntryPath(name, cidr), nil, nil); err != nil {
		return fmt.Errorf("firewall.DeleteIPSetEntry: %w", err)
	}
	return nil
}
