//go:build integration

package integration

import (
	"context"
	"crypto/tls"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/dnaeon/go-vcr.v4/pkg/recorder"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/api"
)

// Environment variables the harness reads. Endpoint + token are required;
// without them every test skips, so the suite is a no-op unless a live 9.x node
// is configured (OQ-5).
const (
	envEndpoint    = "PVE_ENDPOINT"     // e.g. https://pve.example:8006
	envTokenID     = "PVE_TOKEN_ID"     // e.g. root@pam!sdk
	envTokenSecret = "PVE_TOKEN_SECRET" // the token's secret UUID
	envNode        = "PVE_NODE"         // node under test, default "pve"
	envInsecureTLS = "PVE_INSECURE_TLS" // "1" to skip TLS verify (self-signed)
	envRecord      = "PVE_RECORD"       // "1" to record go-vcr cassettes while running

	// Destructive-test gates. Absent -> the corresponding test skips.
	envTestStorage     = "PVE_TEST_STORAGE"      // target storage for a scratch guest disk / uploads
	envTestVMID        = "PVE_TEST_VMID"         // scratch QEMU VMID the suite may create/destroy
	envTestLXCVMID     = "PVE_TEST_LXC_VMID"     // scratch LXC VMID the suite may create/destroy
	envTestLXCTemplate = "PVE_TEST_LXC_TEMPLATE" // OS template volid, e.g. local:vztmpl/debian-12-...tar.zst
	envTestISOPath     = "PVE_TEST_ISO_PATH"     // local path to a (small) ISO to upload (Phase 3)
	envTestVolID       = "PVE_TEST_VOLID"        // existing volume to snapshot + clean up (Phase 3)
	envTestHASIDs      = "PVE_TEST_HA_SIDS"      // CSV of >=2 HA-managed SIDs for a resource-affinity rule (Phase 4)
)

// newClient builds a live client from the environment, skipping the test when
// the node/token are not configured. Safe to call from every test.
func newClient(t *testing.T) *proxmox.Client {
	t.Helper()
	endpoint := os.Getenv(envEndpoint)
	tokenID := os.Getenv(envTokenID)
	secret := os.Getenv(envTokenSecret)
	if endpoint == "" || tokenID == "" || secret == "" {
		t.Skipf("live PVE node not configured (set %s, %s, %s)", envEndpoint, envTokenID, envTokenSecret)
	}

	insecure := os.Getenv(envInsecureTLS) == "1"

	var opts []proxmox.Option
	switch {
	case os.Getenv(envRecord) == "1":
		// Recording: the SDK must use the go-vcr client, which bypasses the
		// SDK's own TLS options, so the insecure choice is applied to the
		// recorder's real transport instead.
		rt := http.DefaultTransport
		if insecure {
			rt = insecureTransport()
		}
		client := newRecorderClient(t, cassetteName(t), recorder.ModeRecordOnly, rt)
		opts = append(opts, proxmox.WithHTTPClient(client))
	case insecure:
		opts = append(opts, proxmox.WithInsecureSkipVerify(true))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	c, err := proxmox.NewClient(ctx, endpoint, api.TokenCredentials(tokenID, secret), opts...)
	if err != nil {
		t.Fatalf("NewClient(%s): %v", endpoint, err)
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
