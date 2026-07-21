//go:build integration

package integration

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joho/godotenv"
	"gopkg.in/dnaeon/go-vcr.v4/pkg/recorder"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/api"
)

// TestMain autoloads PVE_* credentials from a .env (or .env.local) at the module
// root before the live suite runs, so a contributor can keep their token in a
// file instead of exporting it into every shell. Precedence is the important
// part: if the endpoint plus a complete credential pair are already set in the
// environment, no file is read at all — explicit `export`s, CI-injected secrets, and
// `op run --env-file=… -- go test …` all take priority. A 1Password-mounted .env
// works too, but because it is typically a single-use FIFO, the loader only opens
// it when the creds are otherwise unset (opening a pipe twice would block or
// consume a one-shot secret). The loader never resolves `op://` references
// itself; a file of literal `op://…` refs must be run under `op run`.
func TestMain(m *testing.M) {
	loadDotEnv()
	os.Exit(m.Run())
}

// loadDotEnv fills the PVE credential vars from a dotenv file at the module root,
// but only when they are not already present. Under `go test` the working
// directory is the package dir, so a repo-root .env would otherwise be missed. It
// stats (never opens) each candidate first, so a missing file is skipped and a
// 1Password FIFO is left untouched unless it is actually needed, then stops as
// soon as the creds are satisfied so a second candidate (possibly a single-use
// pipe) is not opened needlessly.
func loadDotEnv() {
	if os.Getenv(envReplay) == "1" {
		return // replay is self-contained (committed cassettes); never read a dotenv
	}
	if credsSet() {
		return // env already has them (explicit export / op run / CI): leave files alone
	}
	root := moduleRoot()
	if root == "" {
		return
	}
	// .env.local (git-ignored, personal) wins over a shared .env.
	for _, name := range []string{".env.local", ".env"} {
		path := filepath.Join(root, name)
		if _, err := os.Stat(path); err != nil {
			continue // missing/unstattable: nothing to load (stat never opens a FIFO)
		}
		if err := godotenv.Load(path); err != nil {
			fmt.Fprintf(os.Stderr, "integration: load %s: %v\n", name, err)
			return
		}
		if credsSet() {
			return
		}
	}
}

// credsSet reports whether the endpoint plus one complete credential pair
// (API token, or username+password) is present.
func credsSet() bool {
	return os.Getenv(envEndpoint) != "" && envCredentials() != nil
}

// envCredentials returns the credential strategy the environment selects: an
// API token when PVE_TOKEN_ID/PVE_TOKEN_SECRET are set, otherwise root-style
// password credentials when PVE_USERNAME/PVE_PASSWORD are set (the pvelab
// nested cluster only has root@pam — API tokens do not survive a cluster
// join), otherwise nil. Token wins when both are configured.
func envCredentials() api.Credentials {
	if id, secret := os.Getenv(envTokenID), os.Getenv(envTokenSecret); id != "" && secret != "" {
		return api.TokenCredentials(id, secret)
	}
	if user, pass := os.Getenv(envUsername), os.Getenv(envPassword); user != "" && pass != "" {
		return api.UserCredentials(user, pass, "")
	}
	return nil
}

// moduleRoot returns the nearest ancestor of the working directory that contains
// a go.mod, or "" if none is found.
func moduleRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// Environment variables the harness reads. Endpoint + token are required;
// without them every test skips, so the suite is a no-op unless a live 9.x node
// is configured (OQ-5).
const (
	envEndpoint    = "PVE_ENDPOINT"     // e.g. https://pve.example:8006
	envTokenID     = "PVE_TOKEN_ID"     // e.g. root@pam!sdk
	envTokenSecret = "PVE_TOKEN_SECRET" // the token's secret UUID
	envUsername    = "PVE_USERNAME"     // password auth, e.g. root@pam (used when PVE_TOKEN_* is absent)
	envPassword    = "PVE_PASSWORD"     // password auth; pairs with PVE_USERNAME
	envNode        = "PVE_NODE"         // node under test, default "pve"
	envInsecureTLS = "PVE_INSECURE_TLS" // "1" to skip TLS verify (self-signed)
	envRecord      = "PVE_RECORD"       // "1" to record go-vcr cassettes while running
	envReplay      = "PVE_REPLAY"       // "1" to replay committed cassettes (no live node; CI)
	envDebug       = "PVE_DEBUG"        // "1" to stream a debug line per SDK request to stderr
	envScrubExtra  = "PVE_SCRUB_EXTRA"  // CSV of extra live=placeholder scrub pairs for recording (pvelab writes it)

	// Destructive-test gates. Absent -> the corresponding test skips.
	envTestStorage     = "PVE_TEST_STORAGE"      // target storage for a scratch guest disk / uploads
	envTestISOStorage  = "PVE_TEST_ISO_STORAGE"  // storage that allows "iso" content for the upload test; falls back to PVE_TEST_STORAGE
	envTestVMID        = "PVE_TEST_VMID"         // scratch QEMU VMID the suite may create/destroy
	envTestConsoleVMID = "PVE_TEST_CONSOLE_VMID" // scratch QEMU VMID for the console-mint test (distinct so it can run alongside the lifecycle tests)
	envTestLXCVMID     = "PVE_TEST_LXC_VMID"     // scratch LXC VMID the suite may create/destroy
	envTestLXCTemplate = "PVE_TEST_LXC_TEMPLATE" // OS template volid, e.g. local:vztmpl/debian-12-...tar.zst
	envTestISOPath     = "PVE_TEST_ISO_PATH"     // local path to a (small) ISO to upload (Phase 3)
	envFabricNodes     = "PVE_TEST_FABRIC_NODES" // CSV of >= 2 node names for TestSDNFabricLifecycle (the pvelab cluster)
	envFabricIface     = "PVE_TEST_FABRIC_IFACE" // fabric-facing interface name on every fabric node, e.g. ens19
	// PVE_TEST_PLACEMENT_VMID_1/2 gate TestResourceAffinityPlacement; they are
	// read by name there (placementVMID) since each is a one-shot gate.
	// PVE_TEST_HA_SIDS was retired with TestResourceAffinityRule (design OQ-9:
	// the placement test supersedes the rule-only read-back).
)

// newClient builds a client for a test. In replay mode (PVE_REPLAY=1) it is
// backed by the committed cassette for the running test — no live node, this is
// what CI runs. Otherwise it builds a live client from the environment, skipping
// the test when the node/token are not configured. Safe to call from every test.
func newClient(t *testing.T) *proxmox.Client {
	t.Helper()
	if os.Getenv(envReplay) == "1" {
		return newReplayClient(t)
	}
	return newLiveClient(t, true)
}

// newStreamClient builds a live client that BYPASSES record mode even when
// PVE_RECORD=1: a websocket upgrade (console.Connect) hijacks the connection
// and the raw duplex stream cannot ride go-vcr's request/response model, so a
// stream-carrying test runs live-only and deliberately leaves no cassette
// (design OQ-6). Callers must skip under PVE_REPLAY themselves.
func newStreamClient(t *testing.T) *proxmox.Client {
	t.Helper()
	return newLiveClient(t, false)
}

// newLiveClient builds a client against the configured live node, recording to
// the running test's cassette when record is true and PVE_RECORD=1.
func newLiveClient(t *testing.T, record bool) *proxmox.Client {
	t.Helper()
	endpoint := os.Getenv(envEndpoint)
	creds := envCredentials()
	if endpoint == "" || creds == nil {
		t.Skipf("live PVE node not configured (set %s plus %s/%s or %s/%s)",
			envEndpoint, envTokenID, envTokenSecret, envUsername, envPassword)
	}

	insecure := os.Getenv(envInsecureTLS) == "1"

	var opts []proxmox.Option
	switch {
	case record && os.Getenv(envRecord) == "1":
		// Recording: the SDK must use the go-vcr client, which bypasses the
		// SDK's own TLS options, so the insecure choice is applied to the
		// recorder's real transport instead.
		rt := http.DefaultTransport
		if insecure {
			rt = insecureTransport()
		}
		// Scrub the live endpoint host and node name — plus any extra
		// live=placeholder pairs (the other cluster members' IPs and the site
		// DNS domain, via PVE_SCRUB_EXTRA) — from the cassette so a committed
		// fixture does not expose lab topology.
		scrub, serr := newTopologyScrub(endpoint, testNode()).withExtraPairs(os.Getenv(envScrubExtra))
		if serr != nil {
			t.Fatalf("%s: %v", envScrubExtra, serr)
		}
		client := newRecorderClient(t, cassetteName(t), recorder.ModeRecordOnly, rt, scrub)
		opts = append(opts, proxmox.WithHTTPClient(client))
	case insecure:
		opts = append(opts, proxmox.WithInsecureSkipVerify(true))
	}

	// PVE_DEBUG=1 streams one debug line per SDK request to stderr (method+URL),
	// so a silent task-poll loop is visible while diagnosing a slow/stuck step.
	if os.Getenv(envDebug) == "1" {
		opts = append(opts, proxmox.WithLogger(
			slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	c, err := proxmox.NewClient(ctx, endpoint, creds, opts...)
	if err != nil {
		t.Fatalf("NewClient(%s): %v", endpoint, err)
	}
	return c
}

// newReplayClient builds a client backed by the committed go-vcr cassette for the
// running test (ModeReplayOnly), so the integration suite runs in CI with no live
// node. It dials the placeholder endpoint the cassettes were scrubbed to; the
// host-agnostic matcher (matchReplayRequest) makes the real host irrelevant, and
// ReplayOnly never touches the network. A test whose cassette is missing fails
// (rather than skips) — the CI job -run's only the tests that have one.
func newReplayClient(t *testing.T) *proxmox.Client {
	t.Helper()
	client := newRecorderClient(t, cassetteName(t), recorder.ModeReplayOnly, nil, topologyScrub{})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	c, err := proxmox.NewClient(ctx, "https://"+placeholderHost,
		api.TokenCredentials("root@pam!sdk", "replay-token"), proxmox.WithHTTPClient(client))
	if err != nil {
		t.Fatalf("replay NewClient (cassette %s.yaml): %v", cassetteName(t), err)
	}
	return c
}

// cassetteDir is where recorded cassettes live, relative to this package.
const cassetteDir = "testdata/cassettes"

// cassetteName maps the running test to its cassette path (without .yaml), so
// each test records to and replays from its own fixture.
func cassetteName(t *testing.T) string {
	t.Helper()
	return filepath.Join(cassetteDir, strings.ReplaceAll(t.Name(), "/", "_"))
}

// insecureTransport is the real transport used while recording against a live
// self-signed node (PVE_INSECURE_TLS=1); it mirrors the SDK's own opt-in.
func insecureTransport() http.RoundTripper {
	return &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12}, //nolint:gosec // opt-in for self-signed PVE, matches the SDK
	}
}

// testNode returns the node under test (PVE_NODE, default "pve").
func testNode() string {
	if n := os.Getenv(envNode); n != "" {
		return n
	}
	return "pve"
}

// testCtx returns a per-test context with a generous timeout and cleanup.
func testCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// cleanupCtx returns a bounded context for teardown. Cleanup runs after the test
// body (often after a failure), so it cannot use testCtx's already-cancelled
// context, but it must not use a bare context.Background() either: a wedged
// delete/wait would then poll until the test binary's 10-minute timeout instead
// of failing fast. The caller must cancel.
func cleanupCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 90*time.Second)
}
