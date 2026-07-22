package mockpve_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/api"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/mockpve"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/pverr"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/tasks"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/version"
)

const testUPID = "UPID:pve:000A1B2C:00ABCDEF:6489ABCD:qmstart:100:root@pam:"

func fastWaitPolicy() tasks.WaitPolicy {
	return tasks.WaitPolicy{Initial: time.Millisecond, Max: 5 * time.Millisecond, Factor: 1.5}
}

func TestVersionDefaultAndSeed(t *testing.T) {
	mock := mockpve.New()
	mock.SeedVersion("9.2.1", "9.2", "abc123")
	c, cleanup := mock.NewClient()
	defer cleanup()

	caps, err := version.NewService(c).Capabilities(context.Background())
	if err != nil {
		t.Fatalf("Capabilities: %v", err)
	}
	if caps.String() != "9.2.1" {
		t.Errorf("version = %s, want 9.2.1", caps)
	}
	if !caps.HAClusterSwitch() {
		t.Error("9.2.1 HAClusterSwitch() = false, want true")
	}
}

func TestVersionBelowMinimum(t *testing.T) {
	mock := mockpve.New()
	mock.SeedVersion("8.4.1", "8.4", "old")
	c, cleanup := mock.NewClient()
	defer cleanup()

	_, err := version.NewService(c).Capabilities(context.Background())
	if !errors.Is(err, pverr.ErrUnsupported) {
		t.Errorf("Capabilities on 8.4.1 = %v, want ErrUnsupported", err)
	}
}

func TestUserCredentialsMintTicket(t *testing.T) {
	mock := mockpve.New()
	mock.AddUser("root@pam", "secret")
	ts := mock.Serve()
	defer ts.Close()

	c, err := api.New(ts.URL, api.UserCredentials("root@pam", "secret", ""))
	if err != nil {
		t.Fatalf("api.New: %v", err)
	}

	// Get triggers a ticket mint (POST /access/ticket) then an authed GET.
	v, err := version.NewService(c).Get(context.Background())
	if err != nil {
		t.Fatalf("Get after mint: %v", err)
	}
	if v.Version == "" {
		t.Error("empty version after authenticated request")
	}
}

func TestUserCredentialsBadPassword(t *testing.T) {
	mock := mockpve.New()
	mock.AddUser("root@pam", "secret")
	ts := mock.Serve()
	defer ts.Close()

	c, err := api.New(ts.URL, api.UserCredentials("root@pam", "wrong", ""))
	if err != nil {
		t.Fatalf("api.New: %v", err)
	}

	_, err = version.NewService(c).Get(context.Background())
	if !errors.Is(err, pverr.ErrUnauthorized) {
		t.Errorf("Get with bad password = %v, want ErrUnauthorized", err)
	}
}

func TestTaskWaitSuccess(t *testing.T) {
	mock := mockpve.New()
	mock.AddTask("pve", testUPID, "qmstart", "100", "root@pam", nil)
	c, cleanup := mock.NewClient()
	defer cleanup()

	svc := tasks.NewService(c, tasks.WithWaitPolicy(fastWaitPolicy()))

	go func() {
		time.Sleep(5 * time.Millisecond)
		mock.FinishTask("pve", testUPID, "OK")
	}()

	st, err := svc.Wait(context.Background(), tasks.Ref{Node: "pve", UPID: testUPID})
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if !st.OK() {
		t.Errorf("Wait status = %+v, want OK", st)
	}
}

func TestTaskWaitFailureCarriesLog(t *testing.T) {
	mock := mockpve.New()
	mock.AddTask("pve", testUPID, "qmstart", "100", "root@pam",
		[]tasks.LogLine{{Number: 1, Text: "starting VM"}, {Number: 2, Text: "kvm: missing romfile"}})
	mock.FinishTask("pve", testUPID, "command 'qm start 100' failed: exit code 1")

	c, cleanup := mock.NewClient()
	defer cleanup()
	svc := tasks.NewService(c, tasks.WithWaitPolicy(fastWaitPolicy()))

	_, err := svc.Wait(context.Background(), tasks.Ref{Node: "pve", UPID: testUPID})
	if !errors.Is(err, pverr.ErrTaskFailed) {
		t.Fatalf("Wait = %v, want ErrTaskFailed", err)
	}
	var pe *pverr.Error
	if !errors.As(err, &pe) || pe.UPID != testUPID {
		t.Fatalf("error missing UPID: %v", err)
	}
}

func TestTaskStatusNotFound(t *testing.T) {
	mock := mockpve.New()
	c, cleanup := mock.NewClient()
	defer cleanup()
	svc := tasks.NewService(c)

	unknown := "UPID:pve:00000001:00000001:6489ABCD:qmstart:999:root@pam:"
	_, err := svc.Status(context.Background(), tasks.Ref{Node: "pve", UPID: unknown})
	if !errors.Is(err, pverr.ErrNotFound) {
		t.Errorf("Status of unknown task = %v, want ErrNotFound", err)
	}
}

func TestTaskLog(t *testing.T) {
	mock := mockpve.New()
	mock.AddTask("pve", testUPID, "qmstart", "100", "root@pam",
		[]tasks.LogLine{{Number: 1, Text: "one"}, {Number: 2, Text: "two"}})
	c, cleanup := mock.NewClient()
	defer cleanup()

	lines, err := tasks.NewService(c).Log(context.Background(), tasks.Ref{Node: "pve", UPID: testUPID})
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if len(lines) != 2 || lines[1].Text != "two" {
		t.Errorf("Log = %+v, want two lines", lines)
	}
}

func TestUnauthenticatedRejected(t *testing.T) {
	mock := mockpve.New()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api2/json/version", http.NoBody)
	rec := httptest.NewRecorder()
	mock.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("no-auth /version status = %d, want 401", rec.Code)
	}
}

func TestServeTLS(t *testing.T) {
	mock := mockpve.New(mockpve.WithTLS())
	c, cleanup := mock.NewClient()
	defer cleanup()

	if _, err := version.NewService(c).Get(context.Background()); err != nil {
		t.Fatalf("Get over TLS: %v", err)
	}
}

func TestRegisterHandler(t *testing.T) {
	mock := mockpve.New()
	// A path the mock does not build in, to exercise the extension seam.
	mock.RegisterHandler("GET /api2/json/cluster/nextid", http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			io.WriteString(w, `{"data":[{"type":"cluster","name":"mock"}]}`)
		}))
	c, cleanup := mock.NewClient()
	defer cleanup()

	var out []struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if err := c.DoRequest(context.Background(), http.MethodGet, "/cluster/nextid", nil, &out); err != nil {
		t.Fatalf("DoRequest: %v", err)
	}
	if len(out) != 1 || out[0].Name != "mock" {
		t.Errorf("custom route = %+v, want one entry named mock", out)
	}
}

// staticCache is a ResponseCache backed by a map keyed by "METHOD path".
type staticCache map[string]json.RawMessage

func (c staticCache) Lookup(method, path string) (json.RawMessage, bool) {
	v, ok := c[method+" "+path]
	return v, ok
}

func TestWithCacheOverridesModel(t *testing.T) {
	mock := mockpve.New(mockpve.WithCache(staticCache{
		"GET /api2/json/version": json.RawMessage(`{"version":"7.0.0","release":"7.0"}`),
	}))
	c, cleanup := mock.NewClient()
	defer cleanup()

	// The cache short-circuits the model, so /version reports the cached value
	// (and the SDK rejects it as below the floor).
	v, err := version.NewService(c).Get(context.Background())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if v.Version != "7.0.0" {
		t.Errorf("cached version = %q, want 7.0.0", v.Version)
	}
}
