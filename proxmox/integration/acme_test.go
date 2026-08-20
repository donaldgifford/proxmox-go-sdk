//go:build integration

package integration

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/nodes"
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

	// envACMEDisposable is a deliberate second gate on the ordering tests,
	// mirroring PVE_TEST_HA_ARM. Credentials alone must not be enough to fire
	// an order: the harness autoloads a repo-root .env, and that file points at
	// the REAL node — so a set of ACME variables added to the wrong file would
	// otherwise replace a production pveproxy certificate with an untrusted
	// staging one. Setting this is the operator saying "this node is
	// disposable", which no environment can imply on its own.
	envACMEDisposable = "PVE_TEST_ACME_DISPOSABLE"
)

// TestACMEDNSCloudflare is the flagship live run: wire a node for DNS-01 through
// Cloudflare, order a staging certificate, and prove the served certificate
// actually covers the requested name.
func TestACMEDNSCloudflare(t *testing.T) {
	token := os.Getenv(envACMECFToken)
	if token == "" {
		t.Skipf("Cloudflare ACME test disabled (set %s, %s and %s)",
			envACMECFToken, envACMEDomain, envACMEDisposable)
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
		t.Skipf("Namecheap ACME test disabled (set %s, %s, %s, %s and %s)",
			envACMENCUsername, envACMENCAPIKey, envACMENCSourceIP, envACMEDomain,
			envACMEDisposable)
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
	domain := os.Getenv(envACMEDomain)
	if domain == "" {
		t.Skipf("ACME test disabled (set %s to an FQDN in the provider's zone)", envACMEDomain)
	}
	if os.Getenv(envReplay) != "1" && os.Getenv(envACMEDisposable) != "1" {
		t.Skipf("ordering is gated on %s=1 — an ACME order REPLACES this node's "+
			"pveproxy certificate with an untrusted staging one, so set it only for a "+
			"disposable node (pvelab), never r740a", envACMEDisposable)
	}
	c := newClient(t)
	node := testNode()
	svc := c.Nodes()
	account := "sdk-staging"

	stagingURL := stagingDirectory(t, svc)
	t.Logf("ordering %s on node %s via plugin %s at %s", domain, node, pluginID, stagingURL)

	// 1. The staging account. Registering is a task; a leftover account from a
	// previous run is reused rather than treated as a failure.
	registerStagingAccount(t, c, account, stagingURL)

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

	// The certificate is now installed and pveproxy is serving it, so the revoke
	// is registered as a cleanup BEFORE the assertion that could abort the test.
	// Registered last, it runs first (cleanups are LIFO), ahead of the node
	// config restore. A failure here is reported, not fatal — leaving the node
	// on an untrusted staging certificate is the outcome worth avoiding.
	t.Cleanup(func() { revokeNodeCertificate(t, c, node) })

	// 5. The assertion that matters: the node now SERVES a certificate covering
	// the requested name. Reading it back from the API would only prove PVE
	// stored something.
	assertServedCertificate(t, domain)
}

// revokeNodeCertificate revokes the node's ACME certificate and waits for the
// worker. It tolerates an already-revoked certificate: this runs from a cleanup
// that may follow a failure anywhere in the flow.
func revokeNodeCertificate(t *testing.T, c *proxmox.Client, node string) {
	t.Helper()
	ref, err := c.Nodes().RevokeNodeCertificate(testCtx(t), node)
	if err != nil {
		t.Errorf("RevokeNodeCertificate(%s): %v — the node may still serve the staging certificate", node, err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if _, err := c.Tasks().Wait(ctx, ref); err != nil {
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
func registerStagingAccount(t *testing.T, c *proxmox.Client, account, directory string) {
	t.Helper()
	svc := c.Nodes()
	if _, err := svc.GetACMEAccount(testCtx(t), account); err == nil {
		t.Logf("reusing the existing ACME account %q", account)
		return
	} else if !acmeAccountAbsent(err) {
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
	// Reuse the caller's client: building a second one under PVE_RECORD would
	// start a second recorder on the same cassette path, and go-vcr's
	// record-only mode rewrites the whole file on stop — the two would clobber
	// each other and quietly drop these interactions.
	if _, err := c.Tasks().Wait(ctx, ref); err != nil {
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
//
// It is the one step of the flow that does NOT go through the SDK client, so
// go-vcr never sees it and no cassette can carry it: on replay the only host
// available is the scrub placeholder, which resolves nowhere. Replay therefore
// covers the REST conversation and stops here — the served-certificate proof is
// live-only by construction, like TestConsoleRFB's byte stream.
func assertServedCertificate(t *testing.T, domain string) {
	t.Helper()
	if os.Getenv(envReplay) == "1" {
		t.Log("replay: skipping the TLS probe (no live node; the domain is a scrub placeholder)")
		return
	}
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

	tlsConn, ok := conn.(*tls.Conn)
	if !ok {
		t.Fatalf("expected a TLS connection, got %T", conn)
	}
	state := tlsConn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		t.Fatal("the node presented no certificate")
	}
	leaf := state.PeerCertificates[0]
	if err := leaf.VerifyHostname(domain); err != nil {
		t.Errorf("served certificate does not cover %s: %v (issuer %q, SANs %v)",
			domain, err, leaf.Issuer.CommonName, leaf.DNSNames)
	}
	// A staging certificate is the whole point: a production one here means the
	// directory selection above silently fell through. Issuer.Organization is
	// empty for a DN without an O, so it is appended rather than indexed — this
	// branch only runs when something is already wrong, and a panic here would
	// replace the diagnosis with a stack trace.
	issuer := leaf.Issuer.CommonName
	if len(leaf.Issuer.Organization) > 0 {
		issuer += " " + strings.Join(leaf.Issuer.Organization, " ")
	}
	if !strings.Contains(strings.ToLower(issuer), "staging") {
		t.Errorf("certificate issuer %q is not a staging CA — check the directory selection", issuer)
	}
	t.Logf("node serves a certificate for %v issued by %q", leaf.DNSNames, leaf.Issuer.CommonName)
}

// ptr is a local helper for the pointer-valued optional spec fields.
func ptr[T any](v T) *T { return &v }

// TestACMEPreflight is the cheap, read-only check to run BEFORE the lifecycle
// tests: it verifies the two things that would otherwise fail an expensive
// order, and it does so without ordering anything.
//
//   - The node can reach the CA. GetACMEMeta asks the NODE to fetch the
//     directory's metadata, so a successful call proves outbound reachability to
//     Let's Encrypt staging from where it actually matters — the node, not this
//     workstation (IMPL-0007 Phase 4 task 1).
//   - The typed providers' field names match what the node publishes. That is
//     DESIGN-0006's confirm-live item, and doing it here means finding drift in
//     seconds rather than after a failed DNS-01 exchange (Phase 4 task 2).
//
// A failed ACME order burns Let's Encrypt staging rate limits and needs
// re-recording, so cheap-check-first is worth the extra test.
func TestACMEPreflight(t *testing.T) {
	if os.Getenv(envReplay) == "1" {
		t.Skip("preflight is live-only: it exists to check a real node's reachability")
	}
	c := newClient(t)
	svc := c.Nodes()

	staging := stagingDirectory(t, svc)
	meta, err := svc.GetACMEMeta(testCtx(t), nodes.WithACMEDirectory(staging))
	if err != nil {
		t.Fatalf("the node cannot reach %s: %v\n"+
			"Fix this before ordering — an order would fail after the DNS-01 exchange, "+
			"having already spent a staging rate-limit slot.", staging, err)
	}
	if meta.TermsOfService == "" {
		t.Errorf("%s returned no terms-of-service URL; account registration needs one", staging)
	}
	t.Logf("node reaches %s (terms: %s)", staging, meta.TermsOfService)

	// Confirm the typed providers against what the node publishes.
	schema, err := svc.GetACMEChallengeSchema(testCtx(t))
	if err != nil {
		t.Fatalf("GetACMEChallengeSchema: %v", err)
	}
	for _, provider := range []nodes.ACMEPluginData{
		nodes.ACMECloudflare{},
		nodes.ACMENamecheap{},
	} {
		checkProviderFields(t, schema, provider)
	}
}

// checkProviderFields compares one typed provider's credential keys against the
// field names the node publishes for it.
//
// The asymmetry is deliberate. A key the SDK sends that the provider does not
// know is a real defect — acme.sh ignores it, the challenge fails, and the
// message says nothing about a misspelled variable. A field the provider
// publishes that the SDK does not model is merely an option nobody has needed;
// it is logged, not failed, and ACMERawPluginData reaches it meanwhile.
func checkProviderFields(t *testing.T, schema []nodes.ACMEChallengeSchemaEntry, provider nodes.ACMEPluginData) {
	t.Helper()
	id := provider.API()

	var entry *nodes.ACMEChallengeSchemaEntry
	for i := range schema {
		if schema[i].ID == id {
			entry = &schema[i]
			break
		}
	}
	if entry == nil {
		ids := make([]string, 0, len(schema))
		for _, e := range schema {
			ids = append(ids, e.ID)
		}
		t.Errorf("provider %q is not in the node's challenge schema (%d providers: %s)",
			id, len(schema), strings.Join(ids, ", "))
		return
	}

	live, err := schemaFieldNames(entry.Schema)
	if err != nil {
		// Not Donald's problem to fix in the field: this means the SDK guessed
		// the schema's shape wrong, so hand over the raw JSON to fix it with.
		t.Errorf("cannot read %q's field names — the SDK's assumption about the "+
			"challenge-schema shape is wrong. Raw schema:\n%s\nerror: %v",
			id, entry.Schema, err)
		return
	}

	for key := range provider.Data() {
		if !live[key] {
			names := make([]string, 0, len(live))
			for name := range live {
				names = append(names, name)
			}
			sort.Strings(names)
			t.Errorf("%T sends %q, which %q does not publish. The node's fields are: %s",
				provider, key, id, strings.Join(names, ", "))
		}
	}
	for name := range live {
		if _, ok := provider.Data()[name]; !ok {
			t.Logf("note: %q publishes %q, which %T does not model "+
				"(reachable today via ACMERawPluginData)", id, name, provider)
		}
	}
}

// schemaFieldNames extracts a provider's credential field names from the raw
// challenge schema. The schema is provider-defined and its envelope is not in
// the apidoc, so the two plausible shapes are both tried before giving up:
// PVE's own UI reads a "fields" object, and a bare object of field definitions
// is the obvious alternative.
func schemaFieldNames(raw json.RawMessage) (map[string]bool, error) {
	if len(raw) == 0 {
		return nil, errors.New("empty schema")
	}
	var wrapped struct {
		Fields map[string]json.RawMessage `json:"fields"`
	}
	if err := json.Unmarshal(raw, &wrapped); err == nil && len(wrapped.Fields) > 0 {
		return keySet(wrapped.Fields), nil
	}

	var bare map[string]json.RawMessage
	if err := json.Unmarshal(raw, &bare); err != nil {
		return nil, fmt.Errorf("schema is not a JSON object: %w", err)
	}
	delete(bare, "fields") // present but empty, if we got here.
	if len(bare) == 0 {
		return nil, errors.New("schema declares no fields")
	}
	return keySet(bare), nil
}

func keySet(m map[string]json.RawMessage) map[string]bool {
	out := make(map[string]bool, len(m))
	for k := range m {
		out[k] = true
	}
	return out
}

// TestSchemaFieldNames covers the extraction against both shapes it accepts and
// the ones it must reject. It needs no node, so the parser is tested before the
// live run rather than by it — a parser bug would otherwise surface as a
// confusing failure in the middle of Phase 4.
func TestSchemaFieldNames(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    []string
		wantErr bool
	}{
		{
			name: "wrapped in fields",
			raw:  `{"fields":{"CF_Token":{"type":"string"},"CF_Email":{"type":"string"}}}`,
			want: []string{"CF_Email", "CF_Token"},
		},
		{
			name: "bare object of definitions",
			raw:  `{"CF_Token":{"type":"string"},"CF_Email":{"type":"string"}}`,
			want: []string{"CF_Email", "CF_Token"},
		},
		{
			name: "empty fields falls through to bare and finds nothing",
			raw:  `{"fields":{}}`, wantErr: true,
		},
		{name: "not an object", raw: `["CF_Token"]`, wantErr: true},
		{name: "empty", raw: ``, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := schemaFieldNames(json.RawMessage(tt.raw))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("schemaFieldNames(%s) = %v, want an error", tt.raw, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("schemaFieldNames(%s): %v", tt.raw, err)
			}
			names := make([]string, 0, len(got))
			for name := range got {
				names = append(names, name)
			}
			sort.Strings(names)
			if strings.Join(names, ",") != strings.Join(tt.want, ",") {
				t.Errorf("fields = %v, want %v", names, tt.want)
			}
		})
	}
}
