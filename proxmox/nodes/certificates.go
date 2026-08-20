package nodes

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/internal/svcutil"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/tasks"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/types"
)

// Certificate is one node certificate from GET /nodes/{node}/certificates/info.
// Reads are lossless: keys outside the typed set land in Extra.
type Certificate struct {
	Filename      string   `json:"filename"`
	Fingerprint   string   `json:"fingerprint,omitempty"`
	Subject       string   `json:"subject,omitempty"`
	Issuer        string   `json:"issuer,omitempty"`
	NotBefore     int64    `json:"notbefore,omitempty"` // unix epoch.
	NotAfter      int64    `json:"notafter,omitempty"`  // unix epoch.
	SAN           []string `json:"san,omitempty"`
	PEM           string   `json:"pem,omitempty"`
	PublicKeyType string   `json:"public-key-type,omitempty"`
	PublicKeyBits int      `json:"public-key-bits,omitempty"`
	// Extra carries certificate keys the SDK does not model.
	Extra map[string]string `json:"-"`
}

var certificateKnownFields = map[string]bool{
	"filename": true, "fingerprint": true, "subject": true, "issuer": true,
	"notbefore": true, "notafter": true, "san": true, "pem": true,
	"public-key-type": true, "public-key-bits": true,
}

// UnmarshalJSON decodes the modelled fields and routes unknown keys into Extra.
func (c *Certificate) UnmarshalJSON(data []byte) error {
	type alias Certificate
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("decode certificate: %w", err)
	}
	*c = Certificate(a)
	extra, err := svcutil.DecodeExtra(data, certificateKnownFields)
	if err != nil {
		return fmt.Errorf("decode certificate: %w", err)
	}
	c.Extra = extra
	return nil
}

// CustomCertificateSpec is the body of POST /nodes/{node}/certificates/custom,
// uploading a caller-supplied certificate (and key) for the API/web front-end.
// Certificates (the PEM chain) is required. Pass it by pointer.
type CustomCertificateSpec struct {
	Certificates string         `json:"certificates"`
	Key          string         `json:"key,omitempty"`
	Force        *types.PVEBool `json:"force,omitempty"`   // overwrite an existing custom cert.
	Restart      *types.PVEBool `json:"restart,omitempty"` // restart pveproxy after install.
	// Extra carries PVE parameters the SDK does not model.
	Extra map[string]string `json:"-"`
}

// GetNodeCertificates returns the certificates installed on node (the API/web
// front-end chain plus any custom or ACME certs).
func (s *Service) GetNodeCertificates(ctx context.Context, node string) ([]Certificate, error) {
	var certs []Certificate
	if err := s.c.DoRequest(ctx, http.MethodGet, nodeCertInfoPath(node), nil, &certs); err != nil {
		return nil, fmt.Errorf("nodes.GetNodeCertificates: %w", err)
	}
	return certs, nil
}

// UploadCustomCertificate installs a caller-supplied certificate on node and
// returns the resulting certificate set. The write is synchronous (no task).
func (s *Service) UploadCustomCertificate(ctx context.Context, node string, spec *CustomCertificateSpec) ([]Certificate, error) {
	if spec == nil {
		return nil, fmt.Errorf("nodes.UploadCustomCertificate: %w", svcutil.ErrNilSpec)
	}
	if spec.Certificates == "" {
		return nil, fmt.Errorf("nodes.UploadCustomCertificate: certificates: %w", svcutil.ErrMissingField)
	}
	body, err := svcutil.EncodeWithExtra(spec, spec.Extra)
	if err != nil {
		return nil, fmt.Errorf("nodes.UploadCustomCertificate: %w", err)
	}
	var certs []Certificate
	if err := s.c.DoRequest(ctx, http.MethodPost, nodeCertCustomPath(node), body, &certs); err != nil {
		return nil, fmt.Errorf("nodes.UploadCustomCertificate: %w", err)
	}
	return certs, nil
}

// DeleteCustomCertificate removes node's custom certificate, reverting to the
// self-signed default. The write is synchronous (no task).
func (s *Service) DeleteCustomCertificate(ctx context.Context, node string) error {
	if err := s.c.DoRequest(ctx, http.MethodDelete, nodeCertCustomPath(node), nil, nil); err != nil {
		return fmt.Errorf("nodes.DeleteCustomCertificate: %w", err)
	}
	return nil
}

// ACMEAccount is one registered ACME account from
// GET /cluster/acme/account/{name}. Reads are lossless.
type ACMEAccount struct {
	Location  string `json:"location,omitempty"`
	Directory string `json:"directory,omitempty"`
	TOS       string `json:"tos,omitempty"`
	// Account is the raw ACME account object as returned by the CA; it is left
	// unmodelled (contact addresses, status) and preserved verbatim.
	Account json.RawMessage `json:"account,omitempty"`
	// Extra carries account keys the SDK does not model.
	Extra map[string]string `json:"-"`
}

var acmeAccountKnownFields = map[string]bool{
	"location": true, "directory": true, "tos": true, "account": true,
}

// UnmarshalJSON decodes the modelled fields and routes unknown keys into Extra.
func (a *ACMEAccount) UnmarshalJSON(data []byte) error {
	type alias ACMEAccount
	var raw alias
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("decode acme account: %w", err)
	}
	*a = ACMEAccount(raw)
	extra, err := svcutil.DecodeExtra(data, acmeAccountKnownFields)
	if err != nil {
		return fmt.Errorf("decode acme account: %w", err)
	}
	a.Extra = extra
	return nil
}

// ACMEAccountSpec is the body of POST /cluster/acme/account, registering a new
// ACME account with the CA. Name (the local account handle) and Contact (at
// least one address) are required; Contact is CSV-joined into the "contact"
// param. Pass it by pointer.
type ACMEAccountSpec struct {
	Name      string   `json:"name"`
	Contact   []string `json:"-"`
	Directory string   `json:"directory,omitempty"` // ACME directory URL; CA default if empty.
	TOSURL    string   `json:"tos_url,omitempty"`   // set to accept the CA terms of service.
	// Extra carries PVE parameters the SDK does not model (e.g. eab-kid).
	Extra map[string]string `json:"-"`
}

// ACMEAccountUpdate is the body of PUT /cluster/acme/account/{name}, changing an
// account's contact addresses. Pass it by pointer.
type ACMEAccountUpdate struct {
	Contact []string `json:"-"`
	// Extra carries PVE parameters the SDK does not model.
	Extra map[string]string `json:"-"`
}

// ListACMEAccounts returns the local handles of the registered ACME accounts.
// ACME is cluster-scoped; this and the other ACMEAccount methods take no node.
func (s *Service) ListACMEAccounts(ctx context.Context) ([]string, error) {
	var entries []struct {
		Name string `json:"name"`
	}
	if err := s.c.DoRequest(ctx, http.MethodGet, acmeAccountsPath(), nil, &entries); err != nil {
		return nil, fmt.Errorf("nodes.ListACMEAccounts: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name)
	}
	return names, nil
}

// GetACMEAccount returns one registered ACME account by its local handle.
//
// An account that does not exist is reported by PVE as HTTP 400 "Parameter
// verification failed" with the detail under the name parameter — "ACME
// account config file '<name>' does not exist." — NOT as a 404. So
// errors.Is(err, pverr.ErrNotFound) does not fire for a missing account, and
// code that branches on absence should test the parameter detail instead:
//
//	var perr *pverr.Error
//	if errors.As(err, &perr) && perr.Status == http.StatusBadRequest &&
//		strings.Contains(perr.Params["name"], "does not exist") {
//		// not registered yet
//	}
//
// Observed live 2026-08-19 against PVE 9.2. The SDK does not reclassify it:
// deciding from the message text which 400s are really 404s would bake a guess
// about PVE's prose into the transport, where every op would inherit it.
// [ListACMEAccounts] is the allocation-free way to test for existence.
func (s *Service) GetACMEAccount(ctx context.Context, name string) (*ACMEAccount, error) {
	if name == "" {
		return nil, fmt.Errorf("nodes.GetACMEAccount: name: %w", svcutil.ErrMissingField)
	}
	var acc ACMEAccount
	if err := s.c.DoRequest(ctx, http.MethodGet, acmeAccountPath(name), nil, &acc); err != nil {
		return nil, fmt.Errorf("nodes.GetACMEAccount: %w", err)
	}
	return &acc, nil
}

// RegisterACMEAccount registers a new ACME account with the CA. It runs as a
// worker; the returned tasks.Ref is awaited for completion. Live-observed
// against Let's Encrypt staging on 9.2: UPID:…:acmeregister::root@pam:, exit
// status OK in a few seconds (IMPL-0007 Phase 4, 2026-08-19).
func (s *Service) RegisterACMEAccount(ctx context.Context, spec *ACMEAccountSpec) (tasks.Ref, error) {
	if spec == nil {
		return tasks.Ref{}, fmt.Errorf("nodes.RegisterACMEAccount: %w", svcutil.ErrNilSpec)
	}
	if spec.Name == "" {
		return tasks.Ref{}, fmt.Errorf("nodes.RegisterACMEAccount: name: %w", svcutil.ErrMissingField)
	}
	if len(spec.Contact) == 0 {
		return tasks.Ref{}, fmt.Errorf("nodes.RegisterACMEAccount: contact: %w", svcutil.ErrMissingField)
	}
	body, err := svcutil.EncodeWithExtra(spec, spec.Extra)
	if err != nil {
		return tasks.Ref{}, fmt.Errorf("nodes.RegisterACMEAccount: %w", err)
	}
	body.Set("contact", strings.Join(spec.Contact, ","))
	var upid string
	if err := s.c.DoRequest(ctx, http.MethodPost, acmeAccountsPath(), body, &upid); err != nil {
		return tasks.Ref{}, fmt.Errorf("nodes.RegisterACMEAccount: %w", err)
	}
	return svcutil.TaskRef("nodes.RegisterACMEAccount", upid)
}

// UpdateACMEAccount changes an account's contact addresses. It runs as a worker;
// the returned tasks.Ref is awaited.
//
// Applying the change means talking to the CA, which is why this is a task and
// not a config write — and why an update with no new information is PVE's way of
// refreshing the account from the CA. To ask for that refresh, pass an empty
// update (&ACMEAccountUpdate{}); a nil update is an error, since discarding a
// caller's nil by silently refreshing would hide the mistake.
//
// The task shape here comes from the schema, not from a node: the live run
// registered, ordered and revoked but never updated an account. If a node ever
// answers this one with null, the UPID read turns a write that succeeded into an
// error, and the fix is to make the read tolerant the way
// [Service.ApplyNetworkConfig] is.
func (s *Service) UpdateACMEAccount(ctx context.Context, name string, update *ACMEAccountUpdate) (tasks.Ref, error) {
	if update == nil {
		return tasks.Ref{}, fmt.Errorf("nodes.UpdateACMEAccount: %w", svcutil.ErrNilSpec)
	}
	if name == "" {
		return tasks.Ref{}, fmt.Errorf("nodes.UpdateACMEAccount: name: %w", svcutil.ErrMissingField)
	}
	body, err := svcutil.EncodeWithExtra(update, update.Extra)
	if err != nil {
		return tasks.Ref{}, fmt.Errorf("nodes.UpdateACMEAccount: %w", err)
	}
	if len(update.Contact) > 0 {
		body.Set("contact", strings.Join(update.Contact, ","))
	}
	var upid string
	if err := s.c.DoRequest(ctx, http.MethodPut, acmeAccountPath(name), body, &upid); err != nil {
		return tasks.Ref{}, fmt.Errorf("nodes.UpdateACMEAccount: %w", err)
	}
	return svcutil.TaskRef("nodes.UpdateACMEAccount", upid)
}

// DeactivateACMEAccount deactivates an account with the CA and removes it
// locally. It runs as a worker; the returned tasks.Ref is awaited.
func (s *Service) DeactivateACMEAccount(ctx context.Context, name string) (tasks.Ref, error) {
	if name == "" {
		return tasks.Ref{}, fmt.Errorf("nodes.DeactivateACMEAccount: name: %w", svcutil.ErrMissingField)
	}
	var upid string
	if err := s.c.DoRequest(ctx, http.MethodDelete, acmeAccountPath(name), nil, &upid); err != nil {
		return tasks.Ref{}, fmt.Errorf("nodes.DeactivateACMEAccount: %w", err)
	}
	return svcutil.TaskRef("nodes.DeactivateACMEAccount", upid)
}

// OrderNodeCertificate orders (or renews) node's ACME certificate from the CA
// via POST /nodes/{node}/certificates/acme/certificate. It runs as a worker; the
// returned tasks.Ref is awaited.
//
// Task-vs-sync was settled from the schema and is now live-observed: a dns-01
// staging order on 9.2 returned UPID:…:acmenewcert::root@pam: and finished with
// exit status OK (IMPL-0007 Phase 4, 2026-08-19; replayable as
// TestACMEDNSCloudflare).
//
// Budget for a slow task. That order took roughly 80 seconds — thirteen status
// polls — because acme.sh writes the challenge record and then waits for the CA
// to resolve it, so the wall clock is the provider's DNS propagation, not PVE's.
// A ctx whose deadline suits a normal config write will cancel this one.
func (s *Service) OrderNodeCertificate(ctx context.Context, node string) (tasks.Ref, error) {
	var upid string
	if err := s.c.DoRequest(ctx, http.MethodPost, nodeCertACMEPath(node), nil, &upid); err != nil {
		return tasks.Ref{}, fmt.Errorf("nodes.OrderNodeCertificate: %w", err)
	}
	return svcutil.TaskRef("nodes.OrderNodeCertificate", upid)
}

// RenewNodeCertificate renews node's existing ACME certificate
// (PUT /nodes/{node}/certificates/acme/certificate). It runs as a worker; the
// returned tasks.Ref is awaited.
//
// This is the one certificate verb the live run did not exercise — the phase
// ordered and revoked, and a renewal wants a certificate old enough to be worth
// renewing. Its schema is identical to order's, and PVE renews through the same
// challenge machinery, so expect order's timing rather than a config write's.
func (s *Service) RenewNodeCertificate(ctx context.Context, node string) (tasks.Ref, error) {
	var upid string
	if err := s.c.DoRequest(ctx, http.MethodPut, nodeCertACMEPath(node), nil, &upid); err != nil {
		return tasks.Ref{}, fmt.Errorf("nodes.RenewNodeCertificate: %w", err)
	}
	return svcutil.TaskRef("nodes.RenewNodeCertificate", upid)
}

// RevokeNodeCertificate revokes node's ACME certificate with the CA
// (DELETE /nodes/{node}/certificates/acme/certificate). It runs as a worker; the
// returned tasks.Ref is awaited. Live-observed alongside the order:
// UPID:…:acmerevoke::root@pam:, exit status OK, and unlike the order it finished
// in a few seconds — nothing waits on DNS to revoke.
//
// Revoking is not the whole undo: the node config still carries the acme
// property that named the domains, and PVE will order again on its renewal
// cycle. A caller rolling an order back clears that property too — see
// [Service.SetNodeConfig].
func (s *Service) RevokeNodeCertificate(ctx context.Context, node string) (tasks.Ref, error) {
	var upid string
	if err := s.c.DoRequest(ctx, http.MethodDelete, nodeCertACMEPath(node), nil, &upid); err != nil {
		return tasks.Ref{}, fmt.Errorf("nodes.RevokeNodeCertificate: %w", err)
	}
	return svcutil.TaskRef("nodes.RevokeNodeCertificate", upid)
}
