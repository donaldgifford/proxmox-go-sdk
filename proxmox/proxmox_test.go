package proxmox_test

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/api"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/mockpve"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/pverr"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/tasks"
)

const testUPID = "UPID:pve:000A1B2C:00ABCDEF:6489ABCD:qmstart:100:root@pam:"

func tokenCreds() api.Credentials {
	return api.TokenCredentials("root@pam!mock", "mock-secret")
}

func TestNewClientSeedsCapabilities(t *testing.T) {
	mock := mockpve.New()
	mock.SeedVersion("9.2.1", "9.2", "abc")
	ts := mock.Serve()
	defer ts.Close()

	c, err := proxmox.NewClient(context.Background(), ts.URL, tokenCreds())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if got := c.Capabilities().String(); got != "9.2.1" {
		t.Errorf("Capabilities() = %s, want 9.2.1", got)
	}
	if !c.Capabilities().DynamicLoadBalancer() {
		t.Error("9.2.1 DynamicLoadBalancer() = false, want true")
	}
}

func TestNewClientRejectsBelowMinimum(t *testing.T) {
	mock := mockpve.New()
	mock.SeedVersion("8.4.1", "8.4", "old")
	ts := mock.Serve()
	defer ts.Close()

	_, err := proxmox.NewClient(context.Background(), ts.URL, tokenCreds())
	if !errors.Is(err, pverr.ErrUnsupported) {
		t.Errorf("NewClient against 8.4.1 = %v, want ErrUnsupported", err)
	}
}

func TestClientAccessors(t *testing.T) {
	mock := mockpve.New()
	ts := mock.Serve()
	defer ts.Close()

	c, err := proxmox.NewClient(context.Background(), ts.URL, tokenCreds())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if c.API() == nil {
		t.Error("API() = nil")
	}
	if c.Version() == nil {
		t.Error("Version() = nil")
	}
	if c.Tasks() == nil {
		t.Error("Tasks() = nil")
	}
}

func TestClientTasksWaiterEndToEnd(t *testing.T) {
	mock := mockpve.New()
	mock.AddTask("pve", testUPID, "qmstart", "100", "root@pam", nil)
	mock.FinishTask("pve", testUPID, "OK") // already stopped: first poll returns
	ts := mock.Serve()
	defer ts.Close()

	c, err := proxmox.NewClient(context.Background(), ts.URL, tokenCreds())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	st, err := c.Tasks().Wait(context.Background(), tasks.Ref{Node: "pve", UPID: testUPID})
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if !st.OK() {
		t.Errorf("Wait status = %+v, want OK", st)
	}
}

func TestNewClientTLSWithInsecureSkipVerify(t *testing.T) {
	mock := mockpve.New(mockpve.WithTLS())
	ts := mock.Serve()
	defer ts.Close()

	c, err := proxmox.NewClient(context.Background(), ts.URL, tokenCreds(),
		proxmox.WithInsecureSkipVerify(true), proxmox.WithUserAgent("sdk-test"))
	if err != nil {
		t.Fatalf("NewClient over TLS: %v", err)
	}
	if !c.Capabilities().MeetsMinimum() {
		t.Error("default version should meet the 9.0 minimum")
	}
}

func TestAllOptionsConstruct(t *testing.T) {
	mock := mockpve.New()
	ts := mock.Serve()
	defer ts.Close()

	c, err := proxmox.NewClient(context.Background(), ts.URL, tokenCreds(),
		proxmox.WithHTTPClient(&http.Client{}),
		proxmox.WithRequestTimeout(5*time.Second),
		proxmox.WithRetry(api.DefaultRetryPolicy()),
		proxmox.WithClusterEndpoints(api.Endpoint{Name: "n2", Address: "127.0.0.1:8006", Priority: 1}),
		proxmox.WithMinTLS(tls.VersionTLS13),
		proxmox.WithUserAgent("sdk-test"),
		proxmox.WithLogger(nil), // nil is ignored
	)
	if err != nil {
		t.Fatalf("NewClient with all options: %v", err)
	}
	if !c.Capabilities().MeetsMinimum() {
		t.Error("default version should meet the 9.0 minimum")
	}
}

// recordingLogger counts Debug calls to prove WithLogger is wired through.
type recordingLogger struct{ calls atomic.Int32 }

func (l *recordingLogger) Debug(string, ...any) { l.calls.Add(1) }

func TestWithLoggerWiredToTransport(t *testing.T) {
	mock := mockpve.New()
	ts := mock.Serve()
	defer ts.Close()

	logger := &recordingLogger{}
	if _, err := proxmox.NewClient(context.Background(), ts.URL, tokenCreds(), proxmox.WithLogger(logger)); err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	// NewClient performs the /version round-trip, which the transport logs.
	if logger.calls.Load() == 0 {
		t.Error("WithLogger: Debug never called, want at least one request log")
	}
}
