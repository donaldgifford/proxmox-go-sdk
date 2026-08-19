package nodes_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/mockpve"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/nodes"
)

// TestACMEDNSLifecycleEndToEnd walks the exact call sequence the live DNS-01
// test performs (proxmox/integration/acme_test.go), against mockpve.
//
// It cannot prove issuance — no CA, no DNS, no certificate. What it proves is
// the part a live run should not be spending an order attempt discovering: that
// the sequence hangs together, that each step's read-back sees what the previous
// step wrote, and that the teardown genuinely reverses the setup. Those are the
// failure modes with the worst live cost, because a botched teardown leaves a
// node on an untrusted certificate or leaves provider credentials sitting in the
// cluster config.
func TestACMEDNSLifecycleEndToEnd(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc, ts := newServiceAndTasks(t, mock)
	ctx := context.Background()

	const (
		account  = "staging"
		pluginID = "cf-lifecycle"
		domain   = "pve.acme.example"
	)

	// 1. The staging account. Registering is a task.
	ref, err := svc.RegisterACMEAccount(ctx, &nodes.ACMEAccountSpec{
		Name: account, Contact: []string{"acme@" + domain},
		Directory: "https://acme-staging.example/directory", TOSURL: "https://acme.example/tos",
	})
	if err != nil {
		t.Fatalf("RegisterACMEAccount: %v", err)
	}
	if _, err := ts.Wait(ctx, ref); err != nil {
		t.Fatalf("Wait(register): %v", err)
	}

	// 2. The challenge plugin carrying the provider credentials.
	validationDelay := 30
	if err := svc.CreateACMEPlugin(ctx, &nodes.ACMEPluginSpec{
		ID: pluginID, Type: nodes.ACMEChallengeTypeDNS,
		Data:            nodes.ACMECloudflare{Token: "cf-token-value"},
		ValidationDelay: &validationDelay,
	}); err != nil {
		t.Fatalf("CreateACMEPlugin: %v", err)
	}

	// 3. Point the node at the account and the domain. The plugin must already
	// exist: PVE resolves the reference when it orders, and a config naming a
	// missing plugin is a failure the live run would only see minutes later.
	if err := svc.SetNodeConfig(ctx, testNode, &nodes.NodeConfigUpdate{
		ACME:        &nodes.NodeACME{Account: account},
		ACMEDomains: []nodes.ACMEDomain{{Index: 0, Domain: domain, Plugin: pluginID}},
	}); err != nil {
		t.Fatalf("SetNodeConfig: %v", err)
	}

	cfg, err := svc.GetNodeConfig(ctx, testNode)
	if err != nil {
		t.Fatalf("GetNodeConfig: %v", err)
	}
	if cfg.ACME == nil || cfg.ACME.Account != account {
		t.Fatalf("node acme = %+v, want account %q", cfg.ACME, account)
	}
	if len(cfg.ACMEDomains) != 1 {
		t.Fatalf("acme domains = %+v, want exactly slot 0", cfg.ACMEDomains)
	}
	if got := cfg.ACMEDomains[0]; got.Index != 0 || got.Domain != domain || got.Plugin != pluginID {
		t.Fatalf("acmedomain0 = %+v, want {0 %s %s}", got, domain, pluginID)
	}

	// 4. Order. Live this is the slow step (the whole DNS-01 exchange rides the
	// task). The mock installs a certificate covering the configured names, so
	// the read that follows checks the order consumed the config written in
	// step 3 — the dependency that actually links these calls.
	ref, err = svc.OrderNodeCertificate(ctx, testNode)
	if err != nil {
		t.Fatalf("OrderNodeCertificate: %v", err)
	}
	if _, err := ts.Wait(ctx, ref); err != nil {
		t.Fatalf("Wait(order): %v", err)
	}
	certs, err := svc.GetNodeCertificates(ctx, testNode)
	if err != nil {
		t.Fatalf("GetNodeCertificates: %v", err)
	}
	if !certCoversDomain(certs, domain) {
		t.Fatalf("after the order, no certificate covers %s: %+v", domain, certs)
	}

	// 5. Teardown, in the order the live cleanups run (LIFO): revoke first, so
	// the node stops serving the staging certificate before anything it depends
	// on is removed.
	ref, err = svc.RevokeNodeCertificate(ctx, testNode)
	if err != nil {
		t.Fatalf("RevokeNodeCertificate: %v", err)
	}
	if _, err := ts.Wait(ctx, ref); err != nil {
		t.Fatalf("Wait(revoke): %v", err)
	}
	if certs, err = svc.GetNodeCertificates(ctx, testNode); err != nil {
		t.Fatalf("GetNodeCertificates after revoke: %v", err)
	}
	if certCoversDomain(certs, domain) {
		t.Errorf("the revoked certificate still covers %s: %+v", domain, certs)
	}

	// The node config is restored by DELETING the keys, not by writing empty
	// ones: PVE has no "unset" value for a property string, and an empty acme=
	// is a parse error rather than an erasure.
	if err := svc.SetNodeConfig(ctx, testNode, &nodes.NodeConfigUpdate{
		Delete: []string{"acme", nodes.ACMEDomainKey(0)},
	}); err != nil {
		t.Fatalf("SetNodeConfig(restore): %v", err)
	}
	cfg, err = svc.GetNodeConfig(ctx, testNode)
	if err != nil {
		t.Fatalf("GetNodeConfig after restore: %v", err)
	}
	if cfg.ACME != nil || len(cfg.ACMEDomains) != 0 {
		t.Errorf("after restore: acme = %+v, domains = %+v, want both cleared", cfg.ACME, cfg.ACMEDomains)
	}

	// The plugin goes last, and it is the one whose removal actually matters:
	// left behind, it is a live provider credential in the cluster config.
	if err := svc.DeleteACMEPlugin(ctx, pluginID); err != nil {
		t.Fatalf("DeleteACMEPlugin: %v", err)
	}
	plugins, err := svc.ListACMEPlugins(ctx)
	if err != nil {
		t.Fatalf("ListACMEPlugins: %v", err)
	}
	for _, p := range plugins {
		if p.Plugin == pluginID {
			t.Errorf("plugin %q survived deletion: %s", pluginID, p)
		}
	}

	// The account is deactivated last. This is the one step the live test does
	// NOT perform — it reuses the account across runs, since re-registering
	// gains nothing and costs a CA round trip — so the deactivate path would
	// otherwise go uncovered in the sequence it actually belongs to.
	ref, err = svc.DeactivateACMEAccount(ctx, account)
	if err != nil {
		t.Fatalf("DeactivateACMEAccount: %v", err)
	}
	if _, err := ts.Wait(ctx, ref); err != nil {
		t.Fatalf("Wait(deactivate): %v", err)
	}
	names, err := svc.ListACMEAccounts(ctx)
	if err != nil {
		t.Fatalf("ListACMEAccounts: %v", err)
	}
	if strings.Contains(strings.Join(names, ","), account) {
		t.Errorf("account %q survived deactivation: %v", account, names)
	}
}

// certCoversDomain reports whether any of the node's certificates lists domain
// among its SANs.
func certCoversDomain(certs []nodes.Certificate, domain string) bool {
	for i := range certs {
		if slices.Contains(certs[i].SAN, domain) {
			return true
		}
	}
	return false
}

// TestACMEOrderUsesLegacyDomainList covers the other half of the mock's SAN
// derivation: PVE's older acme=domains=a;b form, which predates the per-slot
// acmedomain keys and is still what a node configured through an older UI
// carries. A consumer inheriting such a node should see the same behaviour from
// mockpve as from PVE — the order certifies what the config names, whichever
// form names it.
func TestACMEOrderUsesLegacyDomainList(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	// Seeded raw, the way a hand-edited or UI-written node config arrives: the
	// SDK's own writer prefers the slots.
	mock.SetNodeConfigKey(testNode, "acme", "account=default,domains=first.example;second.example")
	svc, ts := newServiceAndTasks(t, mock)
	ctx := context.Background()

	ref, err := svc.OrderNodeCertificate(ctx, testNode)
	if err != nil {
		t.Fatalf("OrderNodeCertificate: %v", err)
	}
	if _, err := ts.Wait(ctx, ref); err != nil {
		t.Fatalf("Wait(order): %v", err)
	}
	certs, err := svc.GetNodeCertificates(ctx, testNode)
	if err != nil {
		t.Fatalf("GetNodeCertificates: %v", err)
	}
	for _, domain := range []string{"first.example", "second.example"} {
		if !certCoversDomain(certs, domain) {
			t.Errorf("certificate does not cover %s: %+v", domain, certs)
		}
	}
}

// TestACMEOrderMatchesSDKParsing pins mockpve's reading of an acmedomain slot to
// the SDK's own. PVE's default key means the domain may come first and bare or
// later and keyed, and a mock that reads only the leading token certifies
// "plugin=cf" — so the same config would mean two different things depending on
// which side of the test you asked, which is worse than either being wrong.
func TestACMEOrderMatchesSDKParsing(t *testing.T) {
	t.Parallel()
	for name, slot := range map[string]string{
		"bare domain first":  "pve.acme.example,plugin=cf",
		"keyed domain first": "domain=pve.acme.example,plugin=cf",
		"keyed domain last":  "plugin=cf,domain=pve.acme.example",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			mock := mockpve.New()
			mock.SetNodeConfigKey(testNode, "acmedomain0", slot)
			svc, ts := newServiceAndTasks(t, mock)
			ctx := context.Background()

			cfg, err := svc.GetNodeConfig(ctx, testNode)
			if err != nil {
				t.Fatalf("GetNodeConfig: %v", err)
			}
			if len(cfg.ACMEDomains) != 1 || cfg.ACMEDomains[0].Domain != "pve.acme.example" {
				t.Fatalf("SDK parsed %+v, want the one domain", cfg.ACMEDomains)
			}

			ref, err := svc.OrderNodeCertificate(ctx, testNode)
			if err != nil {
				t.Fatalf("OrderNodeCertificate: %v", err)
			}
			if _, err := ts.Wait(ctx, ref); err != nil {
				t.Fatalf("Wait(order): %v", err)
			}
			certs, err := svc.GetNodeCertificates(ctx, testNode)
			if err != nil {
				t.Fatalf("GetNodeCertificates: %v", err)
			}
			if !certCoversDomain(certs, "pve.acme.example") {
				t.Errorf("certificate does not cover the domain the SDK parsed: %+v", certs)
			}
		})
	}
}

// TestACMEOrderSkipsDomainlessSlot covers the slot PVE would reject: a property
// string with a plugin and no domain. The SDK keeps it in Extra rather than
// pretending to parse it, and the mock must not invent a certificate for it.
func TestACMEOrderSkipsDomainlessSlot(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.SetNodeConfigKey(testNode, "acmedomain0", "plugin=cf")
	svc, ts := newServiceAndTasks(t, mock)
	ctx := context.Background()

	ref, err := svc.OrderNodeCertificate(ctx, testNode)
	if err != nil {
		t.Fatalf("OrderNodeCertificate: %v", err)
	}
	if _, err := ts.Wait(ctx, ref); err != nil {
		t.Fatalf("Wait(order): %v", err)
	}
	certs, err := svc.GetNodeCertificates(ctx, testNode)
	if err != nil {
		t.Fatalf("GetNodeCertificates: %v", err)
	}
	if len(certs) != 0 {
		t.Errorf("order issued a certificate for a slot naming no domain: %+v", certs)
	}
}

// TestACMEOrderOverwritesCustomCertificate pins the file model: PVE serves ONE
// front-end certificate, so an order replaces whatever is installed there,
// including one the operator uploaded. A mock that kept both would report a
// state the node cannot be in — and would hide from a consumer that ordering
// costs them their own certificate.
func TestACMEOrderOverwritesCustomCertificate(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.SetNodeConfigKey(testNode, "acmedomain0", "pve.acme.example,plugin=cf")
	svc, ts := newServiceAndTasks(t, mock)
	ctx := context.Background()

	// Twice, because the upload handler appended rather than replaced: PVE
	// serves one file at that path, so the second upload overwrites the first.
	for range 2 {
		if _, err := svc.UploadCustomCertificate(ctx, testNode, &nodes.CustomCertificateSpec{
			Certificates: "-----BEGIN CERTIFICATE-----\nmock\n-----END CERTIFICATE-----\n",
		}); err != nil {
			t.Fatalf("UploadCustomCertificate: %v", err)
		}
	}
	ref, err := svc.OrderNodeCertificate(ctx, testNode)
	if err != nil {
		t.Fatalf("OrderNodeCertificate: %v", err)
	}
	if _, err := ts.Wait(ctx, ref); err != nil {
		t.Fatalf("Wait(order): %v", err)
	}

	certs, err := svc.GetNodeCertificates(ctx, testNode)
	if err != nil {
		t.Fatalf("GetNodeCertificates: %v", err)
	}
	byFile := make(map[string]int, len(certs))
	for i := range certs {
		byFile[certs[i].Filename]++
	}
	for filename, n := range byFile {
		if n > 1 {
			t.Errorf("%d certificates share the filename %q; PVE has one file", n, filename)
		}
	}
	if !certCoversDomain(certs, "pve.acme.example") {
		t.Errorf("after the order, no certificate covers the domain: %+v", certs)
	}
}
