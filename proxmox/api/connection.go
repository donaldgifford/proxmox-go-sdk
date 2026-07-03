package api

import (
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"
	"sync"
)

// defaultPort is the PVE API port assumed when an endpoint address omits one.
const defaultPort = "8006"

// Endpoint is one node address in a PVE cluster. Lower Priority is tried first;
// ties preserve declaration order.
type Endpoint struct {
	// Name is informational, e.g. "pve-node1".
	Name string
	// Address is a host, host:port, or https://host:port.
	Address string
	// Priority orders failover: 0 is the most preferred.
	Priority int
}

// connection holds the priority-ordered, de-duplicated endpoint set and tracks
// which one is currently active. Failover is sticky and passive (see OQ-2): the
// active endpoint only advances on a transport error and never drifts back.
type connection struct {
	mu      sync.Mutex
	ordered []parsedEndpoint
	current int // index into ordered; guarded by mu
}

type parsedEndpoint struct {
	Endpoint
	base *url.URL // normalised: scheme defaulted to https, explicit port, no path/query
}

// newConnection builds a connection from the primary address (always priority
// 0) plus any extra cluster endpoints. Duplicate addresses are dropped.
func newConnection(primary string, extras []Endpoint) (*connection, error) {
	all := make([]Endpoint, 0, 1+len(extras))
	all = append(all, Endpoint{Name: "primary", Address: primary, Priority: 0})
	all = append(all, extras...)

	parsed := make([]parsedEndpoint, 0, len(all))
	seen := make(map[string]struct{}, len(all))
	for _, ep := range all {
		base, err := normaliseAddress(ep.Address)
		if err != nil {
			return nil, fmt.Errorf("api: endpoint %q: %w", ep.Address, err)
		}
		key := base.String()
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		parsed = append(parsed, parsedEndpoint{Endpoint: ep, base: base})
	}

	sort.SliceStable(parsed, func(i, j int) bool {
		return parsed[i].Priority < parsed[j].Priority
	})

	return &connection{ordered: parsed}, nil
}

// baseURL returns a copy of the active endpoint's base URL, so callers cannot
// mutate the stored value.
func (c *connection) baseURL() *url.URL {
	c.mu.Lock()
	defer c.mu.Unlock()
	u := *c.ordered[c.current].base
	return &u
}

// failover advances to the next endpoint by priority order. It reports false
// when there is only one endpoint (nothing to rotate to).
func (c *connection) failover() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.ordered) <= 1 {
		return false
	}
	c.current = (c.current + 1) % len(c.ordered)
	return true
}

// count reports the number of distinct endpoints.
func (c *connection) count() int { return len(c.ordered) }

// normaliseAddress coerces an address to an https base URL with an explicit
// port (defaulting to 8006) and no path or query.
func normaliseAddress(addr string) (*url.URL, error) {
	if addr == "" {
		return nil, fmt.Errorf("empty address")
	}
	if !strings.Contains(addr, "://") {
		addr = "https://" + addr
	}
	u, err := url.Parse(addr)
	if err != nil {
		return nil, err
	}
	if u.Hostname() == "" {
		return nil, fmt.Errorf("missing host")
	}
	if u.Port() == "" {
		u.Host = net.JoinHostPort(u.Hostname(), defaultPort)
	}
	u.Path = ""
	u.RawQuery = ""
	u.Fragment = ""
	return u, nil
}
