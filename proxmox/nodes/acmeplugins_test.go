package nodes_test

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/mockpve"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/nodes"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/pverr"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/types"
)

// decodeData is the test-side inverse of the SDK's internal encoding. The SDK
// exposes no decoder on purpose (DESIGN-0006: printing a plugin must not spill
// plaintext credentials), so the assertions decode for themselves.
func decodeData(t *testing.T, encoded string) string {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode plugin data %q: %v", encoded, err)
	}
	return string(raw)
}

// TestCreateACMEPluginCloudflare is the flagship round-trip: a typed provider in,
// and a read back that returns byte-identical stored credentials.
func TestCreateACMEPluginCloudflare(t *testing.T) {
	t.Parallel()
	svc := newService(t, mockpve.New())
	ctx := context.Background()

	delay := 30
	if err := svc.CreateACMEPlugin(ctx, &nodes.ACMEPluginSpec{
		ID:              "cf-lab",
		Data:            nodes.ACMECloudflare{Token: "cf-token", AccountID: "acct-1"},
		ValidationDelay: &delay,
		Nodes:           []string{"pve1", "pve2"},
	}); err != nil {
		t.Fatalf("CreateACMEPlugin: %v", err)
	}

	got, err := svc.GetACMEPlugin(ctx, "cf-lab")
	if err != nil {
		t.Fatalf("GetACMEPlugin: %v", err)
	}
	if got.Plugin != "cf-lab" {
		t.Errorf("Plugin = %q, want %q", got.Plugin, "cf-lab")
	}
	// Type defaults to dns without the caller saying so.
	if got.Type != nodes.ACMEChallengeTypeDNS {
		t.Errorf("Type = %q, want %q", got.Type, nodes.ACMEChallengeTypeDNS)
	}
	// The api parameter comes from the provider, never from the caller.
	if got.API != "cf" {
		t.Errorf("API = %q, want %q", got.API, "cf")
	}
	if got, want := decodeData(t, got.Data),
		"CF_Account_ID=acct-1\nCF_Token=cf-token"; got != want {
		t.Errorf("data = %q, want %q", got, want)
	}
	if got.ValidationDelay != 30 {
		t.Errorf("ValidationDelay = %d, want 30", got.ValidationDelay)
	}
	if got.Nodes != "pve1,pve2" {
		t.Errorf("Nodes = %q, want %q", got.Nodes, "pve1,pve2")
	}
	if got.Digest == "" {
		t.Error("Digest is empty; an update cannot be guarded without it")
	}
}

// TestCreateACMEPluginRaw proves the escape hatch reaches a provider the SDK does
// not type, which is the property that keeps all 160 of PVE's providers usable.
func TestCreateACMEPluginRaw(t *testing.T) {
	t.Parallel()
	svc := newService(t, mockpve.New())
	ctx := context.Background()

	if err := svc.CreateACMEPlugin(ctx, &nodes.ACMEPluginSpec{
		ID: "desec-lab",
		Data: nodes.ACMERawPluginData{
			Provider: "desec",
			Values:   map[string]string{"DEDYN_TOKEN": "tok", "DEDYN_NAME": "h.dedyn.io"},
		},
	}); err != nil {
		t.Fatalf("CreateACMEPlugin: %v", err)
	}
	got, err := svc.GetACMEPlugin(ctx, "desec-lab")
	if err != nil {
		t.Fatalf("GetACMEPlugin: %v", err)
	}
	if got.API != "desec" {
		t.Errorf("API = %q, want %q", got.API, "desec")
	}
	if got, want := decodeData(t, got.Data),
		"DEDYN_NAME=h.dedyn.io\nDEDYN_TOKEN=tok"; got != want {
		t.Errorf("data = %q, want %q", got, want)
	}
}

// TestCreateACMEPluginStandalone covers the non-DNS challenge: no provider, no
// credentials, and the data requirement must not fire.
func TestCreateACMEPluginStandalone(t *testing.T) {
	t.Parallel()
	svc := newService(t, mockpve.New())
	ctx := context.Background()

	if err := svc.CreateACMEPlugin(ctx, &nodes.ACMEPluginSpec{
		ID:   "http-only",
		Type: nodes.ACMEChallengeTypeStandalone,
	}); err != nil {
		t.Fatalf("CreateACMEPlugin: %v", err)
	}
	got, err := svc.GetACMEPlugin(ctx, "http-only")
	if err != nil {
		t.Fatalf("GetACMEPlugin: %v", err)
	}
	if got.Type != nodes.ACMEChallengeTypeStandalone {
		t.Errorf("Type = %q, want %q", got.Type, nodes.ACMEChallengeTypeStandalone)
	}
	if got.API != "" || got.Data != "" {
		t.Errorf("API/Data = %q/%q, want both empty for a standalone plugin", got.API, got.Data)
	}
}

func TestListACMEPlugins(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddACMEPlugin("cf-lab", "dns", "cf", "Q0ZfVG9rZW49dA==")
	mock.AddACMEPlugin("nc-lab", "dns", "namecheap", "")
	svc := newService(t, mock)

	plugins, err := svc.ListACMEPlugins(context.Background())
	if err != nil {
		t.Fatalf("ListACMEPlugins: %v", err)
	}
	if len(plugins) != 2 {
		t.Fatalf("ListACMEPlugins returned %d, want 2", len(plugins))
	}
	// The mock sorts, so the order is part of the contract a doc Example relies on.
	if plugins[0].Plugin != "cf-lab" || plugins[1].Plugin != "nc-lab" {
		t.Errorf("order = %q, %q; want cf-lab, nc-lab", plugins[0].Plugin, plugins[1].Plugin)
	}
}

// TestUpdateACMEPluginRotatesData covers the credential-rotation path: an update
// carrying Data replaces the stored payload, which is why ACMEPluginUpdate has a
// Data field at all (delete-and-recreate would be the alternative).
func TestUpdateACMEPluginRotatesData(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddACMEPlugin("cf-lab", "dns", "cf", "old-payload")
	svc := newService(t, mock)
	ctx := context.Background()

	if err := svc.UpdateACMEPlugin(ctx, "cf-lab", &nodes.ACMEPluginUpdate{
		Data: nodes.ACMECloudflare{Token: "rotated-token"},
	}); err != nil {
		t.Fatalf("UpdateACMEPlugin: %v", err)
	}
	got, err := svc.GetACMEPlugin(ctx, "cf-lab")
	if err != nil {
		t.Fatalf("GetACMEPlugin: %v", err)
	}
	if got, want := decodeData(t, got.Data), "CF_Token=rotated-token"; got != want {
		t.Errorf("data = %q, want %q", got, want)
	}
}

// TestUpdateACMEPluginKeepsData is the other half of the rotation contract: an
// update with no Data must not blank the stored credentials.
func TestUpdateACMEPluginKeepsData(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddACMEPlugin("cf-lab", "dns", "cf", "keep-me")
	svc := newService(t, mock)
	ctx := context.Background()

	disable := types.PVEBool(true)
	if err := svc.UpdateACMEPlugin(ctx, "cf-lab", &nodes.ACMEPluginUpdate{
		Disable: &disable,
	}); err != nil {
		t.Fatalf("UpdateACMEPlugin: %v", err)
	}
	got, err := svc.GetACMEPlugin(ctx, "cf-lab")
	if err != nil {
		t.Fatalf("GetACMEPlugin: %v", err)
	}
	if got.Data != "keep-me" {
		t.Errorf("data = %q, want the stored %q", got.Data, "keep-me")
	}
	if !got.Disable.Bool() {
		t.Error("Disable = false, want true")
	}
}

func TestUpdateACMEPluginDelete(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddACMEPlugin("cf-lab", "dns", "cf", "payload")
	svc := newService(t, mock)
	ctx := context.Background()

	delay := 45
	if err := svc.UpdateACMEPlugin(ctx, "cf-lab", &nodes.ACMEPluginUpdate{
		ValidationDelay: &delay,
		Nodes:           []string{"pve1"},
	}); err != nil {
		t.Fatalf("UpdateACMEPlugin (set): %v", err)
	}
	if err := svc.UpdateACMEPlugin(ctx, "cf-lab", &nodes.ACMEPluginUpdate{
		Delete: []string{"validation-delay", "nodes"},
	}); err != nil {
		t.Fatalf("UpdateACMEPlugin (delete): %v", err)
	}
	got, err := svc.GetACMEPlugin(ctx, "cf-lab")
	if err != nil {
		t.Fatalf("GetACMEPlugin: %v", err)
	}
	if got.ValidationDelay != 0 || got.Nodes != "" {
		t.Errorf("after delete: ValidationDelay=%d Nodes=%q, want 0 and empty",
			got.ValidationDelay, got.Nodes)
	}
}

// TestUpdateACMEPluginStaleDigest proves the concurrent-write guard is wired: a
// digest that no longer matches must be refused rather than silently overwriting
// someone else's change.
func TestUpdateACMEPluginStaleDigest(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddACMEPlugin("cf-lab", "dns", "cf", "payload")
	svc := newService(t, mock)
	ctx := context.Background()

	err := svc.UpdateACMEPlugin(ctx, "cf-lab", &nodes.ACMEPluginUpdate{
		Digest:  "digest-from-a-stale-read",
		Disable: ptrBool(true),
	})
	if err == nil {
		t.Fatal("UpdateACMEPlugin with a stale digest succeeded, want a refusal")
	}
	if !strings.Contains(err.Error(), "digest") {
		t.Errorf("error = %v, want it to mention the digest", err)
	}
}

// TestUpdateACMEPluginFreshDigest is the control for the test above: the digest
// from a real read must be accepted.
func TestUpdateACMEPluginFreshDigest(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddACMEPlugin("cf-lab", "dns", "cf", "payload")
	svc := newService(t, mock)
	ctx := context.Background()

	read, err := svc.GetACMEPlugin(ctx, "cf-lab")
	if err != nil {
		t.Fatalf("GetACMEPlugin: %v", err)
	}
	if err := svc.UpdateACMEPlugin(ctx, "cf-lab", &nodes.ACMEPluginUpdate{
		Digest:  read.Digest,
		Disable: ptrBool(true),
	}); err != nil {
		t.Fatalf("UpdateACMEPlugin with the read's digest: %v", err)
	}
}

func TestDeleteACMEPlugin(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddACMEPlugin("cf-lab", "dns", "cf", "payload")
	svc := newService(t, mock)
	ctx := context.Background()

	if err := svc.DeleteACMEPlugin(ctx, "cf-lab"); err != nil {
		t.Fatalf("DeleteACMEPlugin: %v", err)
	}
	if _, err := svc.GetACMEPlugin(ctx, "cf-lab"); !errors.Is(err, pverr.ErrNotFound) {
		t.Errorf("GetACMEPlugin after delete: %v, want ErrNotFound", err)
	}
}

func TestACMEPluginNotFound(t *testing.T) {
	t.Parallel()
	svc := newService(t, mockpve.New())
	ctx := context.Background()

	if _, err := svc.GetACMEPlugin(ctx, "absent"); !errors.Is(err, pverr.ErrNotFound) {
		t.Errorf("GetACMEPlugin: %v, want ErrNotFound", err)
	}
	if err := svc.DeleteACMEPlugin(ctx, "absent"); !errors.Is(err, pverr.ErrNotFound) {
		t.Errorf("DeleteACMEPlugin: %v, want ErrNotFound", err)
	}
	if err := svc.UpdateACMEPlugin(ctx, "absent",
		&nodes.ACMEPluginUpdate{Disable: ptrBool(true)}); !errors.Is(err, pverr.ErrNotFound) {
		t.Errorf("UpdateACMEPlugin: %v, want ErrNotFound", err)
	}
}

// TestACMEPluginGuards covers the client-side contract checks: they must fail
// before any request, so a caller gets a clear error rather than a PVE 400.
func TestACMEPluginGuards(t *testing.T) {
	t.Parallel()
	svc := newService(t, mockpve.New())
	ctx := context.Background()

	if err := svc.CreateACMEPlugin(ctx, nil); err == nil {
		t.Error("CreateACMEPlugin(nil) error = nil, want non-nil")
	}
	if err := svc.CreateACMEPlugin(ctx, &nodes.ACMEPluginSpec{
		Data: nodes.ACMECloudflare{Token: "t"},
	}); err == nil {
		t.Error("CreateACMEPlugin with no ID succeeded, want a missing-field error")
	}
	// A dns plugin without credentials is refused before the request: PVE would
	// accept it and then fail every order, which is much harder to diagnose.
	if err := svc.CreateACMEPlugin(ctx, &nodes.ACMEPluginSpec{ID: "no-data"}); err == nil {
		t.Error("CreateACMEPlugin with no Data succeeded, want a missing-field error")
	}
	if err := svc.UpdateACMEPlugin(ctx, "", &nodes.ACMEPluginUpdate{}); err == nil {
		t.Error("UpdateACMEPlugin with no id succeeded, want a missing-field error")
	}
	if err := svc.UpdateACMEPlugin(ctx, "cf-lab", nil); err == nil {
		t.Error("UpdateACMEPlugin(nil): want a nil-spec error")
	}
	if _, err := svc.GetACMEPlugin(ctx, ""); err == nil {
		t.Error("GetACMEPlugin with no id succeeded, want a missing-field error")
	}
	if err := svc.DeleteACMEPlugin(ctx, ""); err == nil {
		t.Error("DeleteACMEPlugin with no id succeeded, want a missing-field error")
	}
}

// TestACMEPluginRejectsEmptyCredentials covers the trap a non-nil-but-empty
// provider sets: a struct built from an unset environment variable satisfies the
// interface, so the nil guard above does not catch it. PVE would store the
// credential-less plugin and report success, and the failure would only surface
// later as a certificate order that cannot answer its challenge.
func TestACMEPluginRejectsEmptyCredentials(t *testing.T) {
	t.Parallel()
	svc := newService(t, mockpve.New())
	ctx := context.Background()

	// Create: the whole provider is empty (imagine os.Getenv returning "").
	err := svc.CreateACMEPlugin(ctx, &nodes.ACMEPluginSpec{
		ID:   "cf-empty",
		Data: nodes.ACMECloudflare{},
	})
	if err == nil {
		t.Fatal("CreateACMEPlugin with empty credentials succeeded, want a missing-field error")
	}
	if _, getErr := svc.GetACMEPlugin(ctx, "cf-empty"); getErr == nil {
		t.Error("the refused plugin was created anyway — the guard must fire before the request")
	}

	// Update is the worse case: without the guard this returns nil having
	// changed nothing, so the caller believes a rotation happened while the old
	// credentials stay live.
	if err := svc.CreateACMEPlugin(ctx, &nodes.ACMEPluginSpec{
		ID:   "cf-live",
		Data: nodes.ACMECloudflare{Token: "original-token"},
	}); err != nil {
		t.Fatalf("CreateACMEPlugin: %v", err)
	}
	if err := svc.UpdateACMEPlugin(ctx, "cf-live", &nodes.ACMEPluginUpdate{
		Data: nodes.ACMECloudflare{},
	}); err == nil {
		t.Error("UpdateACMEPlugin with empty credentials succeeded, want a missing-field error")
	}
	got, err := svc.GetACMEPlugin(ctx, "cf-live")
	if err != nil {
		t.Fatalf("GetACMEPlugin: %v", err)
	}
	if want := "CF_Token=original-token"; decodeData(t, got.Data) != want {
		t.Errorf("stored data = %q, want the untouched %q", decodeData(t, got.Data), want)
	}

	// A raw provider with no name would send api= and is refused the same way.
	if err := svc.CreateACMEPlugin(ctx, &nodes.ACMEPluginSpec{
		ID:   "unnamed",
		Data: nodes.ACMERawPluginData{Values: map[string]string{"K": "v"}},
	}); err == nil {
		t.Error("CreateACMEPlugin with an unnamed provider succeeded, want a missing-field error")
	}
}

// TestCreateACMEPluginRejectsUnknownType pins the closed enum. Before the type
// was defined, a capital-D typo slipped past the "dns needs data" guard and was
// POSTed to the cluster config.
func TestCreateACMEPluginRejectsUnknownType(t *testing.T) {
	t.Parallel()
	svc := newService(t, mockpve.New())

	err := svc.CreateACMEPlugin(context.Background(), &nodes.ACMEPluginSpec{
		ID:   "typo",
		Type: "DNS",
		Data: nodes.ACMECloudflare{Token: "t"},
	})
	if err == nil {
		t.Fatal(`CreateACMEPlugin with Type "DNS" succeeded, want an invalid-value error`)
	}
	if !strings.Contains(err.Error(), "invalid value") {
		t.Errorf("error = %v, want it to name the invalid value", err)
	}
}

// TestACMEPluginStringRedacts guards the read type against a consumer logging a
// plugin: base64 is an encoding, not protection, so String must elide it.
func TestACMEPluginStringRedacts(t *testing.T) {
	t.Parallel()
	svc := newService(t, mockpve.New())
	ctx := context.Background()

	const token = "live-cloudflare-token"
	if err := svc.CreateACMEPlugin(ctx, &nodes.ACMEPluginSpec{
		ID:   "cf",
		Data: nodes.ACMECloudflare{Token: token},
	}); err != nil {
		t.Fatalf("CreateACMEPlugin: %v", err)
	}
	got, err := svc.GetACMEPlugin(ctx, "cf")
	if err != nil {
		t.Fatalf("GetACMEPlugin: %v", err)
	}
	// Through an any, which is how a value reaches slog — and the only form
	// that exercises fmt's dispatch to String rather than calling it directly.
	var logged any = *got
	rendered := fmt.Sprintf("%v", logged)
	if strings.Contains(rendered, got.Data) {
		t.Errorf("%%v rendered the credential blob: %s", rendered)
	}
	if !strings.Contains(rendered, "<redacted>") || !strings.Contains(rendered, "cf") {
		t.Errorf("%%v = %q, want it redacted but still identifying the plugin", rendered)
	}
}
