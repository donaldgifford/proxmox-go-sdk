package lab

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// answersTestConfig covers the fields the answer flow reads.
func answersTestConfig() *Config {
	return &Config{
		Nested: Nested{
			Domain:       "lab.example",
			Gateway:      "192.0.2.1",
			DNS:          "192.0.2.1",
			AnswerURL:    "http://192.0.2.10:8442",
			AnswerListen: "127.0.0.1:0",
			Nodes: []Node{
				{Name: "pve1-dogfood", VMID: 9201, CIDR: "192.0.2.201/24"},
				{Name: "pve2-dogfood", VMID: 9202, CIDR: "192.0.2.202/24"},
				{Name: "pve3-dogfood", VMID: 9203, CIDR: "192.0.2.203/24"},
			},
		},
	}
}

func TestRenderAnswer(t *testing.T) {
	cfg := answersTestConfig()
	got, err := RenderAnswer(cfg, cfg.Nested.Nodes[0], "hunter2")
	if err != nil {
		t.Fatalf("RenderAnswer: %v", err)
	}
	for _, want := range []string{
		`fqdn = "pve1-dogfood.lab.example"`,
		`root-password = "hunter2"`,
		`cidr = "192.0.2.201/24"`,
		`gateway = "192.0.2.1"`,
		`dns = "192.0.2.1"`,
		`filter.ID_NET_NAME_MAC = "*"`,
		`disk-list = ["sda"]`,
	} {
		if !strings.Contains(string(got), want) {
			t.Errorf("rendered answer missing %q:\n%s", want, got)
		}
	}
}

// postBody sends body to the answer server, returning status and payload.
func postBody(t *testing.T, ts *httptest.Server, body string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, ts.URL, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Errorf("close response body: %v", err)
	}
	return resp.StatusCode, payload
}

func TestAnswerServerMatching(t *testing.T) {
	cfg := answersTestConfig()
	srv := NewAnswerServer(cfg, "hunter2", nil)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	b64 := base64.StdEncoding.EncodeToString([]byte("pve3-dogfood"))
	tests := []struct {
		name     string
		body     string
		wantFQDN string
	}{
		{
			name:     "json-shaped payload",
			body:     `{"product":{"serial":"pve1-dogfood"},"dmi":{"system":{"name":"Standard PC"}}}`,
			wantFQDN: "pve1-dogfood.lab.example",
		},
		{
			name:     "form-encoded payload",
			body:     "serial=pve2-dogfood&uuid=1234",
			wantFQDN: "pve2-dogfood.lab.example",
		},
		{
			name:     "garbage payload with bare substring",
			body:     "xx\x00yy pve1-dogfood zz",
			wantFQDN: "pve1-dogfood.lab.example",
		},
		{
			name:     "base64-encoded serial",
			body:     `{"serial":"` + b64 + `"}`,
			wantFQDN: "pve3-dogfood.lab.example",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, payload := postBody(t, ts, tt.body)
			if status != http.StatusOK {
				t.Fatalf("status = %d, body %s", status, payload)
			}
			if !strings.Contains(string(payload), `fqdn = "`+tt.wantFQDN+`"`) {
				t.Errorf("answer fqdn mismatch, want %s:\n%s", tt.wantFQDN, payload)
			}
			if !strings.Contains(string(payload), `root-password = "hunter2"`) {
				t.Errorf("answer missing root password:\n%s", payload)
			}
		})
	}

	served := srv.Served()
	for _, n := range []string{"pve1-dogfood", "pve2-dogfood", "pve3-dogfood"} {
		if _, ok := served[n]; !ok {
			t.Errorf("Served() missing %s: %v", n, served)
		}
	}
}

func TestAnswerServerNoMatch(t *testing.T) {
	srv := NewAnswerServer(answersTestConfig(), "hunter2", nil)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	status, _ := postBody(t, ts, `{"serial":"some-other-box"}`)
	if status != http.StatusNotFound {
		t.Errorf("status = %d, want 404", status)
	}
	if len(srv.Served()) != 0 {
		t.Errorf("Served() = %v, want empty", srv.Served())
	}
}

func TestAnswerServerGETSerialParam(t *testing.T) {
	srv := NewAnswerServer(answersTestConfig(), "hunter2", nil)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet,
		ts.URL+"?serial="+url.QueryEscape("pve2-dogfood"), http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("close response body: %v", err)
		}
	}()
	payload, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(payload), "pve2-dogfood.lab.example") {
		t.Errorf("status = %d, body:\n%s", resp.StatusCode, payload)
	}
}

// TestAnswerServerLongestMatch guards the prefix-name case: a body carrying
// "pve1-dogfood" must never be routed to a node named "pve1".
func TestAnswerServerLongestMatch(t *testing.T) {
	cfg := answersTestConfig()
	cfg.Nested.Nodes = []Node{
		{Name: "pve1", VMID: 9201, CIDR: "192.0.2.201/24"},
		{Name: "pve1-dogfood", VMID: 9202, CIDR: "192.0.2.202/24"},
	}
	srv := NewAnswerServer(cfg, "hunter2", nil)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	_, payload := postBody(t, ts, `{"serial":"pve1-dogfood"}`)
	if !strings.Contains(string(payload), `fqdn = "pve1-dogfood.lab.example"`) {
		t.Errorf("longest-name match failed:\n%s", payload)
	}
}

// TestAnswerServerStartShutdown exercises the real listener path Start/
// Shutdown drive during `pvelab up`.
func TestAnswerServerStartShutdown(t *testing.T) {
	srv := NewAnswerServer(answersTestConfig(), "hunter2", nil)
	ctx := context.Background()
	if err := srv.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if srv.Addr() == "" {
		t.Fatal("Addr() empty after Start")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"http://"+srv.Addr()+"/?serial=pve1-dogfood", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET via live listener: %v", err)
	}
	payload, _ := io.ReadAll(resp.Body)
	if err := resp.Body.Close(); err != nil {
		t.Errorf("close response body: %v", err)
	}
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(payload), "pve1-dogfood.lab.example") {
		t.Errorf("status = %d, body:\n%s", resp.StatusCode, payload)
	}

	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		t.Errorf("Shutdown: %v", err)
	}
}
