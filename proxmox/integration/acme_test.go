//go:build integration

package integration

import (
	"context"
	"crypto/tls"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/nodes"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/pverr"
)

// ACME DNS-01 live verification (IMPL-0007 Phase 4). These are the only tests in
// the suite that talk to a third party — Let's Encrypt and a DNS provider — so
// they are gated on their own env set and skip cleanly without it.
//
// Two safety properties are structural, not conventions to remember:
//
//   - The directory is always Let's Encrypt STAGING, resolved from the node's own
//     ListACMEDirectories rather than hardcoded. A failed run must not burn
//     production rate limits, and a staging certificate is untrusted, which is
//     what makes the next point survivable.
//   - An order REPLACES the node's pveproxy certificate. Run this against a
//     disposable pvelab node, never r740a (IMPL-0007 OQ-4a). Cleanup revokes and
//     restores, but a cleanup that fails on a real node leaves the homelab's web
//     UI on a broken certificate.
const (
	envACMEDomain      = "PVE_TEST_ACME_DOMAIN"        // FQDN to certify, in the provider's zone
	envACMECFToken     = "PVE_TEST_ACME_CF_TOKEN"      // Cloudflare scoped API token (Zone.DNS edit)
	envACMECFAccountID = "PVE_TEST_ACME_CF_ACCOUNT_ID" // optional Cloudflare account id
	envACMENCUsername  = "PVE_TEST_ACME_NC_USERNAME"   // Namecheap API username
	envACMENCAPIKey    = "PVE_TEST_ACME_NC_API_KEY"    // Namecheap API key
	envACMENCSourceIP  = "PVE_TEST_ACME_NC_SOURCE_IP"  // Namecheap allowlisted caller IP
	envACMEAccountMail = "PVE_TEST_ACME_ACCOUNT_EMAIL" // contact address for the staging account
)

// TestACMEDNSCloudflare is the flagship live run: wire a node for DNS-01 through
// Cloudflare, order a staging certificate, and prove the served certificate
// actually covers the requested name.
func TestACMEDNSCloudflare(t *testing.T) {
	token := os.Getenv(envACMECFToken)
	if token == "" {
		t.Skipf("Cloudflare ACME test disabled (set %s and %s)", envACMECFToken, envACMEDomain)
	}
	runACMEDNSLifecycle(t, "sdk-cf", nodes.ACMECloudflare{
		Token:     token,
		AccountID: os.Getenv(envACMECFAccountID),
	})
}

// TestACMEDNSNamecheap is the same flow through the second provider, which is
// what proves the ACMEPluginData model is genuinely provider-generic rather than
// Cloudflare-shaped. It runs against the SAME domain as the Cloudflare test, so
// the two are sequential by nature: the domain's nameservers must point at the
// provider under test (IMPL-0007 Phase 4 task 4 covers the switch and the
// propagation wait).
func TestACMEDNSNamecheap(t *testing.T) {
	username, apiKey := os.Getenv(envACMENCUsername), os.Getenv(envACMENCAPIKey)
	if username == "" || apiKey == "" {
		t.Skipf("Namecheap ACME test disabled (set %s, %s, %s and %s)",
			envACMENCUsername, envACMENCAPIKey, envACMENCSourceIP, envACMEDomain)
	}
	runACMEDNSLifecycle(t, "sdk-nc", nodes.ACMENamecheap{
		Username: username,
		APIKey:   apiKey,
		SourceIP: os.Getenv(envACMENCSourceIP),
	})
}

// runACMEDNSLifecycle drives the whole flow for one provider: staging directory
// → account → plugin → node config → order → verify the served SAN → revoke and
// restore. Both provider tests are the same sequence with different credentials,
// which is the point of the exercise.
func runACMEDNSLifecycle(t *testing.T, pluginID string, data nodes.ACMEPluginData) {
	t.Helper()
	if os.Getenv(envReplay) == "1" {
		// A cassette replays the PVE side fine, but the certificate assertion
		// below dials the node's TLS port, which no cassette can stand in for.
		t.Skip("ACME lifecycle is live-only: the SAN check dials the node directly")
	}
	domain := os.Getenv(envACMEDomain)
	if domain == "" {
		t.Skipf("ACME test disabled (set %s to an FQDN in the provider's zone)", envACMEDomain)
	}
	c := newClient(t)
	node := testNode()
	svc := c.Nodes()
	account := "sdk-staging"

	stagingURL := stagingDirectory(t, svc)
	t.Logf("ordering %s on node %s via plugin %s at %s", domain, node, pluginID, stagingURL)

	// 1. The staging account. Registering is a task; a leftover account from a
	// previous run is reused rather than treated as a failure.
	registerStagingAccount(t, svc, account, stagingURL)

	// 2. The challenge plugin carrying the provider credentials.
	if err := svc.CreateACMEPlugin(testCtx(t), &nodes.ACMEPluginSpec{
		ID:   pluginID,
		Type: nodes.ACMEChallengeTypeDNS,
		Data: data,
		// DNS propagation is the usual cause of a failed DNS-01: give the
		// record time to land on the authoritative servers before the CA looks.
		ValidationDelay: ptr(60),
	}); err != nil {
		t.Fatalf("CreateACMEPlugin(%s): %v", pluginID, err)
	}
	t.Cleanup(func() {
		ctx, cancel := cleanupCtx()
		defer cancel()
		if err := svc.DeleteACMEPlugin(ctx, pluginID); err != nil {
			t.Errorf("cleanup DeleteACMEPlugin(%s): %v", pluginID, err)
		}
	})

	// The credential must round-trip: a plugin PVE stored empty would fail the
	// order later with a much less obvious message.
	plugin, err := svc.GetACMEPlugin(testCtx(t), pluginID)
	if err != nil {
		t.Fatalf("GetACMEPlugin(%s): %v", pluginID, err)
	}
	if plugin.Data == "" {
		t.Fatalf("plugin %s stored no credentials", pluginID)
	}
	if plugin.API != data.API() {
		t.Errorf("plugin api = %q, want %q", plugin.API, data.API())
	}

	// 3. Point the node at the account and the domain. The previous config is
	// restored on cleanup — this is the node's live certificate wiring.
	wireNodeForACME(t, svc, node, account, domain, pluginID)

	// 4. Order. The task carries the whole DNS-01 exchange, so it is slow: the
	// plugin's validation delay plus the CA's own propagation checks.
	ref, err := svc.OrderNodeCertificate(testCtx(t), node)
	if err != nil {
		t.Fatalf("OrderNodeCertificate(%s): %v", node, err)
	}
	orderCtx, cancelOrder := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancelOrder()
	if _, err := c.Tasks().Wait(orderCtx, ref); err != nil {
		t.Fatalf("waiting for the ACME order: %v", err)
	}

	// 5. The assertion that matters: the node now SERVES a certificate covering
	// the requested name. Reading it back from the API would only prove PVE
	// stored something.
	assertServedCertificate(t, domain)

	// 6. Revoke. The cleanup hooks restore the node config and remove the
	// plugin/account; revoking here (not in a cleanup) keeps the failure visible
	// if the CA refuses.
	revokeRef, err := svc.RevokeNodeCertificate(testCtx(t), node)
	if err != nil {
		t.Fatalf("RevokeNodeCertificate(%s): %v", node, err)
	}
	revokeCtx, cancelRevoke := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancelRevoke()
	if _, err := c.Tasks().Wait(revokeCtx, revokeRef); err != nil {
		t.Errorf("waiting for the revoke: %v", err)
	}
}

// stagingDirectory finds Let's Encrypt's staging endpoint in the node's own
// directory list. Hardcoding the URL would not exercise ListACMEDirectories, and
// this is the one place the suite can afford to prove that read is useful.
func stagingDirectory(t *testing.T, svc *nodes.Service) string {
	t.Helper()
	dirs, err := svc.ListACMEDirectories(testCtx(t))
	if err != nil {
		t.Fatalf("ListACMEDirectories: %v", err)
	}
	for _, d := range dirs {
		if strings.Contains(strings.ToLower(d.Name), "staging") {
			return d.URL
		}
	}
	t.Fatalf("no staging directory among %d entries — refusing to order against production", len(dirs))
	return ""
}

// registerStagingAccount registers the ACME account, tolerating one left behind
// by an earlier run. The account is deliberately NOT removed on cleanup: it
// holds the CA's registration key, and re-registering on every run is what
// burns rate limits.
func registerStagingAccount(t *testing.T, svc *nodes.Service, account, directory string) {
	t.Helper()
	if _, err := svc.GetACMEAccount(testCtx(t), account); err == nil {
		t.Logf("reusing the existing ACME account %q", account)
		return
	} else if !errors.Is(err, pverr.ErrNotFound) {
		t.Fatalf("GetACMEAccount(%s): %v", account, err)
	}

	// The CA requires a contact address and acceptance of its terms; read the
	// terms URL from the CA rather than hardcoding one.
	meta, err := svc.GetACMEMeta(testCtx(t), nodes.WithACMEDirectory(directory))
	if err != nil {
		t.Fatalf("GetACMEMeta(%s): %v", directory, err)
	}
	email := os.Getenv(envACMEAccountMail)
	if email == "" {
		email = "sdk-tests@" + os.Getenv(envACMEDomain)
	}
	ref, err := svc.RegisterACMEAccount(testCtx(t), &nodes.ACMEAccountSpec{
		Name:      account,
		Contact:   []string{email},
		Directory: directory,
		TOSURL:    meta.TermsOfService,
	})
	if err != nil {
		t.Fatalf("RegisterACMEAccount(%s): %v", account, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if _, err := newClient(t).Tasks().Wait(ctx, ref); err != nil {
		t.Fatalf("waiting for the account registration: %v", err)
	}
}

// wireNodeForACME points the node at the account and one acmedomain slot,
// restoring whatever was there before on cleanup. Restoring matters: this is the
// node's real certificate configuration, and leaving a lab node pointed at a
// staging account is a trap for the next run.
func wireNodeForACME(t *testing.T, svc *nodes.Service, node, account, domain, pluginID string) {
	t.Helper()
	before, err := svc.GetNodeConfig(testCtx(t), node)
	if err != nil {
		t.Fatalf("GetNodeConfig(%s): %v", node, err)
	}

	if err := svc.SetNodeConfig(testCtx(t), node, &nodes.NodeConfigUpdate{
		ACME:        &nodes.NodeACME{Account: account},
		ACMEDomains: []nodes.ACMEDomain{{Index: 0, Domain: domain, Plugin: pluginID}},
		Digest:      before.Digest,
	}); err != nil {
		t.Fatalf("SetNodeConfig(%s): %v", node, err)
	}
	t.Cleanup(func() {
		ctx, cancel := cleanupCtx()
		defer cancel()
		restore := &nodes.NodeConfigUpdate{}
		if before.ACME != nil {
			restore.ACME = before.ACME
		} else {
			restore.Delete = append(restore.Delete, "acme")
		}
		// Slot 0 either goes back to what it held or is removed outright.
		var had bool
		for _, d := range before.ACMEDomains {
			if d.Index == 0 {
				restore.ACMEDomains = []nodes.ACMEDomain{d}
				had = true
			}
		}
		if !had {
			restore.Delete = append(restore.Delete, "acmedomain0")
		}
		if err := svc.SetNodeConfig(ctx, node, restore); err != nil {
			t.Errorf("cleanup SetNodeConfig(%s): %v", node, err)
		}
	})

	// Read it back: a typo in the property string surfaces here rather than as
	// an opaque order failure minutes later.
	after, err := svc.GetNodeConfig(testCtx(t), node)
	if err != nil {
		t.Fatalf("GetNodeConfig(%s) after wiring: %v", node, err)
	}
	if after.ACME == nil || after.ACME.Account != account {
		t.Fatalf("node acme account = %+v, want %q", after.ACME, account)
	}
	if len(after.ACMEDomains) == 0 {
		t.Fatal("node has no acmedomain slot after wiring")
	}
	if got := after.ACMEDomains[0]; got.Domain != domain || got.Plugin != pluginID {
		t.Fatalf("acmedomain0 = %+v, want domain %q plugin %q", got, domain, pluginID)
	}
}

// assertServedCertificate dials the node's API port and checks the certificate
// it presents actually covers the domain. This is the end-to-end proof: PVE
// reporting a finished task only means it stored something.
//
// The TLS handshake deliberately skips verification — a Let's Encrypt STAGING
// certificate chains to an untrusted root by design, so verifying would fail on
// a successful run. The check is on the presented SAN, not on trust.
func assertServedCertificate(t *testing.T, domain string) {
	t.Helper()
	dialer := &tls.Dialer{Config: &tls.Config{
		InsecureSkipVerify: true, //nolint:gosec // a staging cert is untrusted by design; the SAN is what is being checked
		MinVersion:         tls.VersionTLS12,
		ServerName:         domain,
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn, err := dialer.DialContext(ctx, "tcp", domain+":8006")
	if err != nil {
		t.Fatalf("dialing %s:8006 for the served certificate: %v", domain, err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			t.Logf("closing the TLS probe: %v", err)
		}
	}()

	state := conn.(*tls.Conn).ConnectionState()
	if len(state.PeerCertificates) == 0 {
		t.Fatal("the node presented no certificate")
	}
	leaf := state.PeerCertificates[0]
	if err := leaf.VerifyHostname(domain); err != nil {
		t.Errorf("served certificate does not cover %s: %v (issuer %q, SANs %v)",
			domain, err, leaf.Issuer.CommonName, leaf.DNSNames)
	}
	// A staging certificate is the whole point: a production one here means the
	// directory selection above silently fell through.
	if !strings.Contains(strings.ToLower(leaf.Issuer.CommonName), "staging") &&
		!strings.Contains(strings.ToLower(leaf.Issuer.Organization[0]), "staging") {
		t.Errorf("certificate issuer %q is not a staging CA — check the directory selection",
			leaf.Issuer.CommonName)
	}
	t.Logf("node serves a certificate for %v issued by %q", leaf.DNSNames, leaf.Issuer.CommonName)
}

// ptr is a local helper for the pointer-valued optional spec fields.
func ptr[T any](v T) *T { return &v }

// compile-time guard that the client type is what the helpers above expect.
var _ = func(c *proxmox.Client) *nodes.Service { return c.Nodes() }
