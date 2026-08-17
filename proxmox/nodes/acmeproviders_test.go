package nodes

import (
	"encoding/base64"
	"strings"
	"testing"
)

// decodePluginData is the test-side inverse of encodePluginData: the SDK offers
// no decode helper on purpose (a consumer printing a plugin dumps base64, not
// plaintext credentials), so the tests do it themselves.
func decodePluginData(t *testing.T, encoded string) string {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode plugin data: %v", err)
	}
	return string(raw)
}

func TestCloudflareData(t *testing.T) {
	t.Parallel()
	cf := Cloudflare{Token: "cf-token", AccountID: "acct-1"}
	if got, want := cf.API(), "cf"; got != want {
		t.Errorf("API() = %q, want %q", got, want)
	}
	// Sorted keys, and the three unset fields are absent rather than blank.
	if got, want := decodePluginData(t, encodePluginData(cf)),
		"CF_Account_ID=acct-1\nCF_Token=cf-token"; got != want {
		t.Errorf("data = %q, want %q", got, want)
	}
}

func TestCloudflareDataLegacyKey(t *testing.T) {
	t.Parallel()
	cf := Cloudflare{Key: "global-key", Email: "ops@example.com", ZoneID: "zone-9"}
	if got, want := decodePluginData(t, encodePluginData(cf)),
		"CF_Email=ops@example.com\nCF_Key=global-key\nCF_Zone_ID=zone-9"; got != want {
		t.Errorf("data = %q, want %q", got, want)
	}
}

func TestNamecheapData(t *testing.T) {
	t.Parallel()
	nc := Namecheap{Username: "user", APIKey: "nc-key", SourceIP: "203.0.113.7"}
	if got, want := nc.API(), "namecheap"; got != want {
		t.Errorf("API() = %q, want %q", got, want)
	}
	if got, want := decodePluginData(t, encodePluginData(nc)),
		"NAMECHEAP_API_KEY=nc-key\nNAMECHEAP_SOURCEIP=203.0.113.7\nNAMECHEAP_USERNAME=user"; got != want {
		t.Errorf("data = %q, want %q", got, want)
	}
}

func TestRawPluginData(t *testing.T) {
	t.Parallel()
	raw := RawPluginData{
		Provider: "desec",
		Values:   map[string]string{"DEDYN_TOKEN": "tok", "DEDYN_NAME": "host.dedyn.io"},
	}
	if got, want := raw.API(), "desec"; got != want {
		t.Errorf("API() = %q, want %q", got, want)
	}
	if got, want := decodePluginData(t, encodePluginData(raw)),
		"DEDYN_NAME=host.dedyn.io\nDEDYN_TOKEN=tok"; got != want {
		t.Errorf("data = %q, want %q", got, want)
	}
}

// TestEncodePluginDataEmpty covers the standalone case: a challenge type with no
// credentials at all renders to an empty payload, not to a stray newline.
func TestEncodePluginDataEmpty(t *testing.T) {
	t.Parallel()
	if got := encodePluginData(RawPluginData{Provider: "x"}); got != "" {
		t.Errorf("data = %q, want empty", got)
	}
	if got := encodePluginData(Cloudflare{}); got != "" {
		t.Errorf("data = %q, want empty for a zero Cloudflare", got)
	}
}

// TestEncodePluginDataStable pins determinism: the render is called repeatedly
// on the same input and must not vary with Go's map iteration order. Without the
// sort this fails within a handful of iterations.
func TestEncodePluginDataStable(t *testing.T) {
	t.Parallel()
	cf := Cloudflare{Token: "t", AccountID: "a", ZoneID: "z", Key: "k", Email: "e"}
	first := encodePluginData(cf)
	for i := 0; i < 64; i++ {
		if got := encodePluginData(cf); got != first {
			t.Fatalf("iteration %d: data = %q, want the stable %q", i, got, first)
		}
	}
	// All five fields present, in sorted order.
	if got, want := decodePluginData(t, first),
		"CF_Account_ID=a\nCF_Email=e\nCF_Key=k\nCF_Token=t\nCF_Zone_ID=z"; got != want {
		t.Errorf("data = %q, want %q", got, want)
	}
}

// TestEncodePluginDataNoPlaintextLeak guards the one property the base64 is
// there for: the rendered value must not contain the credential verbatim, so a
// consumer that logs a spec by accident does not print the token.
func TestEncodePluginDataNoPlaintextLeak(t *testing.T) {
	t.Parallel()
	const token = "super-secret-token"
	if got := encodePluginData(Cloudflare{Token: token}); strings.Contains(got, token) {
		t.Errorf("encoded data %q contains the plaintext token", got)
	}
}
