package nodes_test

import (
	"context"
	"strings"
	"testing"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/mockpve"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/nodes"
)

// TestNodeConfigACMERoundTrip is the phase's flagship: wire a node for DNS-01 on
// two domains, read it back, and get the same values through the typed fields
// rather than a property string.
func TestNodeConfigACMERoundTrip(t *testing.T) {
	t.Parallel()
	svc := newService(t, mockpve.New())
	ctx := context.Background()

	err := svc.SetNodeConfig(ctx, "pve", &nodes.NodeConfigUpdate{
		ACME: &nodes.NodeACME{Account: "le-staging"},
		ACMEDomains: []nodes.ACMEDomain{
			{Index: 0, Domain: "pve.lab.example", Plugin: "cf-lab"},
			{Index: 1, Domain: "alt.lab.example", Plugin: "cf-lab", Alias: "alias.example"},
		},
	})
	if err != nil {
		t.Fatalf("SetNodeConfig: %v", err)
	}

	cfg, err := svc.GetNodeConfig(ctx, "pve")
	if err != nil {
		t.Fatalf("GetNodeConfig: %v", err)
	}
	if cfg.ACME == nil {
		t.Fatal("ACME = nil, want the account that was just written")
	}
	if cfg.ACME.Account != "le-staging" {
		t.Errorf("Account = %q, want %q", cfg.ACME.Account, "le-staging")
	}
	if len(cfg.ACMEDomains) != 2 {
		t.Fatalf("ACMEDomains = %+v, want 2 slots", cfg.ACMEDomains)
	}
	// Slots come back index-ordered regardless of the order they went out in.
	want := []nodes.ACMEDomain{
		{Index: 0, Domain: "pve.lab.example", Plugin: "cf-lab"},
		{Index: 1, Domain: "alt.lab.example", Plugin: "cf-lab", Alias: "alias.example"},
	}
	for i, w := range want {
		if cfg.ACMEDomains[i] != w {
			t.Errorf("ACMEDomains[%d] = %+v, want %+v", i, cfg.ACMEDomains[i], w)
		}
	}
	if cfg.Digest == "" {
		t.Error("Digest is empty, want the guard value for a follow-up write")
	}
}

// TestNodeConfigSparseSlots covers the case that motivated ACMEDomain.Index:
// slots 0 and 3 with nothing between them must survive a read without being
// renumbered, or a read-modify-write would silently move a domain.
func TestNodeConfigSparseSlots(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.SetNodeConfigKey("pve", "acmedomain0", "first.example,plugin=cf")
	mock.SetNodeConfigKey("pve", "acmedomain3", "fourth.example")
	svc := newService(t, mock)

	cfg, err := svc.GetNodeConfig(context.Background(), "pve")
	if err != nil {
		t.Fatalf("GetNodeConfig: %v", err)
	}
	if len(cfg.ACMEDomains) != 2 {
		t.Fatalf("ACMEDomains = %+v, want 2", cfg.ACMEDomains)
	}
	if cfg.ACMEDomains[0].Index != 0 || cfg.ACMEDomains[1].Index != 3 {
		t.Errorf("indices = %d,%d, want 0,3 — the slots were renumbered",
			cfg.ACMEDomains[0].Index, cfg.ACMEDomains[1].Index)
	}
	if cfg.ACMEDomains[1].Domain != "fourth.example" {
		t.Errorf("slot 3 domain = %q, want %q", cfg.ACMEDomains[1].Domain, "fourth.example")
	}
}

// TestNodeConfigExplicitDelete pins the contract: a write touches only the slots
// it names, and removing one takes an explicit Delete.
func TestNodeConfigExplicitDelete(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.SetNodeConfigKey("pve", "acmedomain0", "keep.example,plugin=cf")
	mock.SetNodeConfigKey("pve", "acmedomain1", "drop.example,plugin=cf")
	svc := newService(t, mock)
	ctx := context.Background()

	// A write naming only slot 0 must leave slot 1 alone.
	if err := svc.SetNodeConfig(ctx, "pve", &nodes.NodeConfigUpdate{
		ACMEDomains: []nodes.ACMEDomain{{Index: 0, Domain: "renamed.example", Plugin: "cf"}},
	}); err != nil {
		t.Fatalf("SetNodeConfig: %v", err)
	}
	cfg, err := svc.GetNodeConfig(ctx, "pve")
	if err != nil {
		t.Fatalf("GetNodeConfig: %v", err)
	}
	if len(cfg.ACMEDomains) != 2 {
		t.Fatalf("ACMEDomains = %+v, want both slots still present", cfg.ACMEDomains)
	}
	if cfg.ACMEDomains[0].Domain != "renamed.example" {
		t.Errorf("slot 0 = %q, want the rewritten value", cfg.ACMEDomains[0].Domain)
	}
	if cfg.ACMEDomains[1].Domain != "drop.example" {
		t.Errorf("slot 1 = %q, want it untouched", cfg.ACMEDomains[1].Domain)
	}

	// Removing it is explicit.
	if err := svc.SetNodeConfig(ctx, "pve", &nodes.NodeConfigUpdate{
		Delete: []string{"acmedomain1"},
	}); err != nil {
		t.Fatalf("SetNodeConfig delete: %v", err)
	}
	cfg, err = svc.GetNodeConfig(ctx, "pve")
	if err != nil {
		t.Fatalf("GetNodeConfig: %v", err)
	}
	if len(cfg.ACMEDomains) != 1 || cfg.ACMEDomains[0].Index != 0 {
		t.Errorf("ACMEDomains = %+v, want only slot 0", cfg.ACMEDomains)
	}
}

// TestNodeConfigLossless proves the unmodelled keys survive a read: the node
// config carries settings this SDK does not type, and losing them on a read
// would make any read-modify-write destructive.
func TestNodeConfigLossless(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.SetNodeConfigKey("pve", "description", "rack 3, top of stack")
	mock.SetNodeConfigKey("pve", "wakeonlan", "aa:bb:cc:dd:ee:ff")
	mock.SetNodeConfigKey("pve", "startall-onboot-delay", "30")
	mock.SetNodeConfigKey("pve", "acme", "account=default")
	svc := newService(t, mock)

	cfg, err := svc.GetNodeConfig(context.Background(), "pve")
	if err != nil {
		t.Fatalf("GetNodeConfig: %v", err)
	}
	for key, want := range map[string]string{
		"description":           "rack 3, top of stack",
		"wakeonlan":             "aa:bb:cc:dd:ee:ff",
		"startall-onboot-delay": "30",
	} {
		if got := cfg.Extra[key]; got != want {
			t.Errorf("Extra[%q] = %q, want %q", key, got, want)
		}
	}
	// The typed keys are not duplicated into Extra.
	for _, key := range []string{"acme", "digest"} {
		if _, ok := cfg.Extra[key]; ok {
			t.Errorf("Extra[%q] is set, want the typed field to own it", key)
		}
	}
}

// TestNodeConfigMalformedSlot covers a slot the SDK cannot parse: the read must
// still succeed, with the raw value preserved. Failing the whole read would make
// one hand-edited line render a node unreadable.
func TestNodeConfigMalformedSlot(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.SetNodeConfigKey("pve", "acmedomain0", "good.example,plugin=cf")
	mock.SetNodeConfigKey("pve", "acmedomain1", "plugin=cf") // no domain
	svc := newService(t, mock)

	cfg, err := svc.GetNodeConfig(context.Background(), "pve")
	if err != nil {
		t.Fatalf("GetNodeConfig: %v", err)
	}
	if len(cfg.ACMEDomains) != 1 || cfg.ACMEDomains[0].Domain != "good.example" {
		t.Errorf("ACMEDomains = %+v, want only the parseable slot", cfg.ACMEDomains)
	}
	if got := cfg.Extra["acmedomain1"]; got != "plugin=cf" {
		t.Errorf("Extra[acmedomain1] = %q, want the raw unparseable value", got)
	}
}

// TestNodeConfigDigestGuard covers the concurrent-write guard in both
// directions — a refusal test alone would pass against a server that refused
// everything.
func TestNodeConfigDigestGuard(t *testing.T) {
	t.Parallel()
	svc := newService(t, mockpve.New())
	ctx := context.Background()

	cfg, err := svc.GetNodeConfig(ctx, "pve")
	if err != nil {
		t.Fatalf("GetNodeConfig: %v", err)
	}

	err = svc.SetNodeConfig(ctx, "pve", &nodes.NodeConfigUpdate{
		ACME:   &nodes.NodeACME{Account: "stale"},
		Digest: "not-the-current-digest",
	})
	if err == nil {
		t.Error("SetNodeConfig with a stale digest succeeded, want a refusal")
	}

	if err := svc.SetNodeConfig(ctx, "pve", &nodes.NodeConfigUpdate{
		ACME:   &nodes.NodeACME{Account: "fresh"},
		Digest: cfg.Digest,
	}); err != nil {
		t.Fatalf("SetNodeConfig with the current digest: %v", err)
	}
}

// TestNodeConfigGuards covers the client-side refusals, including the duplicate
// index — two entries at one slot would silently drop a write with no way for
// the caller to tell which one landed.
func TestNodeConfigGuards(t *testing.T) {
	t.Parallel()
	svc := newService(t, mockpve.New())
	ctx := context.Background()

	if err := svc.SetNodeConfig(ctx, "pve", nil); err == nil {
		t.Error("SetNodeConfig(nil): want a nil-spec error")
	}
	if err := svc.SetNodeConfig(ctx, "", &nodes.NodeConfigUpdate{}); err == nil {
		t.Error("SetNodeConfig with no node: want a missing-field error")
	}
	if _, err := svc.GetNodeConfig(ctx, ""); err == nil {
		t.Error("GetNodeConfig with no node: want a missing-field error")
	}
	if err := svc.SetNodeConfig(ctx, "pve", &nodes.NodeConfigUpdate{
		ACMEDomains: []nodes.ACMEDomain{{Index: 0}},
	}); err == nil {
		t.Error("SetNodeConfig with an empty domain: want a missing-field error")
	}
	err := svc.SetNodeConfig(ctx, "pve", &nodes.NodeConfigUpdate{
		ACMEDomains: []nodes.ACMEDomain{
			{Index: 0, Domain: "a.example"},
			{Index: 0, Domain: "b.example"},
		},
	})
	if err == nil {
		t.Fatal("SetNodeConfig with a duplicate index succeeded, want an invalid-value error")
	}
	if !strings.Contains(err.Error(), "twice") {
		t.Errorf("error = %v, want it to name the duplicated slot", err)
	}
	if err := svc.SetNodeConfig(ctx, "pve", &nodes.NodeConfigUpdate{
		ACMEDomains: []nodes.ACMEDomain{{Index: -1, Domain: "a.example"}},
	}); err == nil {
		t.Error("SetNodeConfig with a negative index: want an invalid-value error")
	}
}

// TestNodeConfigSlotBounds pins PVE's actual slot range. The PUT schema renders
// the key as the wildcard "acmedomain[n]", which reads as unbounded — but it
// also sets additionalProperties:0, and the GET's property filter enumerates
// acmedomain0..acmedomain5. Writing slot 6 is a parameter-verification error,
// so the SDK refuses it rather than sending a request that cannot succeed.
func TestNodeConfigSlotBounds(t *testing.T) {
	t.Parallel()
	svc := newService(t, mockpve.New())
	ctx := context.Background()

	if err := svc.SetNodeConfig(ctx, "pve", &nodes.NodeConfigUpdate{
		ACMEDomains: []nodes.ACMEDomain{{Index: nodes.ACMEDomainMaxIndex, Domain: "last.example"}},
	}); err != nil {
		t.Errorf("SetNodeConfig at the maximum slot: %v", err)
	}
	err := svc.SetNodeConfig(ctx, "pve", &nodes.NodeConfigUpdate{
		ACMEDomains: []nodes.ACMEDomain{{Index: nodes.ACMEDomainMaxIndex + 1, Domain: "over.example"}},
	})
	if err == nil {
		t.Fatal("SetNodeConfig above the maximum slot succeeded, want an invalid-value error")
	}
	if !strings.Contains(err.Error(), "0-5") {
		t.Errorf("error = %v, want it to state the range", err)
	}
}

// TestNodeConfigDeleteBeforeSet pins the ordering PVE uses: a request that both
// deletes a key and sets it leaves it SET. The mock has to agree, or a test
// written against it would encode the opposite expectation.
func TestNodeConfigDeleteBeforeSet(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.SetNodeConfigKey("pve", "acmedomain1", "old.example")
	svc := newService(t, mock)
	ctx := context.Background()

	if err := svc.SetNodeConfig(ctx, "pve", &nodes.NodeConfigUpdate{
		ACMEDomains: []nodes.ACMEDomain{{Index: 1, Domain: "new.example"}},
		Delete:      []string{nodes.ACMEDomainKey(1)},
	}); err != nil {
		t.Fatalf("SetNodeConfig: %v", err)
	}
	cfg, err := svc.GetNodeConfig(ctx, "pve")
	if err != nil {
		t.Fatalf("GetNodeConfig: %v", err)
	}
	if len(cfg.ACMEDomains) != 1 || cfg.ACMEDomains[0].Domain != "new.example" {
		t.Errorf("ACMEDomains = %+v, want slot 1 set to new.example — deletes apply before sets",
			cfg.ACMEDomains)
	}
}

// TestNodeConfigEmptyACMERefused covers a silent clear: an empty acme= wipes the
// account and the legacy domain list on a real node, and this type promises that
// clearing is explicit.
func TestNodeConfigEmptyACMERefused(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.SetNodeConfigKey("pve", "acme", "account=live,domains=a.example")
	svc := newService(t, mock)
	ctx := context.Background()

	if err := svc.SetNodeConfig(ctx, "pve", &nodes.NodeConfigUpdate{
		ACME: &nodes.NodeACME{},
	}); err == nil {
		t.Error("SetNodeConfig with an empty ACME succeeded, want an invalid-value error")
	}
	cfg, err := svc.GetNodeConfig(ctx, "pve")
	if err != nil {
		t.Fatalf("GetNodeConfig: %v", err)
	}
	if cfg.ACME == nil || cfg.ACME.Account != "live" {
		t.Errorf("ACME = %+v, want the stored account untouched", cfg.ACME)
	}
	// Clearing it is explicit, and that path works.
	if err := svc.SetNodeConfig(ctx, "pve", &nodes.NodeConfigUpdate{
		Delete: []string{"acme"},
	}); err != nil {
		t.Fatalf("SetNodeConfig delete acme: %v", err)
	}
	if cfg, err = svc.GetNodeConfig(ctx, "pve"); err != nil {
		t.Fatalf("GetNodeConfig: %v", err)
	}
	if cfg.ACME != nil {
		t.Errorf("ACME = %+v, want nil after an explicit delete", cfg.ACME)
	}
}
