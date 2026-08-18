package nodes

import (
	"errors"
	"testing"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/internal/svcutil"
)

// The property-string codecs are unexported, so these tests live inside the
// package. The wire forms below come from the 9.2 apidoc: acme carries keyed
// account/domains sub-keys, and acmedomain[n] makes domain its default key with
// plugin defaulting to standalone.

func TestParseNodeACME(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		in          string
		wantAccount string
		wantDomains []string
	}{
		{"account only", "account=default", "default", nil},
		{
			"account and domains", "account=le-staging,domains=a.example;b.example",
			"le-staging",
			[]string{"a.example", "b.example"},
		},
		{"domains only", "domains=solo.example", "", []string{"solo.example"}},
		{"unknown sub-key ignored", "account=x,futurekey=y", "x", nil},
		{"empty", "", "", nil},
		{"spaces trimmed", "account = padded ", "padded", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := parseNodeACME(tt.in)
			if got.Account != tt.wantAccount {
				t.Errorf("Account = %q, want %q", got.Account, tt.wantAccount)
			}
			if len(got.Domains) != len(tt.wantDomains) {
				t.Fatalf("Domains = %v, want %v", got.Domains, tt.wantDomains)
			}
			for i, want := range tt.wantDomains {
				if got.Domains[i] != want {
					t.Errorf("Domains[%d] = %q, want %q", i, got.Domains[i], want)
				}
			}
		})
	}
}

func TestEncodeNodeACME(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   NodeACME
		want string
	}{
		{"account only", NodeACME{Account: "default"}, "account=default"},
		{
			"both",
			NodeACME{Account: "le", Domains: []string{"a.example", "b.example"}},
			"account=le,domains=a.example;b.example",
		},
		{"domains only", NodeACME{Domains: []string{"a.example"}}, "domains=a.example"},
		{"empty renders empty", NodeACME{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := encodeNodeACME(tt.in); got != tt.want {
				t.Errorf("encodeNodeACME() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseACMEDomain(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		in         string
		wantDomain string
		wantPlugin string
		wantAlias  string
		wantErr    bool
	}{
		// The default-key form is what PVE's own UI writes.
		{name: "bare domain", in: "host.example.com", wantDomain: "host.example.com"},
		{
			name: "bare domain with plugin", in: "host.example.com,plugin=cf",
			wantDomain: "host.example.com", wantPlugin: "cf",
		},
		{
			name:       "all options keyed",
			in:         "domain=host.example.com,plugin=cf,alias=alias.example.com",
			wantDomain: "host.example.com", wantPlugin: "cf",
			wantAlias: "alias.example.com",
		},
		{
			name: "order independent", in: "alias=a.example,plugin=cf,domain=h.example",
			wantDomain: "h.example", wantPlugin: "cf", wantAlias: "a.example",
		},
		{name: "unknown sub-key ignored", in: "h.example,future=1", wantDomain: "h.example"},
		// Malformed: the domain sub-key is required and has no default value.
		{name: "no domain", in: "plugin=cf,alias=a.example", wantErr: true},
		{name: "empty", in: "", wantErr: true},
		// PVE takes a bare token as the default key wherever it appears — its
		// own writer puts it first, but a hand-edited config need not.
		{
			name: "bare value not first", in: "plugin=cf,host.example.com",
			wantDomain: "host.example.com", wantPlugin: "cf",
		},
		{name: "leading empty part", in: ",host.example.com", wantDomain: "host.example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseACMEDomain(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseACMEDomain(%q) = %+v, want an error", tt.in, got)
				}
				if !errors.Is(err, svcutil.ErrMissingField) {
					t.Errorf("error = %v, want it to wrap ErrMissingField", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseACMEDomain(%q): %v", tt.in, err)
			}
			if got.Domain != tt.wantDomain {
				t.Errorf("Domain = %q, want %q", got.Domain, tt.wantDomain)
			}
			if got.Plugin != tt.wantPlugin {
				t.Errorf("Plugin = %q, want %q", got.Plugin, tt.wantPlugin)
			}
			if got.Alias != tt.wantAlias {
				t.Errorf("Alias = %q, want %q", got.Alias, tt.wantAlias)
			}
		})
	}
}

// TestACMEDomainRoundTrip pins the property that matters for read-modify-write:
// encoding a parsed value and parsing it again must be stable, whichever form
// the node config used.
func TestACMEDomainRoundTrip(t *testing.T) {
	t.Parallel()
	for _, in := range []string{
		"host.example.com",
		"host.example.com,plugin=cf",
		"domain=host.example.com,plugin=cf,alias=alias.example.com",
		"alias=a.example,plugin=cf,domain=h.example",
	} {
		t.Run(in, func(t *testing.T) {
			t.Parallel()
			first, err := parseACMEDomain(in)
			if err != nil {
				t.Fatalf("parse %q: %v", in, err)
			}
			encoded := encodeACMEDomain(first)
			second, err := parseACMEDomain(encoded)
			if err != nil {
				t.Fatalf("re-parse %q: %v", encoded, err)
			}
			if first != second {
				t.Errorf("round-trip changed the value: %+v -> %q -> %+v", first, encoded, second)
			}
			// And re-encoding is a fixed point, so a repeated write is a no-op.
			if again := encodeACMEDomain(second); again != encoded {
				t.Errorf("re-encode = %q, want the stable %q", again, encoded)
			}
		})
	}
}

func TestAcmeDomainSlot(t *testing.T) {
	t.Parallel()
	tests := []struct {
		key       string
		wantIndex int
		wantOK    bool
	}{
		{"acmedomain0", 0, true},
		{"acmedomain1", 1, true},
		{"acmedomain5", 5, true},
		// Above PVE's maximum, but the READ path stays permissive: a key the
		// SDK refuses to write is still one it must not lose.
		{"acmedomain42", 42, true},
		{"acmedomain", 0, false},  // the stem alone is not a slot.
		{"acmedomainx", 0, false}, // non-numeric suffix.
		{"acme", 0, false},        // the account key, not a slot.
		{"acmedomain-1", 0, false},
		// Non-canonical suffixes would collide: acmedomain0 and acmedomain00
		// would both parse to slot 0, and feeding the read back would trip the
		// writer's duplicate-index guard.
		{"acmedomain00", 0, false},
		{"acmedomain+1", 0, false},
		{"description", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			t.Parallel()
			index, ok := acmeDomainSlot(tt.key)
			if ok != tt.wantOK {
				t.Fatalf("acmeDomainSlot(%q) ok = %v, want %v", tt.key, ok, tt.wantOK)
			}
			if ok && index != tt.wantIndex {
				t.Errorf("index = %d, want %d", index, tt.wantIndex)
			}
		})
	}
	if got := ACMEDomainKey(3); got != "acmedomain3" {
		t.Errorf("ACMEDomainKey(3) = %q, want %q", got, "acmedomain3")
	}
}
