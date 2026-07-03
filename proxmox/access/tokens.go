package access

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/internal/svcutil"
)

// ListTokens returns a user's API tokens (metadata only; secrets are shown once,
// at creation).
func (s *Service) ListTokens(ctx context.Context, userid string) ([]Token, error) {
	if userid == "" {
		return nil, fmt.Errorf("access.ListTokens: userid: %w", svcutil.ErrMissingField)
	}
	var tokens []Token
	if err := s.c.DoRequest(ctx, http.MethodGet, tokensPath(userid), nil, &tokens); err != nil {
		return nil, fmt.Errorf("access.ListTokens: %w", err)
	}
	return tokens, nil
}

// GetToken returns one token's metadata.
func (s *Service) GetToken(ctx context.Context, userid, tokenid string) (*Token, error) {
	if err := requireUserToken("access.GetToken", userid, tokenid); err != nil {
		return nil, err
	}
	var t Token
	if err := s.c.DoRequest(ctx, http.MethodGet, tokenPath(userid, tokenid), nil, &t); err != nil {
		return nil, fmt.Errorf("access.GetToken: %w", err)
	}
	return &t, nil
}

// CreateToken creates an API token and returns its one-time secret. Store the
// secret immediately — PVE will not show it again. The write is synchronous.
func (s *Service) CreateToken(ctx context.Context, userid, tokenid string, spec *TokenSpec) (*TokenSecret, error) {
	if err := requireUserToken("access.CreateToken", userid, tokenid); err != nil {
		return nil, err
	}
	var body url.Values
	if spec != nil {
		var err error
		if body, err = svcutil.EncodeWithExtra(spec, spec.Extra); err != nil {
			return nil, fmt.Errorf("access.CreateToken: %w", err)
		}
	}
	var secret TokenSecret
	if err := s.c.DoRequest(ctx, http.MethodPost, tokenPath(userid, tokenid), body, &secret); err != nil {
		return nil, fmt.Errorf("access.CreateToken: %w", err)
	}
	return &secret, nil
}

// UpdateToken changes a token's comment, expiry, or privilege separation. The
// write is synchronous (no task).
func (s *Service) UpdateToken(ctx context.Context, userid, tokenid string, spec *TokenSpec) error {
	if spec == nil {
		return fmt.Errorf("access.UpdateToken: %w", svcutil.ErrNilSpec)
	}
	if err := requireUserToken("access.UpdateToken", userid, tokenid); err != nil {
		return err
	}
	body, err := svcutil.EncodeWithExtra(spec, spec.Extra)
	if err != nil {
		return fmt.Errorf("access.UpdateToken: %w", err)
	}
	if err := s.c.DoRequest(ctx, http.MethodPut, tokenPath(userid, tokenid), body, nil); err != nil {
		return fmt.Errorf("access.UpdateToken: %w", err)
	}
	return nil
}

// RevokeToken deletes a token. The write is synchronous (no task).
func (s *Service) RevokeToken(ctx context.Context, userid, tokenid string) error {
	if err := requireUserToken("access.RevokeToken", userid, tokenid); err != nil {
		return err
	}
	if err := s.c.DoRequest(ctx, http.MethodDelete, tokenPath(userid, tokenid), nil, nil); err != nil {
		return fmt.Errorf("access.RevokeToken: %w", err)
	}
	return nil
}

// ClearTokenComment explicitly clears a token's comment. This requires PVE 9.1,
// where clearing (as opposed to leaving unset) became reliable; below that it
// returns a pverr.ErrUnsupported-wrapped error. It PUTs an empty comment to the
// same endpoint as UpdateToken.
func (s *Service) ClearTokenComment(ctx context.Context, userid, tokenid string) error {
	if err := requireUserToken("access.ClearTokenComment", userid, tokenid); err != nil {
		return err
	}
	if err := s.caps.Require("clear token comment", "9.1"); err != nil {
		return fmt.Errorf("access.ClearTokenComment: %w", err)
	}
	// EncodeWithExtra would drop an empty comment (omitempty), so force it.
	body := url.Values{"comment": {""}}
	if err := s.c.DoRequest(ctx, http.MethodPut, tokenPath(userid, tokenid), body, nil); err != nil {
		return fmt.Errorf("access.ClearTokenComment: %w", err)
	}
	return nil
}

// RegenerateTokenSecret rotates a token's secret in place, returning the new
// one-time secret while keeping the token's ID and ACLs. This requires PVE 9.2;
// below that it returns a pverr.ErrUnsupported-wrapped error.
//
// API-shape caveat: the rotate endpoint is not confirmed against a live 9.2
// node. The SDK provisionally PUTs regenerate=1 to the token path; adjust here
// once the real shape is known. The signature is stable regardless.
func (s *Service) RegenerateTokenSecret(ctx context.Context, userid, tokenid string) (*TokenSecret, error) {
	if err := requireUserToken("access.RegenerateTokenSecret", userid, tokenid); err != nil {
		return nil, err
	}
	if err := s.caps.Require("token secret rotation", "9.2"); err != nil {
		return nil, fmt.Errorf("access.RegenerateTokenSecret: %w", err)
	}
	body := url.Values{"regenerate": {"1"}}
	var secret TokenSecret
	if err := s.c.DoRequest(ctx, http.MethodPut, tokenPath(userid, tokenid), body, &secret); err != nil {
		return nil, fmt.Errorf("access.RegenerateTokenSecret: %w", err)
	}
	return &secret, nil
}

// requireUserToken validates the userid/tokenid pair common to the token ops.
func requireUserToken(op, userid, tokenid string) error {
	switch {
	case userid == "":
		return fmt.Errorf("%s: userid: %w", op, svcutil.ErrMissingField)
	case tokenid == "":
		return fmt.Errorf("%s: tokenid: %w", op, svcutil.ErrMissingField)
	}
	return nil
}
