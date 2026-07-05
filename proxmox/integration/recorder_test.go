// This file is intentionally NOT behind the `integration` build tag. The
// recorder helpers below are shared with the tagged live-node harness
// (harness_test.go), and the self-tests at the bottom prove the
// record -> redact -> replay pipeline against the in-process mockpve responder,
// so they run under the default `go test ./...` (and CI) with no live node.

package integration

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/dnaeon/go-vcr.v4/pkg/cassette"
	"gopkg.in/dnaeon/go-vcr.v4/pkg/recorder"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/api"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/mockpve"
)

// redacted is the placeholder written over every secret before a cassette is
// persisted to disk.
const redacted = "REDACTED"

// Secrets are scrubbed from recorded request/response bodies before save.
// Auth material otherwise leaks into committed fixtures: the token secret rides
// the Authorization header, a mint password rides the ticket-request form, and
// the minted ticket / CSRF token / new token value ride the response body.
var (
	formSecretRe = regexp.MustCompile(`(?i)(password|secret|otp)=[^&]*`)
	jsonSecretRe = regexp.MustCompile(`(?i)"(ticket|csrfpreventiontoken|value)"\s*:\s*"[^"]*"`)
)

// redactInteraction is the go-vcr BeforeSaveHook. It rewrites credential-bearing
// headers and bodies to the redacted placeholder so a cassette never carries a
// live secret. It runs before the cassette is written, not on the wire, so the
// real request still authenticates normally.
func redactInteraction(i *cassette.Interaction) error {
	redactHeaders(i.Request.Headers, "Authorization", "Cookie", "Csrfpreventiontoken")
	redactHeaders(i.Response.Headers, "Set-Cookie")

	if i.Request.Body != "" {
		i.Request.Body = formSecretRe.ReplaceAllString(i.Request.Body, "${1}="+redacted)
	}
	for key := range i.Request.Form {
		if k := strings.ToLower(key); k == "password" || k == "secret" || k == "otp" {
			i.Request.Form[key] = []string{redacted}
		}
	}

	// Only credential-minting endpoints put secrets in the response body; other
	// bodies (VM configs, listings) must survive verbatim for replay to match.
	if isCredentialURL(i.Request.URL) && i.Response.Body != "" {
		i.Response.Body = jsonSecretRe.ReplaceAllString(i.Response.Body, `"${1}":"`+redacted+`"`)
	}
	return nil
}

func redactHeaders(h http.Header, keys ...string) {
	for _, k := range keys {
		if len(h.Values(k)) > 0 {
			h.Set(k, redacted)
		}
	}
}

func isCredentialURL(u string) bool {
	return strings.Contains(u, "/access/ticket") || strings.Contains(u, "/token/")
}

// matchMethodURL matches a replay request to a recorded interaction on method +
// URL only. The default matcher also compares headers and body, but redaction
// rewrites the Authorization header (and credential-request bodies), so those
// no longer equal the live request — method + URL is the redaction-safe key.
// (A write-heavy replay that POSTs different bodies to one URL would need a
// body-aware matcher; deferred until cassettes are wired into CI.)
func matchMethodURL(r *http.Request, i cassette.Request) bool { //nolint:gocritic // signature fixed by cassette.MatcherFunc
	return r.Method == i.Method && r.URL.String() == i.URL
}

// newRecorder builds a go-vcr recorder for cassetteName (without the .yaml
// suffix) with secret redaction wired in. Callers own Stop(); newRecorderClient
// wraps this with a t.Cleanup for the common case.
func newRecorder(t *testing.T, cassetteName string, mode recorder.Mode, realTransport http.RoundTripper) *recorder.Recorder {
	t.Helper()
	if mode == recorder.ModeRecordOnly {
		if err := os.MkdirAll(filepath.Dir(cassetteName), 0o750); err != nil {
			t.Fatalf("create cassette dir: %v", err)
		}
	}
	if realTransport == nil {
		realTransport = http.DefaultTransport
	}
	rec, err := recorder.New(cassetteName,
		recorder.WithMode(mode),
		recorder.WithRealTransport(realTransport),
		recorder.WithHook(redactInteraction, recorder.BeforeSaveHook),
		recorder.WithMatcher(matchMethodURL),
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
func newRecorderClient(t *testing.T, cassetteName string, mode recorder.Mode, realTransport http.RoundTripper) *http.Client {
	t.Helper()
	rec := newRecorder(t, cassetteName, mode, realTransport)
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
	rec := newRecorder(t, cassettePath, recorder.ModeRecordOnly, http.DefaultTransport)
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
	repClient := newRecorderClient(t, cassettePath, recorder.ModeReplayOnly, nil)
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
