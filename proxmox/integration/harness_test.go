//go:build integration

package integration

import (
	"context"
	"os"
	"testing"
	"time"

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

	// Destructive-test gates (compute lifecycle). Absent -> those tests skip.
	envTestStorage     = "PVE_TEST_STORAGE"      // target storage for a scratch guest disk
	envTestVMID        = "PVE_TEST_VMID"         // scratch QEMU VMID the suite may create/destroy
	envTestLXCVMID     = "PVE_TEST_LXC_VMID"     // scratch LXC VMID the suite may create/destroy
	envTestLXCTemplate = "PVE_TEST_LXC_TEMPLATE" // OS template volid, e.g. local:vztmpl/debian-12-...tar.zst
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

	var opts []proxmox.Option
	if os.Getenv(envInsecureTLS) == "1" {
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
