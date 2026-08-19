package lab

import (
	"context"
	"net/netip"
	"strings"
	"testing"
)

func TestCheckAnswerURLHost(t *testing.T) {
	local := []netip.Addr{
		netip.MustParseAddr("127.0.0.1"),
		netip.MustParseAddr("10.10.10.105"),
		netip.MustParseAddr("fe80::1"),
	}
	tests := []struct {
		name    string
		url     string
		wantErr string
	}{
		{name: "this machine's address", url: "http://10.10.10.105:8442"},
		{name: "ipv6 address", url: "http://[fe80::1]:8442"},
		{
			// The real failure: answer_url naming the outer host while `up`
			// runs on a workstation. It must say what to do, because the
			// symptom it replaces (three readiness timeouts) says nothing.
			name:    "another machine",
			url:     "http://10.10.11.20:8442",
			wantErr: "not an address of this machine",
		},
		{
			// Local, but the installers are elsewhere — a different mistake
			// deserving a different message.
			name:    "loopback",
			url:     "http://127.0.0.1:8442",
			wantErr: "loopback",
		},
		{name: "unparseable", url: "://nope", wantErr: "nested.answer_url"},
		{name: "no host", url: "http://:8442", wantErr: "names no host"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkAnswerURLHost(context.Background(), tt.url, local)
			switch {
			case tt.wantErr == "" && err != nil:
				t.Fatalf("checkAnswerURLHost(%q) = %v, want nil", tt.url, err)
			case tt.wantErr != "" && err == nil:
				t.Fatalf("checkAnswerURLHost(%q) = nil, want an error mentioning %q", tt.url, tt.wantErr)
			case tt.wantErr != "" && !strings.Contains(err.Error(), tt.wantErr):
				t.Errorf("checkAnswerURLHost(%q) = %v, want it to mention %q", tt.url, err, tt.wantErr)
			}
		})
	}
}

// TestCheckAnswerURLNamesBothSides pins what the message carries: the address
// the URL points at AND the ones this machine has. Diagnosing the mismatch
// without both means going and looking them up.
func TestCheckAnswerURLNamesBothSides(t *testing.T) {
	err := checkAnswerURLHost(context.Background(), "http://10.10.11.20:8442",
		[]netip.Addr{netip.MustParseAddr("10.10.10.105")})
	if err == nil {
		t.Fatal("want an error")
	}
	for _, want := range []string{"10.10.11.20", "10.10.10.105", "pvelab iso"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message is missing %q:\n%v", want, err)
		}
	}
}

// TestLocalAddrsFindsSomething is a smoke test: the comparison is only as good
// as the address list it is given, and an empty one would fail every URL.
func TestLocalAddrsFindsSomething(t *testing.T) {
	addrs, err := localAddrs()
	if err != nil {
		t.Fatalf("localAddrs: %v", err)
	}
	if len(addrs) == 0 {
		t.Error("localAddrs() is empty; every answer_url would be rejected")
	}
}
