package api

import "testing"

func TestNormaliseAddress(t *testing.T) {
	tests := []struct {
		name       string
		in         string
		wantScheme string
		wantHost   string
		wantErr    bool
	}{
		{name: "bare host", in: "pve.example.com", wantScheme: "https", wantHost: "pve.example.com:8006"},
		{name: "host with port", in: "pve.example.com:443", wantScheme: "https", wantHost: "pve.example.com:443"},
		{name: "https url", in: "https://pve.example.com", wantScheme: "https", wantHost: "pve.example.com:8006"},
		{name: "http preserved", in: "http://127.0.0.1:8006", wantScheme: "http", wantHost: "127.0.0.1:8006"},
		{name: "ipv4 bare", in: "10.0.0.5", wantScheme: "https", wantHost: "10.0.0.5:8006"},
		{name: "ipv6 bare", in: "[2001:db8::1]", wantScheme: "https", wantHost: "[2001:db8::1]:8006"},
		{name: "empty", in: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, err := normaliseAddress(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("normaliseAddress(%q) = nil error, want error", tt.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("normaliseAddress(%q) = %v", tt.in, err)
			}
			if u.Scheme != tt.wantScheme {
				t.Errorf("scheme = %q, want %q", u.Scheme, tt.wantScheme)
			}
			if u.Host != tt.wantHost {
				t.Errorf("host = %q, want %q", u.Host, tt.wantHost)
			}
			if u.Path != "" {
				t.Errorf("path = %q, want empty", u.Path)
			}
		})
	}
}

func TestNewConnectionDedupAndSort(t *testing.T) {
	conn, err := newConnection("primary.example.com", []Endpoint{
		{Name: "n2", Address: "node2.example.com", Priority: 2},
		{Name: "n1", Address: "node1.example.com", Priority: 1},
		{Name: "dup", Address: "primary.example.com", Priority: 9}, // dup of primary
	})
	if err != nil {
		t.Fatalf("newConnection: %v", err)
	}
	if got := conn.count(); got != 3 {
		t.Fatalf("count = %d, want 3 (duplicate dropped)", got)
	}
	// Active endpoint starts at priority 0 (the primary).
	if got := conn.baseURL().Host; got != "primary.example.com:8006" {
		t.Errorf("first baseURL host = %q, want primary", got)
	}
	// Ordered ascending by priority.
	for i := 1; i < len(conn.ordered); i++ {
		if conn.ordered[i-1].Priority > conn.ordered[i].Priority {
			t.Errorf("ordered not sorted by priority: %v", conn.ordered)
		}
	}
}

func TestConnectionFailoverRotation(t *testing.T) {
	conn, err := newConnection("a.example.com", []Endpoint{
		{Address: "b.example.com", Priority: 1},
		{Address: "c.example.com", Priority: 2},
	})
	if err != nil {
		t.Fatalf("newConnection: %v", err)
	}

	want := []string{"a.example.com:8006", "b.example.com:8006", "c.example.com:8006", "a.example.com:8006"}
	if got := conn.baseURL().Host; got != want[0] {
		t.Fatalf("start host = %q, want %q", got, want[0])
	}
	for i := 1; i < len(want); i++ {
		if !conn.failover() {
			t.Fatalf("failover %d = false, want true", i)
		}
		if got := conn.baseURL().Host; got != want[i] {
			t.Errorf("after failover %d host = %q, want %q", i, got, want[i])
		}
	}
}

func TestConnectionSingleEndpointNoFailover(t *testing.T) {
	conn, err := newConnection("only.example.com", nil)
	if err != nil {
		t.Fatalf("newConnection: %v", err)
	}
	if conn.failover() {
		t.Error("failover() = true for a single endpoint, want false")
	}
}

func TestBaseURLReturnsCopy(t *testing.T) {
	conn, err := newConnection("a.example.com", nil)
	if err != nil {
		t.Fatalf("newConnection: %v", err)
	}
	u := conn.baseURL()
	u.Host = "mutated:1234"
	if got := conn.baseURL().Host; got == "mutated:1234" {
		t.Error("baseURL did not return a copy; mutation leaked into stored URL")
	}
}
