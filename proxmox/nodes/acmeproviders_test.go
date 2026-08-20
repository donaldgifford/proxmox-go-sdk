package nodes

import (
	"encoding/base64"
	"fmt"
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
	cf := ACMECloudflare{Token: "cf-token", AccountID: "acct-1"}
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
	cf := ACMECloudflare{Key: "global-key", Email: "ops@example.com", ZoneID: "zone-9"}
	if got, want := decodePluginData(t, encodePluginData(cf)),
		"CF_Email=ops@example.com\nCF_Key=global-key\nCF_Zone_ID=zone-9"; got != want {
		t.Errorf("data = %q, want %q", got, want)
	}
}

func TestNamecheapData(t *testing.T) {
	t.Parallel()
	nc := ACMENamecheap{Username: "user", APIKey: "nc-key", SourceIP: "203.0.113.7"}
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
	raw := ACMERawPluginData{
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
	if got := encodePluginData(ACMERawPluginData{Provider: "x"}); got != "" {
		t.Errorf("data = %q, want empty", got)
	}
	if got := encodePluginData(ACMECloudflare{}); got != "" {
		t.Errorf("data = %q, want empty for a zero Cloudflare", got)
	}
}

// TestEncodePluginDataStable pins determinism: the render is called repeatedly
// on the same input and must not vary with Go's map iteration order. Without the
// sort this fails within a handful of iterations.
func TestEncodePluginDataStable(t *testing.T) {
	t.Parallel()
	cf := ACMECloudflare{Token: "t", AccountID: "a", ZoneID: "z", Key: "k", Email: "e"}
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
	if got := encodePluginData(ACMECloudflare{Token: token}); strings.Contains(got, token) {
		t.Errorf("encoded data %q contains the plaintext token", got)
	}
}

// TestProviderStringRedacts is the write-side counterpart to the read type's
// String: the first consumer is a service, not a human at a REPL, and a debug
// line like slog.Debug("spec", "spec", spec) must not put a live token into a
// log pipeline. fmt consults String on the value inside the interface field, so
// redacting each provider covers the enclosing spec too.
func TestProviderStringRedacts(t *testing.T) {
	t.Parallel()
	const (
		token = "live-cf-token"
		key   = "live-nc-key"
		raw   = "live-raw-value"
	)
	spec := &ACMEPluginSpec{ID: "cf", Data: ACMECloudflare{Token: token, Key: "global-key"}}
	for _, verb := range []string{"%v", "%s", "%+v"} {
		if got := fmt.Sprintf(verb, spec); strings.Contains(got, token) {
			t.Errorf("%s of a spec leaked the token: %s", verb, got)
		}
	}
	// Through an any, which is how a value reaches slog — and the only form that
	// exercises fmt's dispatch to String rather than calling it directly.
	var nc any = ACMENamecheap{APIKey: key}
	if got := fmt.Sprintf("%v", nc); strings.Contains(got, key) {
		t.Errorf("Namecheap %%v leaked the key: %s", got)
	}
	var rawData any = ACMERawPluginData{
		Provider: "gandi", Values: map[string]string{"GANDI_TOKEN": raw},
	}
	rendered := fmt.Sprintf("%v", rawData)
	if strings.Contains(rendered, raw) {
		t.Errorf("raw %%v leaked the value: %s", rendered)
	}
	// The provider name is not a secret, and keeping it makes the redacted line
	// useful for debugging.
	if !strings.Contains(rendered, "gandi") {
		t.Errorf("raw %%v = %q, want it to still name the provider", rendered)
	}
}
