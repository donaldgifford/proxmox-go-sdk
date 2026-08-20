// This file is intentionally NOT behind the `integration` build tag. The
// recorder helpers below are shared with the tagged live-node harness
// (harness_test.go), and the self-tests at the bottom prove the
// record -> redact -> replay pipeline against the in-process mockpve responder,
// so they run under the default `go test ./...` (and CI) with no live node.

package integration

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"gopkg.in/dnaeon/go-vcr.v4/pkg/cassette"
	"gopkg.in/dnaeon/go-vcr.v4/pkg/recorder"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/api"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/mockpve"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/nodes"
)

// redacted is the placeholder written over every secret before a cassette is
// persisted to disk.
const redacted = "REDACTED"

// Secrets are scrubbed from recorded request/response bodies before save.
// Auth material otherwise leaks into committed fixtures: the token secret rides
// the Authorization header, a mint password rides the ticket-request form, and
// the minted ticket / CSRF token / new token value ride the response body.
var (
	// "key" catches the PEM private key UploadCustomCertificate sends as key=,
	// and any provider apikey= — it does not match sshkeys= (that is "keys=").
	formSecretRe = regexp.MustCompile(`(?i)(password|secret|otp|key)=[^&]*`)
	jsonSecretRe = regexp.MustCompile(`(?i)"(ticket|csrfpreventiontoken|value|password)"\s*:\s*"[^"]*"`)
)

// The ACME plugin's data field carries live DNS-provider credentials (a
// Cloudflare API token) as base64 of KEY=value lines. It rides the request form
// on create/update AND comes back in every plugin read, so both directions need
// scrubbing — and base64 is exactly the shape a human skimming a cassette will
// not recognise as a secret, so this cannot be left to the manual leak review.
//
// The scrub is scoped by URL rather than applied globally: "data" is also the
// name of PVE's response envelope, so a blanket rule would rewrite
// {"data":"UPID:…"} and destroy the task id in every task-returning cassette.
var (
	acmeAccountURLPart = "/cluster/acme/account"
	contactFormRe      = regexp.MustCompile(`contact=[^&]*`)
	// RFC 8555 contacts are mailto URIs inside the CA's account object.
	mailtoRe = regexp.MustCompile(`mailto:[^"\s,\]]+`)

	acmePluginURLPart = "/cluster/acme/plugins"
	acmeDataFormRe    = regexp.MustCompile(`(^|&)data=[^&]*`)
	acmeDataJSONRe    = regexp.MustCompile(`"data"\s*:\s*"[^"]*"`)
)

// uploadBodyTruncatedMarker labels a multipart upload body that was dropped
// before the cassette hit disk (see truncateUploadBody).
const uploadBodyTruncatedMarker = "multipart upload body truncated"

// redactInteraction is the go-vcr BeforeSaveHook. It rewrites credential-bearing
// headers and bodies to the redacted placeholder so a cassette never carries a
// live secret, and truncates streamed multipart upload bodies so an ISO/disk
// image does not bloat the fixture. It runs before the cassette is written, not
// on the wire, so the real request still authenticates and uploads normally.
func redactInteraction(i *cassette.Interaction) error {
	redactHeaders(i.Request.Headers, "Authorization", "Cookie", "Csrfpreventiontoken")
	redactHeaders(i.Response.Headers, "Set-Cookie")

	truncateUploadBody(&i.Request)

	if i.Request.Body != "" {
		i.Request.Body = formSecretRe.ReplaceAllString(i.Request.Body, "${1}="+redacted)
	}
	for key := range i.Request.Form {
		if k := strings.ToLower(key); k == "password" || k == "secret" || k == "otp" {
			i.Request.Form[key] = []string{redacted}
		}
	}

	// Secret fields ride response bodies from more than just /access/ticket: a
	// console mint (POST .../vncproxy or .../spiceproxy) returns a one-time VNC
	// ticket + password, and token creation returns a value. Scrub these field
	// names wherever they appear. This is safe for replay — matchReplayRequest
	// keys on method+path, not body.
	//
	// "value" is the one name that also appears as legitimate DATA: PVE's SMART
	// attribute tables and the qemu pending-config listing both use it. Neither
	// is recorded today, and the pattern only matches a quoted string (SMART
	// values decode as ints), so nothing needed is clobbered — but a future
	// cassette on those surfaces would come back corrupted, and this is the
	// comment that should stop the search.
	if i.Response.Body != "" {
		i.Response.Body = jsonSecretRe.ReplaceAllString(i.Response.Body, `"${1}":"`+redacted+`"`)
	}
	// The reason phrase carries PVE's own error text — "500 create ha rule
	// failed: 400 Rule '...' is invalid." is in a committed cassette — so it can
	// carry a node name, a hostname, or the detail of a failed ACME write. It is
	// stored as its own field, so neither the body scrub nor the topology scrub
	// reached it before.
	if i.Response.Status != "" {
		i.Response.Status = formSecretRe.ReplaceAllString(i.Response.Status, "${1}="+redacted)
		i.Response.Status = jsonSecretRe.ReplaceAllString(i.Response.Status, `"${1}":"`+redacted+`"`)
	}

	redactACMEPluginData(i)
	redactACMEAccountContact(i)
	scrubACMECredentialValues(i)
	return nil
}

// redactACMEAccountContact scrubs the ACME account's contact address under
// /cluster/acme/account, in the register/update form and in the CA account
// object a read returns (PVE passes that through verbatim, contacts included).
//
// The topology scrub also rewrites the address when PVE_TEST_ACME_ACCOUNT_EMAIL
// is set — but the account is registered ONCE and reused across runs, so a
// re-record takes the "reusing the existing account" path and needs no email
// variable at all. That is precisely the run where a personal address set months
// earlier would ship. Structural beats conditional for something whose absence
// is silent.
//
// The account's location URL is deliberately kept: it names the CA-side account,
// not the operator, and replay reads it back.
func redactACMEAccountContact(i *cassette.Interaction) {
	if !strings.Contains(i.Request.URL, acmeAccountURLPart) {
		return
	}
	i.Request.Body = contactFormRe.ReplaceAllString(i.Request.Body, "contact="+redacted)
	if _, ok := i.Request.Form["contact"]; ok {
		i.Request.Form["contact"] = []string{redacted}
	}
	i.Response.Body = mailtoRe.ReplaceAllString(i.Response.Body, "mailto:"+redacted)
}

// acmeCredentialEnvs name the environment variables holding the live DNS
// provider credentials the ACME tests use. Their values are scrubbed from every
// interaction regardless of URL.
var acmeCredentialEnvs = []string{
	"PVE_TEST_ACME_CF_TOKEN",
	"PVE_TEST_ACME_CF_ACCOUNT_ID",
	"PVE_TEST_ACME_NC_API_KEY",
	"PVE_TEST_ACME_NC_USERNAME",
}

// scrubACMECredentialValues is the belt to redactACMEPluginData's braces: it
// scrubs the credential VALUES everywhere they appear, not only under the plugin
// URL.
//
// It also tries the base64 of each value, but that half is opportunistic and
// must not be relied on: the SDK base64-encodes the WHOLE blob (sorted KEY=value
// lines), and base64 works in 3-byte groups, so base64(value) is a substring of
// base64(blob) only when the value happens to land on a 3-aligned offset and run
// to the end. For a multi-field provider it usually does not. The URL-scoped
// rule is what actually covers the encoded blob; this is for a raw value echoed
// somewhere else.
//
// The URL-scoped rule covers every request the SDK makes with a credential in
// it, but says nothing about a response that echoes one back from somewhere
// else. The concrete worry is a failed order: tasks.Wait surfaces the failure
// with its log tail, which means reading /nodes/{node}/tasks/{upid}/log — a URL
// the scoped rule ignores, on the run you are most likely to be recording while
// debugging. Whether PVE's acme wrapper can put a provider credential in that
// log is unproven; this makes the answer not matter.
func scrubACMECredentialValues(i *cassette.Interaction) {
	var skipped []string
	for _, name := range acmeCredentialEnvs {
		value := os.Getenv(name)
		// Short values would scrub half the cassette; a real API token is long.
		// A Namecheap username is not, though, so a SET variable that falls under
		// the floor is announced rather than silently unprotected — the operator
		// is the only one who can decide whether that value matters.
		if len(value) < minCredentialLen {
			if value != "" {
				skipped = append(skipped, name)
			}
			continue
		}
		for _, form := range []string{value, base64.StdEncoding.EncodeToString([]byte(value))} {
			i.Request.URL = strings.ReplaceAll(i.Request.URL, form, redacted)
			i.Request.Body = strings.ReplaceAll(i.Request.Body, form, redacted)
			i.Response.Body = strings.ReplaceAll(i.Response.Body, form, redacted)
			for key, values := range i.Request.Form {
				for n, v := range values {
					i.Request.Form[key][n] = strings.ReplaceAll(v, form, redacted)
				}
			}
		}
	}
	if len(skipped) > 0 {
		shortCredentialWarning.Do(func() {
			fmt.Fprintf(os.Stderr,
				"recorder: NOT value-scrubbing %s — under %d characters, "+
					"so scrubbing it would shred unrelated text. Check the cassette by hand.\n",
				strings.Join(skipped, ", "), minCredentialLen)
		})
	}
}

// minCredentialLen is the floor below which a value is too short to scrub by
// value without corrupting the cassette around it.
const minCredentialLen = 12

// shortCredentialWarning keeps the skipped-value warning to one line per run
// rather than one per recorded interaction.
var shortCredentialWarning sync.Once

// redactACMEPluginData scrubs the ACME plugin credential blob in all three
// places it appears: the request form body, go-vcr's separately-stored parsed
// Form map (the gap the 2026-07-23 leak review found for node names), and the
// response body of a plugin read.
//
// Under the URL guard the JSON pattern is safe for both read shapes — the
// envelope is {"data":[ for a list and {"data":{ for a single plugin, and
// requiring :" means only the inner string field matches.
func redactACMEPluginData(i *cassette.Interaction) {
	if !strings.Contains(i.Request.URL, acmePluginURLPart) {
		return
	}
	if i.Request.Body != "" {
		i.Request.Body = acmeDataFormRe.ReplaceAllString(i.Request.Body, "${1}data="+redacted)
	}
	if v, ok := i.Request.Form["data"]; ok && len(v) > 0 {
		i.Request.Form["data"] = []string{redacted}
	}
	if i.Response.Body != "" {
		i.Response.Body = acmeDataJSONRe.ReplaceAllString(i.Response.Body, `"data":"`+redacted+`"`)
	}
}

func redactHeaders(h http.Header, keys ...string) {
	for _, k := range keys {
		if len(h.Values(k)) > 0 {
			h.Set(k, redacted)
		}
	}
}

// truncateUploadBody drops the body of a streamed multipart upload (ISO / disk
// image) before it is written to a cassette. Left intact, a single ISO upload
// bloats the fixture by megabytes of base64 (an 8.4 MB cassette for a 4 MB ISO)
// for no replay value: matchReplayRequest keys on method+path, not body, and the mock
// corpus is built from the *response* (the import task), not the uploaded bytes.
// The multipart Content-Type (with its boundary) and the response are preserved,
// so the recording still faithfully shows the request shape.
func truncateUploadBody(r *cassette.Request) {
	if !strings.HasPrefix(r.Headers.Get("Content-Type"), "multipart/form-data") || r.Body == "" {
		return
	}
	r.Body = fmt.Sprintf("[%s: %d bytes]", uploadBodyTruncatedMarker, len(r.Body))
	r.ContentLength = int64(len(r.Body))
}

// Topology placeholders. A committed cassette must not expose lab topology (the
// live endpoint host/IP and node name), so a recording rewrites them to these
// stable, RFC-friendly stand-ins. The host placeholder keeps PVE's default port.
const (
	placeholderHost     = "pve.example:8006"
	placeholderBareHost = "pve.example"
	placeholderNode     = "pve"
	// The ACME tests certify a real FQDN in a zone Donald controls, and it
	// reaches a cassette in more places than the plugin credentials do: the
	// node's acmedomain config, the order task's log, the certificate's SAN
	// list, and the DNS-01 challenge records the CA reports. Scrubbing it is
	// not optional cleanup after the fact.
	placeholderACMEDomain = "pve.acme.example"
	placeholderACMEZone   = "acme.example"
	// The account contact and the caller address a DNS provider allowlists are
	// not credentials — they are identifying data about Donald, low-entropy
	// enough that the credential scrub skips them by design, and not derived
	// from the endpoint, so the topology pairs miss them too.
	placeholderACMEContact  = "acme@pve.acme.example"
	placeholderACMESourceIP = "192.0.2.10"
)

// topologyScrub rewrites live topology values to fixed placeholders across a
// recorded interaction's URL and bodies, as an ordered list of
// live → placeholder pairs. Beyond the endpoint host and node name, cluster
// responses can carry the OTHER members' IPs (corosync ring addresses, status
// entries) and the site DNS domain (Phase 0 set real fqdns like
// pve1-dogfood.<site-domain>), so extra pairs ride in via PVE_SCRUB_EXTRA
// (withExtraPairs). The node also rides response-body UPIDs (UPID:<node>:…)
// and the task-poll URLs the SDK derives from them, so it must be replaced
// everywhere for a replay to stay internally consistent. The zero value (no
// pairs) is a no-op, so unit tests and the mockpve self-tests record verbatim.
//
// What this does NOT promise: scrubbing all topology. It knows one endpoint and
// one node; sibling node names and cluster names reach committed cassettes and
// are accepted policy (TESTING.md's review checklist lists node names, IPs, MACs
// and storage names as details the reviewer signs off on). Anything beyond the
// one node is the operator's call via PVE_SCRUB_EXTRA. Nor does it reach inside
// base64: a certificate PEM carries its subject and SANs in DER, where no string
// pair matches — so a test that reads certificates back needs its own rule that
// replaces the PEM wholesale, not a pair.
type topologyScrub struct {
	pairs [][2]string // ordered {live, placeholder} replacements.
}

// newTopologyScrub derives the scrub from a live endpoint URL and node name.
// The host:port pair precedes the bare-host pair, so the ":port" form is never
// left dangling.
func newTopologyScrub(endpoint, node string) topologyScrub {
	var s topologyScrub
	if u, err := url.Parse(endpoint); err == nil && u.Host != "" {
		s.pairs = append(s.pairs, [2]string{u.Host, placeholderHost})
		if h, _, herr := net.SplitHostPort(u.Host); herr == nil {
			s.pairs = append(s.pairs, [2]string{h, placeholderBareHost})
		}
	}
	if node != "" {
		s.pairs = append(s.pairs, [2]string{node, placeholderNode})
	}
	return s
}

// withACMEDomain returns the scrub extended with the ACME test domain: the FQDN
// being certified, and its parent zone when that is still more than one label
// (the zone shows up on its own in challenge and CAA records).
//
// A node is usually the first label of the FQDN it certifies (pve1-dogfood in
// pve1-dogfood.lab.example.com), so a node pair applied before the domain pair
// would rewrite that label, leave the domain pair matching nothing, and publish
// the zone. apply sorts longest-live-value-first, which is what prevents that;
// the pairs are prepended here as well so the intent survives a reader who has
// not got to apply yet, not because ordering at this point decides anything.
func (s topologyScrub) withACMEDomain(domain string) topologyScrub {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return s
	}
	pairs := [][2]string{{domain, placeholderACMEDomain}}
	// Every parent suffix, not just the immediate one: a lab domain is commonly
	// three levels deep (pve1.dogfood.example.dev), and the registrable domain
	// at the top is the half that identifies the operator. It appears on its own
	// wherever the provider talks about the zone rather than the record — an
	// ACME worker's task log, which tasks.Wait reads and the cassette keeps.
	// Stop before the public suffix: a bare TLD carries nothing and rewriting it
	// would corrupt every unrelated hostname in the interaction.
	for zone := domain; ; {
		_, parent, ok := strings.Cut(zone, ".")
		if !ok || !strings.Contains(parent, ".") {
			break
		}
		pairs = append(pairs, [2]string{parent, placeholderACMEZone})
		zone = parent
	}
	s.pairs = append(pairs, s.pairs...)
	return s
}

// minScrubPairLen is the shortest live value a PVE_SCRUB_EXTRA pair may carry.
const minScrubPairLen = 3

// withACMEIdentity returns the scrub extended with the ACME account contact
// address and the source IP a DNS provider allowlists (Namecheap requires one).
// Both reach a cassette on their own: the contact rides the CA account object
// that GET /cluster/acme/account/{name} returns verbatim, and the source IP is
// an ISP-assigned address that can surface in a provider's error text inside an
// order task log.
//
// Neither is caught by the other two mechanisms — they are not high-entropy
// secrets, so scrubACMECredentialValues skips them, and they are not derived
// from the endpoint. Call order does not matter: apply sorts longest-first, so
// a contact ending in the certified domain is rewritten whole.
func (s topologyScrub) withACMEIdentity(contact, sourceIP string) topologyScrub {
	if contact = strings.TrimSpace(contact); contact != "" {
		s.pairs = append(s.pairs, [2]string{contact, placeholderACMEContact})
	}
	if sourceIP = strings.TrimSpace(sourceIP); sourceIP != "" {
		s.pairs = append(s.pairs, [2]string{sourceIP, placeholderACMESourceIP})
	}
	return s
}

// withExtraPairs returns the scrub extended with live=placeholder pairs from a
// CSV (the PVE_SCRUB_EXTRA shape pvelab writes into .pvelab.env), e.g.
// "10.0.0.12=192.0.2.11,lab.internal=lab.example". Empty entries are skipped;
// a malformed entry errors so a typo cannot silently leak topology.
func (s topologyScrub) withExtraPairs(csv string) (topologyScrub, error) {
	for entry := range strings.SplitSeq(csv, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		live, placeholder, ok := strings.Cut(entry, "=")
		if !ok || live == "" || placeholder == "" {
			return s, fmt.Errorf("scrub pair %q: want live=placeholder", entry)
		}
		// A one- or two-character live value is a typo, not topology: it would
		// match inside unrelated words and shred the cassette, and a scrub that
		// mangles everything is as unreviewable as one that scrubs nothing.
		if len(live) < minScrubPairLen {
			return s, fmt.Errorf("scrub pair %q: live value must be at least %d characters",
				entry, minScrubPairLen)
		}
		s.pairs = append(s.pairs, [2]string{live, placeholder})
	}
	return s, nil
}

func (s topologyScrub) apply(i *cassette.Interaction) {
	if len(s.pairs) == 0 {
		return
	}
	// Longest live value first, always. Pairs overlap by nature here — a node
	// name is a label of the FQDN it certifies, a bare host sits inside
	// host:port, a contact address ends in the domain — and if the shorter pair
	// runs first it rewrites part of the longer one, which then matches nothing
	// and the remainder ships. Sorting makes that structural rather than
	// something each caller has to arrange by construction.
	pairs := slices.Clone(s.pairs)
	slices.SortStableFunc(pairs, func(a, b [2]string) int { return len(b[0]) - len(a[0]) })
	rep := func(v string) string {
		if v == "" {
			return v
		}
		for _, p := range pairs {
			v = strings.ReplaceAll(v, p[0], p[1])
		}
		return v
	}
	i.Request.URL = rep(i.Request.URL)
	i.Request.Body = rep(i.Request.Body)
	i.Response.Body = rep(i.Response.Body)
	// The HTTP reason phrase is a separate field carrying PVE's error text
	// verbatim, and a failing write is exactly when a hostname or node name ends
	// up in it. TestScrubCoversEveryStringField keeps this list honest.
	i.Response.Status = rep(i.Response.Status)
	// The Host header is stored separately from the URL and carries the live
	// endpoint verbatim — found in review of the first nested-cluster cassette
	// (2026-07-12; the earlier committed batch was hand-fixed without noticing
	// the automated gap). Scrub it and every header value (Location and
	// friends can carry the endpoint too).
	i.Request.Host = rep(i.Request.Host)
	// go-vcr stores the parsed form alongside the raw body; scrubbing one but
	// not the other leaves the live value in the cassette — found in review of
	// the 2026-07-23 pvelab batch (`node=pve` in the body next to the live
	// node name in the form map).
	for _, vals := range i.Request.Form {
		for idx := range vals {
			vals[idx] = rep(vals[idx])
		}
	}
	for _, h := range []http.Header{i.Request.Headers, i.Response.Headers} {
		for _, vals := range h {
			for idx := range vals {
				vals[idx] = rep(vals[idx])
			}
		}
	}
}

// matchReplayRequest matches a replay request to a recorded interaction on method
// plus URL path+query, deliberately ignoring scheme and host. Recording rewrites
// the host to a placeholder (topologyScrub), so a committed cassette's host no
// longer equals any live/CI endpoint; matching on the path (which the SDK builds
// from the node + resource, both already topology-scrubbed) lets a replay run
// against any endpoint. Headers/body are ignored too, since redaction rewrites
// the Authorization header and credential bodies.
func matchReplayRequest(r *http.Request, i cassette.Request) bool { //nolint:gocritic // signature fixed by cassette.MatcherFunc
	if r.Method != i.Method {
		return false
	}
	iu, err := url.Parse(i.URL)
	if err != nil {
		return false
	}
	return r.URL.Path == iu.Path && r.URL.RawQuery == iu.RawQuery
}

// newRecorder builds a go-vcr recorder for cassetteName (without the .yaml
// suffix) with secret redaction and topology scrubbing wired in. Callers own
// Stop(); newRecorderClient wraps this with a t.Cleanup for the common case.
func newRecorder(t *testing.T, cassetteName string, mode recorder.Mode, realTransport http.RoundTripper, scrub topologyScrub) *recorder.Recorder {
	t.Helper()
	if mode == recorder.ModeRecordOnly {
		if err := os.MkdirAll(filepath.Dir(cassetteName), 0o750); err != nil {
			t.Fatalf("create cassette dir: %v", err)
		}
	}
	if realTransport == nil {
		realTransport = http.DefaultTransport
	}
	// Redact secrets first, then scrub topology, so the placeholder host/node are
	// written over an already-secret-free interaction.
	beforeSave := func(i *cassette.Interaction) error {
		if err := redactInteraction(i); err != nil {
			return err
		}
		scrub.apply(i)
		return nil
	}
	rec, err := recorder.New(cassetteName,
		recorder.WithMode(mode),
		recorder.WithRealTransport(realTransport),
		recorder.WithHook(beforeSave, recorder.BeforeSaveHook),
		recorder.WithMatcher(matchReplayRequest),
		recorder.WithSkipRequestLatency(true),
		// NOTE: WithReplayableInteractions is deliberately NOT set. A task-status
		// poll loop makes many identical GETs to /tasks/{upid}/status; replayable
		// interactions serve the first recording for all of them, so in record
		// mode the network is only hit once and the task is frozen at its first
		// state ("running") forever — Wait then never sees "stopped". Leaving it
		// off records every poll as its own sequential interaction (running…,
		// stopped) and replays them in order, one consumption each.
	)
	if err != nil {
		t.Fatalf("new recorder for %q: %v", cassetteName, err)
	}
	return rec
}

// newRecorderClient returns an *http.Client backed by go-vcr for cassetteName,
// flushing (and redacting) the cassette on test cleanup. In record mode it
// proxies to realTransport; in replay mode it serves recorded interactions and
// needs no network.
func newRecorderClient(t *testing.T, cassetteName string, mode recorder.Mode, realTransport http.RoundTripper, scrub topologyScrub) *http.Client {
	t.Helper()
	rec := newRecorder(t, cassetteName, mode, realTransport, scrub)
	t.Cleanup(func() {
		if serr := rec.Stop(); serr != nil {
			t.Errorf("stop recorder for %q: %v", cassetteName, serr)
		}
	})
	return rec.GetDefaultClient()
}

// TestRedactInteraction is the security-critical unit test: it feeds the
// BeforeSaveHook an interaction carrying a token secret, a mint password, and a
// response ticket, and asserts every one is scrubbed while the non-secret body
// survives.
func TestRedactInteraction(t *testing.T) {
	const secret = "token-secret-xyz"
	i := &cassette.Interaction{
		Request: cassette.Request{
			URL:     "https://pve:8006/api2/json/access/ticket",
			Method:  http.MethodPost,
			Body:    "username=root@pam&password=hunter2",
			Form:    map[string][]string{"password": {"hunter2"}},
			Headers: http.Header{"Authorization": {"PVEAPIToken=root@pam!sdk=" + secret}},
		},
		Response: cassette.Response{
			Body:    `{"data":{"ticket":"PVE:root@pam:DEADBEEF","CSRFPreventionToken":"abc:def","username":"root@pam"}}`,
			Headers: http.Header{"Set-Cookie": {"PVEAuthCookie=PVE:root@pam:DEADBEEF"}},
		},
	}

	if err := redactInteraction(i); err != nil {
		t.Fatalf("redactInteraction: %v", err)
	}

	joined := i.Request.Body + i.Response.Body +
		strings.Join(i.Request.Headers["Authorization"], "") +
		strings.Join(i.Response.Headers["Set-Cookie"], "") +
		strings.Join(i.Request.Form["password"], "")
	for _, leak := range []string{secret, "hunter2", "DEADBEEF", "abc:def"} {
		if strings.Contains(joined, leak) {
			t.Errorf("secret %q survived redaction: %q", leak, joined)
		}
	}
	// A non-secret field must be preserved so replay still matches.
	if !strings.Contains(i.Response.Body, "root@pam") {
		t.Error("non-secret response field was clobbered")
	}
}

// TestRedactConsoleTicket guards the gap that leaked a live VNC ticket: a console
// mint (POST .../vncproxy) returns a one-time ticket + password in its response
// body under a NON-credential URL, so redaction keyed on /access/ticket missed
// them. The ticket and password must be scrubbed regardless of the URL, while a
// non-secret field (port) survives.
func TestRedactConsoleTicket(t *testing.T) {
	const (
		vncTicket = `8T:,O)X\:PVEVNC:6A4BB5CD::VDV71nhRWkraSECRETdata+/==`
		vncPass   = `8T:,O)X\`
	)
	i := &cassette.Interaction{
		Request: cassette.Request{
			URL:    "https://pve:8006/api2/json/nodes/pve/qemu/9102/vncproxy",
			Method: http.MethodPost,
		},
		Response: cassette.Response{
			Body: `{"data":{"port":"5900","ticket":"` + vncTicket + `","upid":"UPID:x","password":"` + vncPass + `"}}`,
		},
	}
	if err := redactInteraction(i); err != nil {
		t.Fatalf("redactInteraction: %v", err)
	}
	for _, leak := range []string{vncTicket, vncPass} {
		if strings.Contains(i.Response.Body, leak) {
			t.Errorf("console secret survived redaction: %q in %q", leak, i.Response.Body)
		}
	}
	if !strings.Contains(i.Response.Body, `"port":"5900"`) {
		t.Errorf("non-secret port field was clobbered: %q", i.Response.Body)
	}
}

// TestScrubTopology proves the recording rewrites the live endpoint host:port,
// its bare host, and the node name to stable placeholders across the request URL
// and both bodies — including the node inside a response-body UPID, so the
// task-poll URL the SDK later derives stays consistent — while leaving unrelated
// text alone.
func TestScrubTopology(t *testing.T) {
	scrub := newTopologyScrub("https://10.10.11.20:8006", "r740a")
	i := &cassette.Interaction{
		Request: cassette.Request{
			Method:  http.MethodPost,
			URL:     "https://10.10.11.20:8006/api2/json/nodes/r740a/qemu/100/status/start",
			Host:    "10.10.11.20:8006",
			Headers: http.Header{"Referer": {"https://10.10.11.20:8006/"}},
			// go-vcr stores the parsed form next to the raw body; both must
			// scrub (found 2026-07-23: a migrate's form kept the live node).
			Body: "node=r740a",
			Form: url.Values{"node": {"r740a"}},
		},
		Response: cassette.Response{
			Body:    `{"data":"UPID:r740a:0005:...:qmstart:100:root@pam!sdk:"}`,
			Headers: http.Header{"Location": {"https://10.10.11.20:8006/next"}},
		},
	}
	scrub.apply(i)

	for _, leak := range []string{"10.10.11.20", "r740a"} {
		if strings.Contains(i.Request.URL, leak) || strings.Contains(i.Response.Body, leak) {
			t.Errorf("topology %q survived scrub: url=%q body=%q", leak, i.Request.URL, i.Response.Body)
		}
		if strings.Contains(i.Request.Host, leak) ||
			strings.Contains(i.Request.Headers.Get("Referer"), leak) ||
			strings.Contains(i.Response.Headers.Get("Location"), leak) {
			t.Errorf("topology %q survived scrub in Host/headers: host=%q", leak, i.Request.Host)
		}
		if strings.Contains(i.Request.Body, leak) || strings.Contains(i.Request.Form.Get("node"), leak) {
			t.Errorf("topology %q survived scrub in body/form: body=%q form=%q",
				leak, i.Request.Body, i.Request.Form.Get("node"))
		}
	}
	if got := i.Request.Form.Get("node"); got != placeholderNode {
		t.Errorf("scrubbed form node = %q, want %q", got, placeholderNode)
	}
	if i.Request.Host != placeholderHost {
		t.Errorf("scrubbed Host = %q, want %q", i.Request.Host, placeholderHost)
	}
	if !strings.Contains(i.Request.URL, "https://"+placeholderHost+"/api2/json/nodes/"+placeholderNode+"/") {
		t.Errorf("scrubbed URL = %q, want placeholder host+node", i.Request.URL)
	}
	if !strings.Contains(i.Response.Body, "UPID:"+placeholderNode+":") {
		t.Errorf("scrubbed UPID body = %q, want placeholder node", i.Response.Body)
	}
	// The token id is not topology and must survive (it is not a secret).
	if !strings.Contains(i.Response.Body, "root@pam!sdk") {
		t.Errorf("scrubbed body dropped the token id: %q", i.Response.Body)
	}

	// The zero scrub is a no-op (mockpve self-tests record verbatim).
	blank := &cassette.Interaction{Request: cassette.Request{URL: "https://127.0.0.1:9/x"}}
	topologyScrub{}.apply(blank)
	if blank.Request.URL != "https://127.0.0.1:9/x" {
		t.Errorf("zero scrub altered URL: %q", blank.Request.URL)
	}
}

// TestScrubTopologyMultiPair covers the Phase 3 nested-cluster shape: beyond
// the endpoint (pve1) and node, a cluster response carries the OTHER members'
// IPs (corosync ring addresses) and the site DNS domain inside fqdns —
// PVE_SCRUB_EXTRA pairs must rewrite them all, and a malformed pair must error
// rather than silently leak.
// TestScrubACMEDomain covers the Phase-4 recording shape: the certified FQDN
// reaches a cassette through the node's acmedomain config, the order task log,
// and the issued certificate's SANs, and the node name is the FQDN's first
// label — the ordering hazard the prepend exists for. The zone must go too:
// a DNS-01 challenge names _acme-challenge.<zone>, which publishes the zone on
// its own even after the FQDN is rewritten.
// scrubExemptFields names the serialized cassette fields topologyScrub does not
// rewrite, each with the reason it is safe. Everything else must be reached, and
// TestScrubCoversEveryStringField fails when a new field appears here or in a
// go-vcr upgrade — the gap that let Response.Status ship PVE's error text
// verbatim was not a bug in any rule, it was a field nobody had enumerated.
var scrubExemptFields = map[string]string{
	// Fixed protocol values, never topology.
	"Request.Proto":  "HTTP/1.1",
	"Response.Proto": "HTTP/1.1",
	"Request.Method": "the HTTP verb",
	// Set only on SERVER-side requests; Go leaves both empty on a client
	// request, and all 17 committed cassettes confirm they are absent.
	"Request.RemoteAddr": "server-side only",
	"Request.RequestURI": "server-side only",
}

// TestScrubCoversEveryStringField walks the cassette structs by reflection and
// asserts every string field either gets rewritten or is exempt with a stated
// reason. It is deliberately structural: the pipeline is an allowlist, and an
// allowlist with no completeness check is one dependency upgrade away from
// silently letting a new field through.
func TestScrubCoversEveryStringField(t *testing.T) {
	const live = "10.0.0.11"
	scrub := newTopologyScrub("https://"+live+":8006", "pve1-dogfood")

	for _, side := range []struct {
		name string
		typ  reflect.Type
	}{
		{"Request", reflect.TypeOf(cassette.Request{})},
		{"Response", reflect.TypeOf(cassette.Response{})},
	} {
		for idx := range side.typ.NumField() {
			f := side.typ.Field(idx)
			if f.Type.Kind() != reflect.String {
				continue // headers/forms are covered by their own assertions below
			}
			qualified := side.name + "." + f.Name
			if reason, ok := scrubExemptFields[qualified]; ok {
				if reason == "" {
					t.Errorf("%s is exempt with no stated reason", qualified)
				}
				continue
			}

			i := &cassette.Interaction{}
			target := reflect.ValueOf(i).Elem().FieldByName(side.name).FieldByName(f.Name)
			target.SetString("host " + live + " failed")
			scrub.apply(i)
			if strings.Contains(target.String(), live) {
				t.Errorf("%s is not scrubbed (%q survived) — add a rule in apply, "+
					"or add it to scrubExemptFields with the reason it is safe",
					qualified, target.String())
			}
		}
	}

	// The two map-shaped fields, asserted directly: reflection above skips them.
	i := &cassette.Interaction{
		Request: cassette.Request{
			Form:    map[string][]string{"node": {live}},
			Headers: http.Header{"X-Probe": []string{live}},
		},
		Response: cassette.Response{Headers: http.Header{"Location": []string{live}}},
	}
	scrub.apply(i)
	if i.Request.Form.Get("node") == live {
		t.Error("Request.Form is not scrubbed")
	}
	if i.Request.Headers.Get("X-Probe") == live {
		t.Error("Request.Headers is not scrubbed")
	}
	if i.Response.Headers.Get("Location") == live {
		t.Error("Response.Headers is not scrubbed")
	}
}

// TestRedactACMEAccountContact pins the structural contact scrub: it must fire
// on an account read whether or not the operator set the email variable, which
// is the case the env-derived topology pair misses (the account is registered
// once and reused, so a re-record has no reason to set it).
func TestRedactACMEAccountContact(t *testing.T) {
	i := &cassette.Interaction{
		Request: cassette.Request{
			Method: http.MethodGet,
			URL:    "https://pve.example:8006/api2/json/cluster/acme/account/staging",
		},
		Response: cassette.Response{
			Body: `{"data":{"location":"https://acme-staging.example/acct/12345",` +
				`"account":{"status":"valid","contact":["mailto:donald@personal.example"]}}}`,
		},
	}
	if err := redactInteraction(i); err != nil {
		t.Fatalf("redactInteraction: %v", err)
	}
	if strings.Contains(i.Response.Body, "donald@personal.example") {
		t.Errorf("account body = %q, want the contact redacted", i.Response.Body)
	}
	if !strings.Contains(i.Response.Body, "acme-staging.example/acct/12345") {
		t.Errorf("account body = %q, want the CA location kept", i.Response.Body)
	}

	// The register form carries it too, and a mailto: in an unrelated response
	// must be left alone — the rule is URL-scoped for the same reason the plugin
	// data rule is.
	reg := &cassette.Interaction{
		Request: cassette.Request{
			Method: http.MethodPost,
			URL:    "https://pve.example:8006/api2/json/cluster/acme/account",
			Body:   "contact=donald%40personal.example&directory=https%3A%2F%2Facme",
			Form:   map[string][]string{"contact": {"donald@personal.example"}},
		},
	}
	if err := redactInteraction(reg); err != nil {
		t.Fatalf("redactInteraction(register): %v", err)
	}
	if strings.Contains(reg.Request.Body, "personal.example") ||
		strings.Contains(strings.Join(reg.Request.Form["contact"], ","), "personal.example") {
		t.Errorf("register body = %q form = %v, want the contact redacted",
			reg.Request.Body, reg.Request.Form)
	}

	other := &cassette.Interaction{
		Request:  cassette.Request{URL: "https://pve.example:8006/api2/json/cluster/status"},
		Response: cassette.Response{Body: `{"data":"mailto:root@pam"}`},
	}
	if err := redactInteraction(other); err != nil {
		t.Fatalf("redactInteraction(other): %v", err)
	}
	if !strings.Contains(other.Response.Body, "mailto:root@pam") {
		t.Errorf("unrelated body = %q, want it untouched", other.Response.Body)
	}
}

// TestRedactStatusLine pins the reason-phrase scrub against the shape that is
// already in a committed cassette: PVE answers a rejected write with its own
// error text in the status line, where neither the body scrub nor a header rule
// reaches it.
func TestRedactStatusLine(t *testing.T) {
	i := &cassette.Interaction{Response: cassette.Response{
		Status: "500 create ha rule failed: password=hunter2 rejected",
	}}
	if err := redactInteraction(i); err != nil {
		t.Fatalf("redactInteraction: %v", err)
	}
	if strings.Contains(i.Response.Status, "hunter2") {
		t.Errorf("status = %q, want the secret redacted", i.Response.Status)
	}
}

func TestScrubACMEDomain(t *testing.T) {
	scrub := newTopologyScrub("https://10.0.0.11:8006", "pve1-dogfood").
		withACMEDomain("pve1-dogfood.lab.example.com")

	i := &cassette.Interaction{
		Request: cassette.Request{
			Method: http.MethodPut,
			URL:    "https://10.0.0.11:8006/api2/json/nodes/pve1-dogfood/config",
			Body:   "acmedomain0=pve1-dogfood.lab.example.com%2Cplugin%3Dcf",
		},
		Response: cassette.Response{
			Body: `{"data":"UPID:pve1-dogfood:00001234:acmeorder::root@pam:` +
				`_acme-challenge.lab.example.com TXT added; ` +
				`SAN=pve1-dogfood.lab.example.com"}`,
		},
	}
	scrub.apply(i)

	for _, leak := range []string{"pve1-dogfood.lab.example.com", "lab.example.com", "10.0.0.11"} {
		if strings.Contains(i.Request.URL+i.Request.Body, leak) || strings.Contains(i.Response.Body, leak) {
			t.Errorf("%q survived scrub: url=%q body=%q resp=%q",
				leak, i.Request.URL, i.Request.Body, i.Response.Body)
		}
	}
	if !strings.Contains(i.Request.Body, placeholderACMEDomain) {
		t.Errorf("scrubbed request body = %q, want the domain placeholder", i.Request.Body)
	}
	if !strings.Contains(i.Response.Body, "_acme-challenge."+placeholderACMEZone) {
		t.Errorf("scrubbed response = %q, want the zone placeholder", i.Response.Body)
	}
	if !strings.Contains(i.Response.Body, "UPID:"+placeholderNode+":") {
		t.Errorf("scrubbed response = %q, want the placeholder node in the UPID", i.Response.Body)
	}

	// A single-label parent is left alone: rewriting "com" would shred every
	// unrelated hostname in the cassette.
	shallow := newTopologyScrub("", "").withACMEDomain("host.example")
	j := &cassette.Interaction{Response: cassette.Response{Body: "host.example and example alone"}}
	shallow.apply(j)
	if j.Response.Body != placeholderACMEDomain+" and example alone" {
		t.Errorf("shallow-domain scrub = %q", j.Response.Body)
	}
	// An unset env var is a no-op, not an empty-string replacement.
	if got := (topologyScrub{}).withACMEDomain(""); len(got.pairs) != 0 {
		t.Errorf("withACMEDomain(\"\") added %d pair(s), want 0", len(got.pairs))
	}
}

// TestScrubACMEIdentity covers the two values that are neither credentials nor
// endpoint-derived: the account contact (returned verbatim inside the CA
// account object) and the provider's allowlisted source IP (Donald's egress
// address, which a provider error can echo into an order task log). The contact
// deliberately ends in the certified domain — the case that proves apply's
// longest-first ordering, since the domain pair would otherwise rewrite the
// tail and strand the local part.
func TestScrubACMEIdentity(t *testing.T) {
	scrub := newTopologyScrub("https://10.0.0.11:8006", "pve1-dogfood").
		withACMEDomain("pve1-dogfood.lab.example.com").
		withACMEIdentity("sdk-tests@lab.example.com", "203.0.113.45")

	i := &cassette.Interaction{
		Response: cassette.Response{
			Body: `{"data":{"account":{"contact":["mailto:sdk-tests@lab.example.com"]},` +
				`"log":"NAMECHEAP_SOURCE_IP 203.0.113.45 not allowlisted"}}`,
		},
	}
	scrub.apply(i)

	for _, leak := range []string{"sdk-tests@lab.example.com", "203.0.113.45", "lab.example.com"} {
		if strings.Contains(i.Response.Body, leak) {
			t.Errorf("%q survived scrub: %q", leak, i.Response.Body)
		}
	}
	if !strings.Contains(i.Response.Body, "mailto:"+placeholderACMEContact) {
		t.Errorf("scrubbed body = %q, want the contact placeholder intact", i.Response.Body)
	}
	if !strings.Contains(i.Response.Body, placeholderACMESourceIP) {
		t.Errorf("scrubbed body = %q, want the source-IP placeholder", i.Response.Body)
	}
	// Unset env vars add nothing.
	if got := (topologyScrub{}).withACMEIdentity("", ""); len(got.pairs) != 0 {
		t.Errorf("withACMEIdentity(\"\", \"\") added %d pair(s), want 0", len(got.pairs))
	}
}

func TestScrubTopologyMultiPair(t *testing.T) {
	scrub, err := newTopologyScrub("https://10.0.0.11:8006", "pve1-dogfood").
		withExtraPairs("10.0.0.12=192.0.2.11, 10.0.0.13=192.0.2.12,lab.internal=lab.example")
	if err != nil {
		t.Fatalf("withExtraPairs: %v", err)
	}

	i := &cassette.Interaction{
		Request: cassette.Request{
			Method: http.MethodGet,
			URL:    "https://10.0.0.11:8006/api2/json/cluster/status",
		},
		Response: cassette.Response{
			Body: `{"data":[` +
				`{"type":"node","name":"pve1-dogfood","ip":"10.0.0.11"},` +
				`{"type":"node","name":"pve2-dogfood","ip":"10.0.0.12"},` +
				`{"type":"node","name":"pve3-dogfood","ip":"10.0.0.13"},` +
				`{"fqdn":"pve2-dogfood.lab.internal","ring0_addr":"10.0.0.12"}]}`,
		},
	}
	scrub.apply(i)

	for _, leak := range []string{"10.0.0.11", "10.0.0.12", "10.0.0.13", "lab.internal"} {
		if strings.Contains(i.Request.URL, leak) || strings.Contains(i.Response.Body, leak) {
			t.Errorf("topology %q survived scrub: url=%q body=%q", leak, i.Request.URL, i.Response.Body)
		}
	}
	for _, want := range []string{`"ip":"192.0.2.11"`, `"ip":"192.0.2.12"`, "pve2-dogfood.lab.example"} {
		if !strings.Contains(i.Response.Body, want) {
			t.Errorf("scrubbed body = %q, want %q", i.Response.Body, want)
		}
	}

	// A malformed entry errors — a typo must not silently leak topology.
	if _, err := (topologyScrub{}).withExtraPairs("10.0.0.12"); err == nil {
		t.Error("withExtraPairs with a pairless entry succeeded, want error")
	}
	// So does a live value too short to be topology: matching "1" everywhere
	// would corrupt the whole cassette.
	if _, err := (topologyScrub{}).withExtraPairs("1=192.0.2.11"); err == nil {
		t.Error("withExtraPairs with a one-character live value succeeded, want error")
	}
	if _, err := (topologyScrub{}).withExtraPairs("=x"); err == nil {
		t.Error("withExtraPairs with an empty live value succeeded, want error")
	}
	// An empty CSV (env var unset) is fine.
	if _, err := (topologyScrub{}).withExtraPairs(""); err != nil {
		t.Errorf("withExtraPairs(\"\") = %v, want nil", err)
	}
}

// TestTruncateUploadBody proves the BeforeSaveHook drops a multipart upload body
// (so an ISO does not bloat the cassette) while leaving a non-multipart body
// alone and preserving the multipart Content-Type for replay fidelity.
func TestTruncateUploadBody(t *testing.T) {
	bigISO := strings.Repeat("A", 4<<20) // 4 MiB stand-in for an uploaded ISO.
	i := &cassette.Interaction{
		Request: cassette.Request{
			URL:     "https://pve:8006/api2/json/nodes/pve/storage/local/upload",
			Method:  http.MethodPost,
			Body:    "--b\r\nContent-Disposition: form-data; name=\"content\"\r\n\r\niso\r\n--b\r\n" + bigISO + "\r\n--b--",
			Headers: http.Header{"Content-Type": {"multipart/form-data; boundary=b"}},
		},
	}
	if err := redactInteraction(i); err != nil {
		t.Fatalf("redactInteraction: %v", err)
	}
	if strings.Contains(i.Request.Body, bigISO) {
		t.Error("multipart upload body survived; the ISO bytes reached the cassette")
	}
	if !strings.Contains(i.Request.Body, uploadBodyTruncatedMarker) {
		t.Errorf("truncated body = %q, want the truncation marker", i.Request.Body)
	}
	if i.Request.ContentLength != int64(len(i.Request.Body)) {
		t.Errorf("ContentLength = %d, want %d (the truncated length)", i.Request.ContentLength, len(i.Request.Body))
	}
	// The multipart Content-Type must survive so the recording still shows the
	// request was an upload.
	if got := i.Request.Headers.Get("Content-Type"); !strings.HasPrefix(got, "multipart/form-data") {
		t.Errorf("Content-Type = %q, want multipart/form-data preserved", got)
	}

	// A non-multipart body (a normal form POST) must be left untouched.
	plain := &cassette.Interaction{
		Request: cassette.Request{
			Method:  http.MethodPost,
			Body:    "vmid=100&name=web",
			Headers: http.Header{"Content-Type": {"application/x-www-form-urlencoded"}},
		},
	}
	if err := redactInteraction(plain); err != nil {
		t.Fatalf("redactInteraction(plain): %v", err)
	}
	if plain.Request.Body != "vmid=100&name=web" {
		t.Errorf("non-multipart body was altered: %q", plain.Request.Body)
	}
}

// TestRecorderRecordReplay proves the full go-vcr pipeline against mockpve:
// record a real interaction, confirm the token secret never reaches disk, then
// replay it with the server shut down. No live PVE node is required, so this
// guards the harness the live capture relies on.
func TestRecorderRecordReplay(t *testing.T) {
	const secret = "s3cr3t-token-value-do-not-leak"

	mock := mockpve.New()
	mock.AddVM("pve", 100, "web", "running")
	ts := mock.Serve()

	ctx := context.Background()
	creds := api.TokenCredentials("root@pam!sdk", secret)
	cassettePath := filepath.Join(t.TempDir(), "selftest")

	// --- Record against the live mockpve server, flushing explicitly. ---
	rec := newRecorder(t, cassettePath, recorder.ModeRecordOnly, http.DefaultTransport, topologyScrub{})
	c, err := proxmox.NewClient(ctx, ts.URL, creds, proxmox.WithHTTPClient(rec.GetDefaultClient()))
	if err != nil {
		t.Fatalf("record NewClient: %v", err)
	}
	recorded, err := c.QEMU("pve").Get(ctx, 100)
	if err != nil {
		t.Fatalf("record Get: %v", err)
	}
	if recorded.Status != "running" {
		t.Fatalf("record status = %q, want running", recorded.Status)
	}
	if serr := rec.Stop(); serr != nil {
		t.Fatalf("flush cassette: %v", serr)
	}
	ts.Close() // replay must not be able to reach the server

	// --- Assert redaction reached disk. ---
	data, err := os.ReadFile(cassettePath + ".yaml")
	if err != nil {
		t.Fatalf("read cassette: %v", err)
	}
	if bytes.Contains(data, []byte(secret)) {
		t.Fatal("SECURITY: token secret leaked into the recorded cassette")
	}
	if !bytes.Contains(data, []byte(redacted)) {
		t.Error("expected the REDACTED marker in the cassette")
	}

	// --- Replay with the server gone; the recorded data must come back. ---
	repClient := newRecorderClient(t, cassettePath, recorder.ModeReplayOnly, nil, topologyScrub{})
	c2, err := proxmox.NewClient(ctx, ts.URL, creds, proxmox.WithHTTPClient(repClient))
	if err != nil {
		t.Fatalf("replay NewClient: %v", err)
	}
	replayed, err := c2.QEMU("pve").Get(ctx, 100)
	if err != nil {
		t.Fatalf("replay Get (server is down): %v", err)
	}
	if replayed.Status != recorded.Status {
		t.Errorf("replay status = %q, want %q", replayed.Status, recorded.Status)
	}
}

// TestRecorderPasswordAuthRedaction is the password-credential twin of
// TestRecorderRecordReplay: it records a REAL password-auth exchange (the
// /access/ticket mint UserCredentials performs, plus an authenticated read)
// through the recorder against mockpve and asserts neither the password nor
// the minted ticket/CSRF material reaches the cassette on disk. The pvelab
// nested cluster authenticates the suite this way (PVE_USERNAME/PVE_PASSWORD),
// so this guards every password-auth cassette before one is ever committed.
func TestRecorderPasswordAuthRedaction(t *testing.T) {
	const password = "hunter2-do-not-leak"

	mock := mockpve.New()
	mock.AddUser("root@pam", password)
	mock.AddVM("pve", 100, "web", "running")
	ts := mock.Serve()
	defer ts.Close()

	ctx := context.Background()
	cassettePath := filepath.Join(t.TempDir(), "password-auth")

	rec := newRecorder(t, cassettePath, recorder.ModeRecordOnly, http.DefaultTransport, topologyScrub{})
	c, err := proxmox.NewClient(ctx, ts.URL, api.UserCredentials("root@pam", password, ""),
		proxmox.WithHTTPClient(rec.GetDefaultClient()))
	if err != nil {
		t.Fatalf("record NewClient (password auth): %v", err)
	}
	if _, err := c.QEMU("pve").Get(ctx, 100); err != nil {
		t.Fatalf("record Get: %v", err)
	}
	if serr := rec.Stop(); serr != nil {
		t.Fatalf("flush cassette: %v", serr)
	}

	data, err := os.ReadFile(cassettePath + ".yaml")
	if err != nil {
		t.Fatalf("read cassette: %v", err)
	}
	if bytes.Contains(data, []byte(password)) {
		t.Fatal("SECURITY: password leaked into the recorded cassette")
	}
	// The minted ticket rides the /access/ticket response body plus the Cookie
	// header of subsequent requests, and the CSRF token rides a request header;
	// none may survive. mockpve mints "mock-ticket-<user>"/"mock-csrf-<user>"
	// (mockpve/handlers.go), so asserting on those prefixes proves the real
	// minted values were scrubbed — not just that a pattern never occurred.
	for _, leak := range []string{"mock-ticket-", "mock-csrf-"} {
		if bytes.Contains(data, []byte(leak)) {
			t.Errorf("ticket material %q survived into the cassette", leak)
		}
	}
	if !bytes.Contains(data, []byte(redacted)) {
		t.Error("expected the REDACTED marker in the cassette")
	}
}

// TestRedactACMEPluginData is the guard that must exist BEFORE the first live
// ACME recording, not after: a plugin's data field is a live Cloudflare API
// token, cassettes are committed to git, and base64 is exactly what a human
// leak review fails to recognise. It pins all three places the blob appears —
// the request body, go-vcr's parsed Form map, and the plugin read's response.
func TestRedactACMEPluginData(t *testing.T) {
	// base64("CF_Token=live-cloudflare-token"), as the SDK renders it.
	const blob = "Q0ZfVG9rZW49bGl2ZS1jbG91ZGZsYXJlLXRva2Vu"
	create := &cassette.Interaction{
		Request: cassette.Request{
			URL:    "https://pve:8006/api2/json/cluster/acme/plugins",
			Method: http.MethodPost,
			Body:   "id=cf-lab&type=dns&api=cf&data=" + blob,
			Form:   map[string][]string{"data": {blob}, "api": {"cf"}},
		},
		Response: cassette.Response{Body: `{"data":null}`},
	}
	if err := redactInteraction(create); err != nil {
		t.Fatalf("redactInteraction: %v", err)
	}
	if strings.Contains(create.Request.Body, blob) {
		t.Errorf("credential blob survived in the request body: %q", create.Request.Body)
	}
	if strings.Contains(strings.Join(create.Request.Form["data"], ""), blob) {
		t.Errorf("credential blob survived in the parsed Form map: %v", create.Request.Form)
	}
	// Non-secret parameters must survive so the cassette still documents the
	// request shape.
	for _, keep := range []string{"id=cf-lab", "type=dns", "api=cf"} {
		if !strings.Contains(create.Request.Body, keep) {
			t.Errorf("%q was clobbered: %q", keep, create.Request.Body)
		}
	}

	// A read returns the stored credential — PVE does not treat data as
	// write-only — in both the single and list shapes.
	for _, tc := range []struct{ name, body string }{
		{"single", `{"data":{"plugin":"cf-lab","api":"cf","data":"` + blob + `"}}`},
		{"list", `{"data":[{"plugin":"cf-lab","api":"cf","data":"` + blob + `"}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			read := &cassette.Interaction{
				Request: cassette.Request{
					URL:    "https://pve:8006/api2/json/cluster/acme/plugins/cf-lab",
					Method: http.MethodGet,
				},
				Response: cassette.Response{Body: tc.body},
			}
			if err := redactInteraction(read); err != nil {
				t.Fatalf("redactInteraction: %v", err)
			}
			if strings.Contains(read.Response.Body, blob) {
				t.Errorf("credential blob survived in a read: %q", read.Response.Body)
			}
			if !strings.Contains(read.Response.Body, "cf-lab") {
				t.Errorf("the plugin id was clobbered: %q", read.Response.Body)
			}
		})
	}
}

// TestRedactACMEDataSpareUPID is the other half of the ACME scrub: it must be
// URL-scoped. "data" is also the name of PVE's response envelope, so a blanket
// rule would rewrite {"data":"UPID:…"} and break tasks.Wait on replay for every
// existing task-returning cassette.
func TestRedactACMEDataSpareUPID(t *testing.T) {
	const upid = "UPID:pve:0005:qmstart:100:root@pam!sdk:"
	i := &cassette.Interaction{
		Request: cassette.Request{
			URL:    "https://pve:8006/api2/json/nodes/pve/qemu/100/status/start",
			Method: http.MethodPost,
		},
		Response: cassette.Response{Body: `{"data":"` + upid + `"}`},
	}
	if err := redactInteraction(i); err != nil {
		t.Fatalf("redactInteraction: %v", err)
	}
	if !strings.Contains(i.Response.Body, upid) {
		t.Errorf("the UPID envelope was clobbered by the ACME scrub: %q", i.Response.Body)
	}
}

// TestScrubACMECredentialValues covers the leak the URL-scoped rule cannot: a
// credential echoed back under a URL that has nothing to do with ACME plugins.
// A failed order's task log is the realistic case, and a failed order is exactly
// what you re-record while debugging.
func TestScrubACMECredentialValues(t *testing.T) {
	const token = "cf-live-token-abcdefghijklmnop"
	t.Setenv("PVE_TEST_ACME_CF_TOKEN", token)

	i := &cassette.Interaction{
		Request: cassette.Request{
			URL:    "https://pve:8006/api2/json/nodes/pve/tasks/UPID:x/log",
			Method: http.MethodGet,
		},
		Response: cassette.Response{
			Body: `{"data":[{"n":1,"t":"using CF_Token=` + token + ` for the challenge"}]}`,
		},
	}
	if err := redactInteraction(i); err != nil {
		t.Fatalf("redactInteraction: %v", err)
	}
	if strings.Contains(i.Response.Body, token) {
		t.Errorf("credential survived in a task log: %q", i.Response.Body)
	}
	if !strings.Contains(i.Response.Body, "for the challenge") {
		t.Errorf("the surrounding log line was clobbered: %q", i.Response.Body)
	}

	// The base64 form is scrubbed too, since that is how it rides the wire.
	encoded := base64.StdEncoding.EncodeToString([]byte(token))
	j := &cassette.Interaction{
		Request:  cassette.Request{URL: "https://pve:8006/api2/json/somewhere/else"},
		Response: cassette.Response{Body: `{"data":{"blob":"` + encoded + `"}}`},
	}
	if err := redactInteraction(j); err != nil {
		t.Fatalf("redactInteraction: %v", err)
	}
	if strings.Contains(j.Response.Body, encoded) {
		t.Errorf("base64 credential survived: %q", j.Response.Body)
	}
}

// TestScrubACMEIgnoresShortValues guards the other direction: a short or empty
// env value must not turn into a scrub that rewrites unrelated text.
func TestScrubACMEIgnoresShortValues(t *testing.T) {
	t.Setenv("PVE_TEST_ACME_CF_ACCOUNT_ID", "abc")

	body := `{"data":[{"node":"pve","status":"abc"}]}`
	i := &cassette.Interaction{
		Request:  cassette.Request{URL: "https://pve:8006/api2/json/nodes"},
		Response: cassette.Response{Body: body},
	}
	if err := redactInteraction(i); err != nil {
		t.Fatalf("redactInteraction: %v", err)
	}
	if i.Response.Body != body {
		t.Errorf("a short env value scrubbed unrelated content: %q", i.Response.Body)
	}
}

// TestRecorderACMEFlowRedaction rehearses Phase 4's leak review without a node.
//
// Every other ACME redaction test here hands a hand-built cassette.Interaction
// to a hook and checks the struct that comes back. That proves the rules are
// right; it does not prove the bytes on disk are clean, and those are two
// different claims. A credential can survive in a field the recorder serializes
// but no hook visits — which is exactly how Response.Status went unscrubbed
// through sixteen committed cassettes. So this records the ACME flow through
// the real recorder against mockpve and greps the resulting YAML.
//
// The flow is the one the live run performs: register a plugin carrying a
// provider credential, read it back (PVE returns the credential base64-encoded
// in data, so the read is a second chance to leak), and point a node's config
// at the domain. The credential is checked base64 because that is the only form
// that ever reaches the wire — the SDK encodes it before sending — so the raw
// check is there to catch a future change to that encoding, not today's bytes.
//
// Both halves were verified by mutation: disabling redactACMEPluginData leaks
// the encoded credential, and dropping the domain pair publishes the zone even
// though the node pair still matches its first label. That second one is the
// ordering rationale in withACMEDomain, demonstrated rather than asserted.
func TestRecorderACMEFlowRedaction(t *testing.T) {
	const (
		token    = "cf-scoped-token-do-not-leak-abcdef123456"
		liveZone = "acme-live.example.net"
		pluginID = "cf-rehearsal"
		// The node is the first label of the FQDN it certifies — the ordering
		// hazard withACMEDomain documents, rehearsed here rather than asserted
		// on a synthetic interaction.
		liveNode = "pve1-dogfood"
	)
	liveDomain := liveNode + "." + liveZone

	mock := mockpve.New()
	ts := mock.Serve()
	defer ts.Close()

	ctx := context.Background()
	creds := api.TokenCredentials("root@pam!sdk", "unrelated-api-token-secret")
	cassettePath := filepath.Join(t.TempDir(), "acme-rehearsal")

	scrub := newTopologyScrub(ts.URL, liveNode).withACMEDomain(liveDomain)
	rec := newRecorder(t, cassettePath, recorder.ModeRecordOnly, http.DefaultTransport, scrub)
	c, err := proxmox.NewClient(ctx, ts.URL, creds, proxmox.WithHTTPClient(rec.GetDefaultClient()))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	svc := c.Nodes()

	if err := svc.CreateACMEPlugin(ctx, &nodes.ACMEPluginSpec{
		ID:   pluginID,
		Data: nodes.ACMECloudflare{Token: token},
	}); err != nil {
		t.Fatalf("CreateACMEPlugin: %v", err)
	}
	if _, err := svc.GetACMEPlugin(ctx, pluginID); err != nil {
		t.Fatalf("GetACMEPlugin: %v", err)
	}
	if err := svc.SetNodeConfig(ctx, liveNode, &nodes.NodeConfigUpdate{
		ACMEDomains: []nodes.ACMEDomain{{Index: 0, Domain: liveDomain, Plugin: pluginID}},
	}); err != nil {
		t.Fatalf("SetNodeConfig: %v", err)
	}
	if err := rec.Stop(); err != nil {
		t.Fatalf("flush cassette: %v", err)
	}

	data, err := os.ReadFile(cassettePath + ".yaml")
	if err != nil {
		t.Fatalf("read cassette: %v", err)
	}
	for _, leak := range []struct{ what, value string }{
		{"provider credential", token},
		{"base64 provider credential", base64.StdEncoding.EncodeToString([]byte("CF_Token=" + token))},
		{"certified FQDN", liveDomain},
		{"DNS zone", liveZone},
		{"node name", liveNode},
	} {
		if bytes.Contains(data, []byte(leak.value)) {
			t.Errorf("SECURITY: %s reached the recorded cassette", leak.what)
		}
	}
	// A cassette with everything stripped would pass the checks above while
	// being useless, so confirm the interactions are still there and carry the
	// placeholders the scrub promises.
	for _, want := range []string{redacted, placeholderACMEDomain, "/cluster/acme/plugins"} {
		if !bytes.Contains(data, []byte(want)) {
			t.Errorf("cassette is missing %q — recorded nothing, or scrubbed too much", want)
		}
	}
}

// TestScrubACMEDomainDeepZone covers the lab's actual shape: a node under a
// delegated subdomain, where the registrable domain at the top is the part that
// names the operator. Scrubbing only the immediate parent leaves it in the
// cassette.
func TestScrubACMEDomainDeepZone(t *testing.T) {
	scrub := newTopologyScrub("https://10.0.0.9:8006", "pve1").
		withACMEDomain("pve1.dogfood.example.dev")

	i := &cassette.Interaction{}
	i.Response.Body = "ordered pve1.dogfood.example.dev; zone dogfood.example.dev " +
		"delegated from example.dev; unrelated host cdn.other.dev stays"
	scrub.apply(i)

	for _, leak := range []string{"dogfood.example.dev", "example.dev"} {
		if strings.Contains(i.Response.Body, leak) {
			t.Errorf("%q survived the scrub: %q", leak, i.Response.Body)
		}
	}
	// The TLD is not a pair, so a hostname in an unrelated domain is untouched
	// apart from its own suffix — proof the walk stops at the public suffix
	// instead of rewriting every ".dev" in sight.
	if !strings.Contains(i.Response.Body, "cdn.other.dev") {
		t.Errorf("an unrelated hostname was rewritten: %q", i.Response.Body)
	}
}
