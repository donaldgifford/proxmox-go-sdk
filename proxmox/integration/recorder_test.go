// This file is intentionally NOT behind the `integration` build tag. The
// recorder helpers below are shared with the tagged live-node harness
// (harness_test.go), and the self-tests at the bottom prove the
// record -> redact -> replay pipeline against the in-process mockpve responder,
// so they run under the default `go test ./...` (and CI) with no live node.

package integration

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
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
	jsonSecretRe = regexp.MustCompile(`(?i)"(ticket|csrfpreventiontoken|value|password)"\s*:\s*"[^"]*"`)
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
	// names wherever they appear. This is safe for replay — matchReplayRequest keys
	// on method+path, not body — and PVE config/listing responses never legitimately
	// carry these keys (they are write-only), so nothing needed is clobbered.
	if i.Response.Body != "" {
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
)

// topologyScrub rewrites the live endpoint host:port and node name to fixed
// placeholders across a recorded interaction's URL and bodies. The node also
// rides response-body UPIDs (UPID:<node>:…) and the task-poll URLs the SDK
// derives from them, so it must be replaced everywhere for a replay to stay
// internally consistent. The zero value (empty fields) is a no-op, so unit tests
// and the mockpve self-test record verbatim.
type topologyScrub struct {
	host string // live "host:port", e.g. "10.10.11.20:8006"
	node string // live node name, e.g. "r740a"
}

// newTopologyScrub derives the scrub from a live endpoint URL and node name.
func newTopologyScrub(endpoint, node string) topologyScrub {
	s := topologyScrub{node: node}
	if u, err := url.Parse(endpoint); err == nil {
		s.host = u.Host
	}
	return s
}

func (s topologyScrub) apply(i *cassette.Interaction) {
	if s.host == "" && s.node == "" {
		return
	}
	bareHost := s.host
	if h, _, err := net.SplitHostPort(s.host); err == nil {
		bareHost = h
	}
	rep := func(v string) string {
		if v == "" {
			return v
		}
		if s.host != "" {
			// host:port before bare host, so the ":port" form is not left dangling.
			v = strings.ReplaceAll(v, s.host, placeholderHost)
			v = strings.ReplaceAll(v, bareHost, placeholderBareHost)
		}
		if s.node != "" {
			v = strings.ReplaceAll(v, s.node, placeholderNode)
		}
		return v
	}
	i.Request.URL = rep(i.Request.URL)
	i.Request.Body = rep(i.Request.Body)
	i.Response.Body = rep(i.Response.Body)
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
			Method: http.MethodPost,
			URL:    "https://10.10.11.20:8006/api2/json/nodes/r740a/qemu/100/status/start",
		},
		Response: cassette.Response{
			Body: `{"data":"UPID:r740a:0005:...:qmstart:100:root@pam!sdk:"}`,
		},
	}
	scrub.apply(i)

	for _, leak := range []string{"10.10.11.20", "r740a"} {
		if strings.Contains(i.Request.URL, leak) || strings.Contains(i.Response.Body, leak) {
			t.Errorf("topology %q survived scrub: url=%q body=%q", leak, i.Request.URL, i.Response.Body)
		}
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
