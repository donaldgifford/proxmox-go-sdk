package nodes_test

import (
	"context"
	"strings"
	"testing"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/mockpve"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/nodes"
)

func TestGetChallengeSchema(t *testing.T) {
	t.Parallel()
	svc := newService(t, mockpve.New())

	entries, err := svc.GetChallengeSchema(context.Background())
	if err != nil {
		t.Fatalf("GetChallengeSchema: %v", err)
	}
	if len(entries) < 2 {
		t.Fatalf("GetChallengeSchema returned %d entries, want at least 2", len(entries))
	}
	byID := make(map[string]nodes.ChallengeSchemaEntry, len(entries))
	for _, e := range entries {
		byID[e.ID] = e
	}
	// The ids are exactly what a plugin's api field expects, so the typed
	// providers' API() values must appear here.
	for _, want := range []string{nodes.Cloudflare{}.API(), nodes.Namecheap{}.API()} {
		entry, ok := byID[want]
		if !ok {
			t.Fatalf("challenge schema has no entry for %q", want)
		}
		if len(entry.Schema) == 0 {
			t.Errorf("%q: Schema is empty, want the raw provider field schema", want)
		}
	}
	// Cloudflare's schema should mention the variable the typed struct renders —
	// this is the in-repo half of the live confirm-the-field-names check.
	if got := string(byID["cf"].Schema); !strings.Contains(got, "CF_Token") {
		t.Errorf("cf schema = %s, want it to mention CF_Token", got)
	}
}

func TestListACMEDirectories(t *testing.T) {
	t.Parallel()
	svc := newService(t, mockpve.New())

	dirs, err := svc.ListACMEDirectories(context.Background())
	if err != nil {
		t.Fatalf("ListACMEDirectories: %v", err)
	}
	if len(dirs) == 0 {
		t.Fatal("ListACMEDirectories returned nothing")
	}
	var staging bool
	for _, d := range dirs {
		if d.Name == "" || d.URL == "" {
			t.Errorf("directory %+v has an empty field", d)
		}
		if strings.Contains(d.URL, "staging") {
			staging = true
		}
	}
	// Staging is the directory a test suite should point at, so its presence is
	// worth asserting rather than assuming.
	if !staging {
		t.Error("no staging directory returned")
	}
}

func TestGetACMEMeta(t *testing.T) {
	t.Parallel()
	svc := newService(t, mockpve.New())

	meta, err := svc.GetACMEMeta(context.Background())
	if err != nil {
		t.Fatalf("GetACMEMeta: %v", err)
	}
	if meta.TermsOfService == "" {
		t.Error("TermsOfService is empty; it is what ACMEAccountSpec.TOSURL needs")
	}
	if len(meta.CAAIdentities) == 0 {
		t.Error("CAAIdentities is empty")
	}
	// The mock serves a key the SDK does not model; the lossless read must keep it.
	if _, ok := meta.Extra["externalAccountBinding"]; !ok {
		t.Errorf("Extra = %v, want the unmodelled externalAccountBinding key", meta.Extra)
	}
}

// TestGetACMEMetaWithDirectory proves the option reaches the wire as a query
// parameter: the mock derives its answer from the directory it was asked about.
func TestGetACMEMetaWithDirectory(t *testing.T) {
	t.Parallel()
	svc := newService(t, mockpve.New())

	const staging = "https://acme-staging-v02.api.letsencrypt.org/directory"
	meta, err := svc.GetACMEMeta(context.Background(), nodes.WithACMEDirectory(staging))
	if err != nil {
		t.Fatalf("GetACMEMeta: %v", err)
	}
	if want := staging + "/terms"; meta.TermsOfService != want {
		t.Errorf("TermsOfService = %q, want %q — the directory option did not reach the wire",
			meta.TermsOfService, want)
	}
}
