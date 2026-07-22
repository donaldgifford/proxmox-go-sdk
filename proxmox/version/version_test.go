package version

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/api"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/pverr"
)

func TestParse(t *testing.T) {
	tests := []struct {
		in      string
		want    string // String() form
		wantErr bool
	}{
		{in: "9.0.3", want: "9.0.3"},
		{in: "9.0", want: "9.0.0"},
		{in: "9", want: "9.0.0"},
		{in: "9.0.0-1", want: "9.0.0"},
		{in: "9.2.0~rc1", want: "9.2.0"},
		{in: "10.2.5", want: "10.2.5"},
		{in: "  9.1.2  ", want: "9.1.2"},
		{in: "", wantErr: true},
		{in: "x.y.z", wantErr: true},
		{in: "9.x.0", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := Parse(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Parse(%q) = %v, want error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse(%q) unexpected error: %v", tt.in, err)
			}
			if got.String() != tt.want {
				t.Errorf("Parse(%q).String() = %q, want %q", tt.in, got.String(), tt.want)
			}
		})
	}
}

func TestAtLeast(t *testing.T) {
	tests := []struct {
		ver          string
		major, minor int
		want         bool
	}{
		{ver: "9.0.3", major: 9, minor: 0, want: true},
		{ver: "9.0.3", major: 9, minor: 1, want: false},
		{ver: "9.2.0", major: 9, minor: 1, want: true},
		{ver: "9.2.0", major: 9, minor: 2, want: true},
		{ver: "10.0.0", major: 9, minor: 9, want: true},
		{ver: "8.4.0", major: 9, minor: 0, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.ver, func(t *testing.T) {
			caps := mustParse(t, tt.ver)
			if got := caps.AtLeast(tt.major, tt.minor); got != tt.want {
				t.Errorf("%s.AtLeast(%d,%d) = %v, want %v", tt.ver, tt.major, tt.minor, got, tt.want)
			}
		})
	}
}

func TestGates(t *testing.T) {
	tests := []struct {
		ver string
		oci bool // OCITemplates 9.1+
		tsr bool // TokenSecretRotation 9.2+
		tpm bool // TPMStateSnapshots 9.1+
		vcs bool // VolumeChainSnapshots 9.1+
		rzx bool // ZFSRAIDZExpansion 9.2+
		hcs bool // HAClusterSwitch 9.2+
	}{
		{ver: "9.0.0", oci: false, tsr: false, tpm: false, vcs: false, rzx: false, hcs: false},
		{ver: "9.1.0", oci: true, tsr: false, tpm: true, vcs: true, rzx: false, hcs: false},
		{ver: "9.2.0", oci: true, tsr: true, tpm: true, vcs: true, rzx: true, hcs: true},
	}
	for _, tt := range tests {
		t.Run(tt.ver, func(t *testing.T) {
			caps := mustParse(t, tt.ver)
			if got := caps.OCITemplates(); got != tt.oci {
				t.Errorf("OCITemplates() = %v, want %v", got, tt.oci)
			}
			if got := caps.TokenSecretRotation(); got != tt.tsr {
				t.Errorf("TokenSecretRotation() = %v, want %v", got, tt.tsr)
			}
			if got := caps.TPMStateSnapshots(); got != tt.tpm {
				t.Errorf("TPMStateSnapshots() = %v, want %v", got, tt.tpm)
			}
			if got := caps.VolumeChainSnapshots(); got != tt.vcs {
				t.Errorf("VolumeChainSnapshots() = %v, want %v", got, tt.vcs)
			}
			if got := caps.ZFSRAIDZExpansion(); got != tt.rzx {
				t.Errorf("ZFSRAIDZExpansion() = %v, want %v", got, tt.rzx)
			}
			if got := caps.HAClusterSwitch(); got != tt.hcs {
				t.Errorf("HAClusterSwitch() = %v, want %v", got, tt.hcs)
			}
		})
	}
}

func TestMeetsMinimum(t *testing.T) {
	if !mustParse(t, "9.0.0").MeetsMinimum() {
		t.Error("9.0.0 MeetsMinimum() = false, want true")
	}
	if mustParse(t, "8.9.9").MeetsMinimum() {
		t.Error("8.9.9 MeetsMinimum() = true, want false")
	}
}

func TestRequire(t *testing.T) {
	caps := mustParse(t, "9.1.0")

	if err := caps.Require("oci templates", "9.1"); err != nil {
		t.Errorf("Require(9.1) on 9.1.0 = %v, want nil", err)
	}

	err := caps.Require("dynamic load balancer", "9.2")
	if !errors.Is(err, pverr.ErrUnsupported) {
		t.Errorf("Require(9.2) on 9.1.0 error = %v, want ErrUnsupported", err)
	}

	if err := caps.Require("bogus", "not-a-version"); err == nil {
		t.Error("Require with invalid minVersion = nil, want error")
	}
}

func TestServiceCapabilities(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `{"data":{"version":"9.0.3","release":"9.0","repoid":"deadbeef"}}`)
	}))
	defer srv.Close()

	svc := NewService(mustClient(t, srv.URL))

	v, err := svc.Get(context.Background())
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if v.Version != "9.0.3" || v.Release != "9.0" || v.RepoID != "deadbeef" {
		t.Errorf("Get() = %+v, want version 9.0.3 release 9.0 repoid deadbeef", v)
	}

	caps, err := svc.Capabilities(context.Background())
	if err != nil {
		t.Fatalf("Capabilities() error: %v", err)
	}
	if caps.String() != "9.0.3" {
		t.Errorf("Capabilities() = %s, want 9.0.3", caps)
	}
}

func TestServiceCapabilitiesBelowMinimum(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `{"data":{"version":"8.4.1","release":"8.4"}}`)
	}))
	defer srv.Close()

	svc := NewService(mustClient(t, srv.URL))
	_, err := svc.Capabilities(context.Background())
	if !errors.Is(err, pverr.ErrUnsupported) {
		t.Errorf("Capabilities() on 8.4.1 error = %v, want ErrUnsupported", err)
	}
}

func mustParse(t *testing.T, s string) Capabilities {
	t.Helper()
	caps, err := Parse(s)
	if err != nil {
		t.Fatalf("Parse(%q): %v", s, err)
	}
	return caps
}

func mustClient(t *testing.T, addr string) api.Client {
	t.Helper()
	c, err := api.New(addr, api.TokenCredentials("root@pam!test", "secret"))
	if err != nil {
		t.Fatalf("api.New(%q): %v", addr, err)
	}
	return c
}
