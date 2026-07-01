package firewall

import (
	"context"
	"fmt"
	"net/http"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/internal/svcutil"
)

// ListRules returns the scoped firewall rule table, ordered by position.
func (s *Service) ListRules(ctx context.Context) ([]Rule, error) {
	var rules []Rule
	if err := s.c.DoRequest(ctx, http.MethodGet, s.rulesPath(), nil, &rules); err != nil {
		return nil, fmt.Errorf("firewall.ListRules: %w", err)
	}
	return rules, nil
}

// GetRule returns one rule by its position in the scoped table.
func (s *Service) GetRule(ctx context.Context, pos int) (*Rule, error) {
	var r Rule
	if err := s.c.DoRequest(ctx, http.MethodGet, s.rulePath(pos), nil, &r); err != nil {
		return nil, fmt.Errorf("firewall.GetRule: %w", err)
	}
	return &r, nil
}

// CreateRule inserts a rule into the scoped table. The write is synchronous (no
// task).
func (s *Service) CreateRule(ctx context.Context, spec *RuleSpec) error {
	if spec == nil {
		return fmt.Errorf("firewall.CreateRule: %w", svcutil.ErrNilSpec)
	}
	switch {
	case spec.Type == "":
		return fmt.Errorf("firewall.CreateRule: type: %w", svcutil.ErrMissingField)
	case spec.Action == "":
		return fmt.Errorf("firewall.CreateRule: action: %w", svcutil.ErrMissingField)
	}
	body, err := svcutil.EncodeWithExtra(spec, spec.Extra)
	if err != nil {
		return fmt.Errorf("firewall.CreateRule: %w", err)
	}
	if err := s.c.DoRequest(ctx, http.MethodPost, s.rulesPath(), body, nil); err != nil {
		return fmt.Errorf("firewall.CreateRule: %w", err)
	}
	return nil
}

// UpdateRule changes the rule at pos. The write is synchronous (no task).
func (s *Service) UpdateRule(ctx context.Context, pos int, update *RuleUpdate) error {
	if update == nil {
		return fmt.Errorf("firewall.UpdateRule: %w", svcutil.ErrNilSpec)
	}
	body, err := svcutil.EncodeWithExtra(update, update.Extra)
	if err != nil {
		return fmt.Errorf("firewall.UpdateRule: %w", err)
	}
	if err := s.c.DoRequest(ctx, http.MethodPut, s.rulePath(pos), body, nil); err != nil {
		return fmt.Errorf("firewall.UpdateRule: %w", err)
	}
	return nil
}

// DeleteRule removes the rule at pos. The write is synchronous (no task).
func (s *Service) DeleteRule(ctx context.Context, pos int) error {
	if err := s.c.DoRequest(ctx, http.MethodDelete, s.rulePath(pos), nil, nil); err != nil {
		return fmt.Errorf("firewall.DeleteRule: %w", err)
	}
	return nil
}
