package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/pverr"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/types"
)

// fastPolicy retries quickly so tests do not sleep on backoff.
func fastPolicy(attempts int) RetryPolicy {
	return RetryPolicy{
		Attempts:      attempts,
		InitialDelay:  time.Millisecond,
		MaxDelay:      time.Millisecond,
		BackoffFactor: 2.0,
		Jitter:        false,
	}
}

func mustClient(t *testing.T, primary string, creds Credentials, opts ...TransportOption) Client {
	t.Helper()
	c, err := New(primary, creds, opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestExpandPath(t *testing.T) {
	var tr transport
	tests := []struct{ in, want string }{
		{"version", "/api2/json/version"},
		{"cluster/resources", "/api2/json/cluster/resources"},
		{"/nodes/pve/qemu", "/api2/json/nodes/pve/qemu"},
		{"/api2/json/already", "/api2/json/already"},
	}
	for _, tt := range tests {
		if got := tr.ExpandPath(tt.in); got != tt.want {
			t.Errorf("ExpandPath(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFormEncodeStruct(t *testing.T) {
	body := struct {
		Name     string        `json:"name"`
		Cores    int           `json:"cores"`
		Template types.PVEBool `json:"template"`
	}{Name: "vm1", Cores: 2, Template: true}

	got, err := formEncode(body)
	if err != nil {
		t.Fatalf("formEncode: %v", err)
	}
	// url.Values.Encode sorts keys: cores, name, template.
	if want := "cores=2&name=vm1&template=1"; got != want {
		t.Errorf("formEncode = %q, want %q", got, want)
	}
}

func TestFormEncodeOmitempty(t *testing.T) {
	body := struct {
		A string `json:"a,omitempty"`
		B string `json:"b"`
	}{A: "", B: "x"}
	got, err := formEncode(body)
	if err != nil {
		t.Fatalf("formEncode: %v", err)
	}
	if got != "b=x" {
		t.Errorf("formEncode = %q, want b=x", got)
	}
}

func TestFormEncodeURLValuesPassthrough(t *testing.T) {
	got, err := formEncode(url.Values{"a": {"1"}, "b": {"two words"}})
	if err != nil {
		t.Fatalf("formEncode: %v", err)
	}
	if got != "a=1&b=two+words" {
		t.Errorf("formEncode = %q, want a=1&b=two+words", got)
	}
}

func TestFormEncodeNil(t *testing.T) {
	got, err := formEncode(nil)
	if err != nil || got != "" {
		t.Errorf("formEncode(nil) = (%q, %v), want (\"\", nil)", got, err)
	}
}

// TestFormEncodeStringRoundTrip proves string values are JSON-unescaped before
// form-encoding, so quotes/ampersands/spaces survive a round trip.
func TestFormEncodeStringRoundTrip(t *testing.T) {
	const desc = `a "quoted" & spaced`
	body := struct {
		Desc string `json:"desc"`
	}{Desc: desc}

	got, err := formEncode(body)
	if err != nil {
		t.Fatalf("formEncode: %v", err)
	}
	vals, err := url.ParseQuery(got)
	if err != nil {
		t.Fatalf("ParseQuery(%q): %v", got, err)
	}
	if vals.Get("desc") != desc {
		t.Errorf("round-trip desc = %q, want %q", vals.Get("desc"), desc)
	}
}

func TestDoUpload(t *testing.T) {
	var gotAuth, gotPath, gotContent, gotFile string
	var multipartCT bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		multipartCT = mediaType == "multipart/form-data"
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			http.Error(w, "bad multipart", http.StatusBadRequest)
			return
		}
		gotContent = r.FormValue("content")
		if f, _, err := r.FormFile("filename"); err == nil {
			b, _ := io.ReadAll(f)
			gotFile = string(b)
			_ = f.Close()
		}
		io.WriteString(w, `{"data":"UPID:pve:0:0:0:imgcopy:local:root@pam:"}`)
	}))
	defer srv.Close()

	c := mustClient(t, srv.URL, TokenCredentials("root@pam!t", "sec"))

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("content", "iso"); err != nil {
		t.Fatalf("WriteField: %v", err)
	}
	fw, err := mw.CreateFormFile("filename", "x.iso")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	io.WriteString(fw, "PAYLOAD")
	if err := mw.Close(); err != nil {
		t.Fatalf("multipart Close: %v", err)
	}

	var upid string
	if err := c.DoUpload(context.Background(), "nodes/pve/storage/local/upload", &buf, mw.FormDataContentType(), &upid); err != nil {
		t.Fatalf("DoUpload: %v", err)
	}
	if upid == "" {
		t.Error("DoUpload returned empty UPID")
	}
	if gotPath != "/api2/json/nodes/pve/storage/local/upload" {
		t.Errorf("server path = %q, want the expanded upload path", gotPath)
	}
	if want := "PVEAPIToken=root@pam!t=sec"; gotAuth != want {
		t.Errorf("Authorization = %q, want %q", gotAuth, want)
	}
	if !multipartCT {
		t.Error("server did not see a multipart/form-data Content-Type")
	}
	if gotContent != "iso" {
		t.Errorf("content field = %q, want iso", gotContent)
	}
	if gotFile != "PAYLOAD" {
		t.Errorf("streamed file = %q, want PAYLOAD", gotFile)
	}
}

func TestDoRequestSuccessAndAuthHeader(t *testing.T) {
	var gotAuth, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		io.WriteString(w, `{"data":{"version":"9.0.3"}}`)
	}))
	defer srv.Close()

	c := mustClient(t, srv.URL, TokenCredentials("root@pam!t", "sec"), WithRetryPolicy(fastPolicy(1)))
	var out struct {
		Version string `json:"version"`
	}
	if err := c.DoRequest(context.Background(), http.MethodGet, "version", nil, &out); err != nil {
		t.Fatalf("DoRequest: %v", err)
	}
	if out.Version != "9.0.3" {
		t.Errorf("version = %q, want 9.0.3", out.Version)
	}
	if gotPath != "/api2/json/version" {
		t.Errorf("server path = %q, want /api2/json/version", gotPath)
	}
	if want := "PVEAPIToken=root@pam!t=sec"; gotAuth != want {
		t.Errorf("Authorization = %q, want %q", gotAuth, want)
	}
}

func TestDoRequestWriteHeadersAndBody(t *testing.T) {
	var gotCSRF, gotCT, gotCookie, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCSRF = r.Header.Get("CSRFPreventionToken")
		gotCT = r.Header.Get("Content-Type")
		if ck, err := r.Cookie("PVEAuthCookie"); err == nil {
			gotCookie = ck.Value
		}
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		io.WriteString(w, `{"data":"UPID:pve:00001234:::qmstart:100:root@pam:"}`)
	}))
	defer srv.Close()

	c := mustClient(t, srv.URL, TicketCredentials("TKT", "CSRF"), WithRetryPolicy(fastPolicy(1)))
	var upid string
	err := c.DoRequest(context.Background(), http.MethodPost,
		"nodes/pve/qemu/100/status/start", url.Values{"force": {"1"}}, &upid)
	if err != nil {
		t.Fatalf("DoRequest: %v", err)
	}
	if gotCSRF != "CSRF" {
		t.Errorf("CSRFPreventionToken = %q, want CSRF", gotCSRF)
	}
	if gotCT != "application/x-www-form-urlencoded" {
		t.Errorf("Content-Type = %q", gotCT)
	}
	if gotCookie != "TKT" {
		t.Errorf("PVEAuthCookie = %q, want TKT", gotCookie)
	}
	if gotBody != "force=1" {
		t.Errorf("body = %q, want force=1", gotBody)
	}
	if !strings.HasPrefix(upid, "UPID:") {
		t.Errorf("upid = %q, want UPID prefix", upid)
	}
}

func TestDoRequestNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, `{"data":null,"message":"VM 999 not found"}`)
	}))
	defer srv.Close()

	c := mustClient(t, srv.URL, TokenCredentials("u!t", "s"), WithRetryPolicy(fastPolicy(1)))
	err := c.DoRequest(context.Background(), http.MethodGet, "nodes/pve/qemu/999/status/current", nil, nil)
	if !errors.Is(err, pverr.ErrNotFound) {
		t.Fatalf("err = %v, want errors.Is ErrNotFound", err)
	}
	var pe *pverr.Error
	if !errors.As(err, &pe) {
		t.Fatalf("err is not *pverr.Error: %v", err)
	}
	if pe.Status != http.StatusNotFound {
		t.Errorf("Status = %d, want 404", pe.Status)
	}
	if pe.Message != "VM 999 not found" {
		t.Errorf("Message = %q", pe.Message)
	}
}

func TestDoRequestRetriesThenTransient(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
		io.WriteString(w, `{"message":"cluster busy"}`)
	}))
	defer srv.Close()

	c := mustClient(t, srv.URL, TokenCredentials("u!t", "s"), WithRetryPolicy(fastPolicy(3)))
	err := c.DoRequest(context.Background(), http.MethodGet, "version", nil, nil)
	if !errors.Is(err, pverr.ErrTransient) {
		t.Fatalf("err = %v, want errors.Is ErrTransient", err)
	}
	if got := atomic.LoadInt32(&hits); got != 3 {
		t.Errorf("server hits = %d, want 3 (Attempts)", got)
	}
}

func TestDoRequestFailoverAcrossEndpoints(t *testing.T) {
	var h1, h2 int32
	srv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&h1, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv1.Close()
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&h2, 1)
		io.WriteString(w, `{"data":{"version":"9.0"}}`)
	}))
	defer srv2.Close()

	c := mustClient(t, srv1.URL, TokenCredentials("u!t", "s"),
		WithClusterEndpoints(Endpoint{Name: "n2", Address: srv2.URL, Priority: 1}),
		WithRetryPolicy(fastPolicy(1)))

	var out struct {
		Version string `json:"version"`
	}
	if err := c.DoRequest(context.Background(), http.MethodGet, "version", nil, &out); err != nil {
		t.Fatalf("DoRequest: %v", err)
	}
	if out.Version != "9.0" {
		t.Errorf("version = %q, want 9.0 (from failover node)", out.Version)
	}
	if atomic.LoadInt32(&h1) < 1 || atomic.LoadInt32(&h2) < 1 {
		t.Errorf("hits: srv1=%d srv2=%d, want both >= 1", h1, h2)
	}
}

func TestUserCredentialsMintOnFirstUse(t *testing.T) {
	var mints int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api2/json/access/ticket":
			atomic.AddInt32(&mints, 1)
			io.WriteString(w, `{"data":{"ticket":"TKT","CSRFPreventionToken":"CSRF"}}`)
		case r.URL.Path == "/api2/json/version":
			ck, err := r.Cookie("PVEAuthCookie")
			if err != nil || ck.Value != "TKT" {
				w.WriteHeader(http.StatusUnauthorized)
				io.WriteString(w, `{"message":"no ticket"}`)
				return
			}
			io.WriteString(w, `{"data":{"version":"9.0"}}`)
		default:
			http.Error(w, "unexpected", http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	c := mustClient(t, srv.URL, UserCredentials("root@pam", "pw", ""), WithRetryPolicy(fastPolicy(1)))
	var out struct {
		Version string `json:"version"`
	}
	if err := c.DoRequest(context.Background(), http.MethodGet, "version", nil, &out); err != nil {
		t.Fatalf("DoRequest: %v", err)
	}
	if out.Version != "9.0" {
		t.Errorf("version = %q, want 9.0", out.Version)
	}
	if got := atomic.LoadInt32(&mints); got != 1 {
		t.Errorf("mint count = %d, want 1", got)
	}
}

// TestTicketExpiredReauth verifies the single re-auth-and-replay: the first
// ticket is rejected as expired, the transport mints a second and replays.
func TestTicketExpiredReauth(t *testing.T) {
	var mu sync.Mutex
	mintCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api2/json/access/ticket" {
			mu.Lock()
			mintCount++
			ticket := fmt.Sprintf("T%d", mintCount)
			mu.Unlock()
			fmt.Fprintf(w, `{"data":{"ticket":%q,"CSRFPreventionToken":"C"}}`, ticket)
			return
		}
		ck, _ := r.Cookie("PVEAuthCookie")
		if ck != nil && ck.Value == "T2" {
			io.WriteString(w, `{"data":{"version":"9.0"}}`)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"message":"invalid PVE ticket: ticket expired"}`)
	}))
	defer srv.Close()

	c := mustClient(t, srv.URL, UserCredentials("root@pam", "pw", ""), WithRetryPolicy(fastPolicy(1)))
	var out struct {
		Version string `json:"version"`
	}
	if err := c.DoRequest(context.Background(), http.MethodGet, "version", nil, &out); err != nil {
		t.Fatalf("DoRequest: %v", err)
	}
	if out.Version != "9.0" {
		t.Errorf("version = %q, want 9.0 after re-auth", out.Version)
	}
	mu.Lock()
	defer mu.Unlock()
	if mintCount != 2 {
		t.Errorf("mint count = %d, want 2 (initial + forced re-auth)", mintCount)
	}
}

func TestDoRequestContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `{"data":{}}`)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before the call

	c := mustClient(t, srv.URL, TokenCredentials("u!t", "s"), WithRetryPolicy(fastPolicy(3)))
	err := c.DoRequest(ctx, http.MethodGet, "version", nil, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestNewRejectsNilCredentials(t *testing.T) {
	if _, err := New("pve.example.com", nil); err == nil {
		t.Error("New with nil credentials = nil error, want error")
	}
}
