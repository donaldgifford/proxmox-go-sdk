package coverage

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/donaldgifford/proxmox-go-sdk/cmd/pve-schemadiff/schema"
)

// apiPrefix is the REST root every mockpve pattern carries and no baseline path
// does; normalization strips it so the two sides are comparable.
const apiPrefix = "/api2/json"

// wildcard matches one ServeMux/apidoc path placeholder, e.g. the {node} in
// /nodes/{node}/qemu. Both sides use single-segment placeholders only (verified
// against the committed baseline and the mock's patterns), so a non-greedy
// character class is sufficient.
var wildcard = regexp.MustCompile(`\{[^}]*\}`)

// Key is the normalized identity of one endpoint — the form in which a mockpve
// route and a real PVE endpoint can be compared. Placeholder names are erased
// ("{}") because the two sides name them differently: PVE's /nodes/{node}/qemu/{vmid}
// is the mock's /api2/json/nodes/{node}/qemu/{vmid} but PVE's
// /cluster/sdn/fabrics/fabric/{id} is the mock's .../fabric/{fabric}.
//
// Build a Key with [ParsePattern] or [EndpointKey]; the zero value is not a
// valid key. Keys are comparable, so they work directly as map keys.
type Key struct {
	Method string // upper-case HTTP verb, e.g. "GET".
	Path   string // normalized path, prefix stripped and wildcards erased.
}

// String renders the key as "METHOD path", the form used in report tables and
// error messages.
func (k Key) String() string { return k.Method + " " + k.Path }

// NormalizePath strips the /api2/json prefix (if present) and erases every
// placeholder name, so /api2/json/nodes/{node}/qemu/{vmid}/config and
// /nodes/{node}/qemu/{id}/config both become /nodes/{}/qemu/{}/config.
func NormalizePath(path string) string {
	return wildcard.ReplaceAllString(strings.TrimPrefix(path, apiPrefix), "{}")
}

// ParsePattern splits a mockpve route pattern — a Go 1.22 ServeMux string as
// [mockpve.Server.Routes] reports it, e.g. "GET /api2/json/nodes/{node}/qemu" —
// into a normalized Key. It errors on a pattern that is not "METHOD /path",
// since a method-less pattern cannot be compared against the baseline (which is
// keyed by verb) and silently dropping it would inflate coverage.
func ParsePattern(pattern string) (Key, error) {
	method, path, ok := strings.Cut(strings.TrimSpace(pattern), " ")
	if !ok {
		return Key{}, fmt.Errorf("coverage: route pattern %q has no method: want \"METHOD /path\"", pattern)
	}
	path = strings.TrimSpace(path)
	if method == "" || !strings.HasPrefix(path, "/") {
		return Key{}, fmt.Errorf("coverage: route pattern %q is malformed: want \"METHOD /path\"", pattern)
	}
	return Key{Method: strings.ToUpper(method), Path: NormalizePath(path)}, nil
}

// EndpointKey normalizes a baseline endpoint into a Key. Baseline paths carry no
// /api2/json prefix, so only the placeholder names are erased.
func EndpointKey(ep schema.Endpoint) Key {
	return Key{Method: strings.ToUpper(ep.Method), Path: NormalizePath(ep.Path)}
}

// ParsePatterns normalizes a whole route table, reporting the first malformed
// pattern. The result preserves input order and may contain duplicates: two
// mock routes whose placeholder names differ only in spelling normalize to the
// same Key, which callers de-duplicate as they index.
func ParsePatterns(patterns []string) ([]Key, error) {
	keys := make([]Key, 0, len(patterns))
	for _, p := range patterns {
		k, err := ParsePattern(p)
		if err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, nil
}
