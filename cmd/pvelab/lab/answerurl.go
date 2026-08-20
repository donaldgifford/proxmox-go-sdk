package lab

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strings"
)

// CheckAnswerURLLocal verifies that nested.answer_url names the machine about
// to run `up`.
//
// The answer fetch is the only connection the install flow initiates *toward*
// whoever runs `up`: the URL is baked into the ISO at `pvelab iso` time, and
// the answer server is an ephemeral listener inside the `up` process. Point it
// at a different host and every installer POSTs into the void — a failure that
// surfaces fifteen minutes later as three identical readiness timeouts, with
// nothing in the log to say why, because the server that would have logged the
// request never saw one.
//
// Checking it costs a DNS lookup and turns that into an immediate error.
func (c *Config) CheckAnswerURLLocal(ctx context.Context) error {
	addrs, err := localAddrs()
	if err != nil {
		return err
	}
	return checkAnswerURLHost(ctx, c.Nested.AnswerURL, addrs)
}

// localAddrs collects the addresses of this machine's interfaces.
func localAddrs() ([]netip.Addr, error) {
	ifaceAddrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, fmt.Errorf("read local interface addresses: %w", err)
	}
	out := make([]netip.Addr, 0, len(ifaceAddrs))
	for _, a := range ifaceAddrs {
		prefix, perr := netip.ParsePrefix(a.String())
		if perr != nil {
			continue // a non-IP interface address is nothing to compare against.
		}
		out = append(out, prefix.Addr())
	}
	return out, nil
}

// checkAnswerURLHost is the comparison, split out so it is testable without
// depending on the machine's interfaces.
func checkAnswerURLHost(ctx context.Context, rawURL string, local []netip.Addr) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("nested.answer_url %q: %w", rawURL, err)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("nested.answer_url %q names no host", rawURL)
	}

	wanted, err := resolveHost(ctx, host)
	if err != nil {
		return fmt.Errorf("nested.answer_url %q: %w", rawURL, err)
	}
	for _, w := range wanted {
		// Loopback is local but useless: the installers are on another machine
		// entirely, so they can never reach it. Worth its own message — the
		// generic one would send the operator looking for a routing problem.
		if w.IsLoopback() {
			return fmt.Errorf(
				"nested.answer_url %q is a loopback address; the nested installers must reach it from the lab network, so it needs this machine's routable address",
				rawURL,
			)
		}
		for _, l := range local {
			if w == l {
				return nil
			}
		}
	}
	return fmt.Errorf(
		"nested.answer_url %q resolves to %s, which is not an address of this machine (%s).\n"+
			"The installers POST their answer request to that URL, so it must name the machine running `up`. Either:\n"+
			"  - run `pvelab up` on that host, or\n"+
			"  - set answer_url to this machine's address reachable from the lab network, then re-run `pvelab iso` (the URL is baked into the ISO)",
		rawURL, joinAddrs(wanted), joinAddrs(local))
}

// resolveHost turns the URL's host into addresses, accepting a literal IP
// without asking a resolver about it.
func resolveHost(ctx context.Context, host string) ([]netip.Addr, error) {
	if addr, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{addr}, nil
	}
	names, err := net.DefaultResolver.LookupHost(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("resolve %q: %w", host, err)
	}
	out := make([]netip.Addr, 0, len(names))
	for _, n := range names {
		if addr, perr := netip.ParseAddr(n); perr == nil {
			out = append(out, addr)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("resolved to no usable address")
	}
	return out, nil
}

func joinAddrs(addrs []netip.Addr) string {
	parts := make([]string, 0, len(addrs))
	for _, a := range addrs {
		parts = append(parts, a.String())
	}
	return strings.Join(parts, ", ")
}
