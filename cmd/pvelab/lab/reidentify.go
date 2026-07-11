package lab

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/ssh"
)

// Clone-boot cadence: a linked clone boots an installed system (~1–2 min),
// nothing like the 15-minute install ceiling.
const (
	cloneDialInterval  = 5 * time.Second
	cloneReadyCeiling  = 5 * time.Minute
	rebootProbeCeiling = 5 * time.Minute
)

// cloneSession is what the re-identify pass needs from an SSH connection to a
// booted clone: run commands, then close. *ssh.Client satisfies it.
type cloneSession interface {
	execer
	Close() error
}

var _ cloneSession = (*ssh.Client)(nil)

// cloneDialer opens an SSH session to a booted clone. A seam (the
// clusterDialer precedent) so tests drive the command flow with a scripted
// fake instead of a real SSH server.
type cloneDialer func(ctx context.Context, host string) (cloneSession, error)

// ReidentifyClones boots the stopped clones ONE AT A TIME and rewrites each
// one's identity from the template's to its node's: every clone wakes up with
// the template's baked-in hostname, static IP, and SSH host key, so parallel
// starts would collide on one address. Per node: start → SSH in at the
// template's IP (retried while the clone boots) → rewrite hostname, /etc/hosts,
// /etc/network/interfaces, regenerate SSH host keys, move the pmxcfs node dir
// → reboot → poll the node's OWN endpoint until its API answers → next clone.
//
// PVE tolerating this hostname/IP rename end-to-end is the clone path's
// load-bearing LIVE-VERIFY unknown (see the IMPL-0002 Phase 5 notes): unit
// tests pin the command sequence and serialization, never PVE's behaviour.
func ReidentifyClones(ctx context.Context, c *proxmox.Client, cfg *Config, rootPassword string, logger *slog.Logger) ([]NodeReadiness, error) {
	return reidentifyClones(ctx, c, cfg, nestedCloneDialer(c, rootPassword),
		versionProbe(rootPassword), cloneDialInterval, cloneReadyCeiling, logger)
}

// reidentifyClones is ReidentifyClones with the SSH dialer, readiness probe,
// and cadence injected for tests.
func reidentifyClones(ctx context.Context, c *proxmox.Client, cfg *Config, dial cloneDialer,
	probe readyProbe, interval, ceiling time.Duration, logger *slog.Logger,
) ([]NodeReadiness, error) {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	t := cfg.Nested.Template
	if t == nil {
		return nil, errors.New("nested.template is not configured — the clone path needs its CIDR (the template's boot address)")
	}
	tPrefix, err := netip.ParsePrefix(t.CIDR)
	if err != nil {
		return nil, fmt.Errorf("nested.template.cidr: %w", err)
	}
	templateIP := tPrefix.Addr().String()
	oldName := templateNodeName(cfg)

	svc := c.QEMU(cfg.Outer.Node)
	results := make([]NodeReadiness, len(cfg.Nested.Nodes))
	for i, n := range cfg.Nested.Nodes {
		results[i] = NodeReadiness{Node: n.Name}
		start := time.Now()

		logger.Info("starting clone — it boots with the template's identity", "node", n.Name, "vmid", n.VMID)
		ref, err := svc.Start(ctx, n.VMID)
		if err != nil {
			return results, fmt.Errorf("start clone %d (%s): %w", n.VMID, n.Name, err)
		}
		if _, err := c.Tasks().Wait(ctx, ref); err != nil {
			return results, fmt.Errorf("wait start clone %d (%s): %w", n.VMID, n.Name, err)
		}

		if err := reidentifyOne(ctx, cfg, n, oldName, templateIP, dial, interval, ceiling, logger); err != nil {
			return results, err
		}

		endpoint, err := nodeEndpoint(n)
		if err != nil {
			return results, err
		}
		if err := awaitEndpoint(ctx, probe, endpoint, n.Name, interval, ceiling, logger); err != nil {
			return results, err
		}
		results[i].Ready = true
		results[i].Elapsed = time.Since(start)
		logger.Info("clone re-identified and ready", "node", n.Name,
			"elapsed", results[i].Elapsed.Round(time.Second).String())
	}
	return results, nil
}

// reidentifyOne dials the freshly booted clone at the template's address
// (retrying while it boots), runs the identity-rewrite script, and reboots it.
// The reboot drops the SSH connection, so that exec's error is expected and
// only logged.
func reidentifyOne(ctx context.Context, cfg *Config, n Node, oldName, templateIP string,
	dial cloneDialer, interval, ceiling time.Duration, logger *slog.Logger,
) error {
	sess, err := dialWithRetry(ctx, n, templateIP, dial, interval, ceiling, logger)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := sess.Close(); cerr != nil {
			logger.Debug("close clone ssh session", "node", n.Name, "err", cerr)
		}
	}()

	script := reidentifyScript(oldName, templateIP, cfg.Nested.Template.CIDR, n)
	logger.Info("rewriting clone identity", "node", n.Name, "from", oldName, "to", n.Name)
	if out, err := sess.Exec(ctx, script); err != nil {
		return fmt.Errorf("re-identify clone %s: %w (output: %s)", n.Name, err, out)
	}
	if _, err := sess.Exec(ctx, "reboot"); err != nil {
		logger.Debug("reboot exec returned an error — expected, the connection drops", "node", n.Name, "err", err)
	}
	return nil
}

// dialWithRetry polls the clone's SSH port on the template address until the
// booting system answers, bounded by ceiling.
func dialWithRetry(ctx context.Context, n Node, host string, dial cloneDialer,
	interval, ceiling time.Duration, logger *slog.Logger,
) (cloneSession, error) {
	dialCtx, cancel := context.WithTimeout(ctx, ceiling)
	defer cancel()
	for {
		sess, err := dial(dialCtx, host)
		if err == nil {
			return sess, nil
		}
		logger.Debug("clone not SSH-able yet", "node", n.Name, "host", host, "err", err)
		select {
		case <-dialCtx.Done():
			return nil, fmt.Errorf("clone %s never became SSH-able at the template address %s within %s", n.Name, host, ceiling)
		case <-time.After(interval):
		}
	}
}

// awaitEndpoint polls one endpoint with probe until it answers, bounded by
// ceiling (the single-node version of waitReady's inner loop).
func awaitEndpoint(ctx context.Context, probe readyProbe, endpoint, node string,
	interval, ceiling time.Duration, logger *slog.Logger,
) error {
	pollCtx, cancel := context.WithTimeout(ctx, ceiling)
	defer cancel()
	for {
		if err := probe(pollCtx, endpoint); err == nil {
			return nil
		}
		logger.Debug("re-identified clone not answering yet", "node", node, "endpoint", endpoint)
		select {
		case <-pollCtx.Done():
			return fmt.Errorf("node %s (%s) not ready within %s after re-identify", node, endpoint, ceiling)
		case <-time.After(interval):
		}
	}
}

// reidentifyScript renders the shell script that rewrites a booted clone's
// identity from the template's to node n's. The /etc/pve/nodes move is
// best-effort (`|| true`): on a fresh clone the old directory holds only
// per-node certificates, which PVE regenerates for the new name at boot.
func reidentifyScript(oldName, oldIP, oldCIDR string, n Node) string {
	lines := []string{
		"set -e",
		"hostnamectl set-hostname " + n.Name,
		fmt.Sprintf("sed -i 's/%s/%s/g; s/%s/%s/g' /etc/hosts", oldIP, mustNodeIP(n), oldName, n.Name),
		fmt.Sprintf("sed -i 's|address %s|address %s|' /etc/network/interfaces", oldCIDR, n.CIDR),
		"rm -f /etc/ssh/ssh_host_*",
		"ssh-keygen -A",
		fmt.Sprintf("mv /etc/pve/nodes/%s /etc/pve/nodes/%s || true", oldName, n.Name),
	}
	return strings.Join(lines, "\n")
}

// mustNodeIP is n.IP for script rendering; the CIDR was validated at config
// load, so a parse failure here is a programming error.
func mustNodeIP(n Node) string {
	ip, err := n.IP()
	if err != nil {
		panic(err)
	}
	return ip
}

// nestedCloneDialer builds the production dialer: root@<template IP> with the
// lab's shared root password, host keys pinned trust-on-first-use for the run.
func nestedCloneDialer(c *proxmox.Client, rootPassword string) cloneDialer {
	hostKey := tofuHostKeyCallback() // shared across the serialized dials.
	return func(ctx context.Context, host string) (cloneSession, error) {
		sc := c.SSH(
			ssh.WithUser("root"),
			ssh.WithPassword(rootPassword),
			ssh.WithHostKeyCallback(hostKey),
		)
		if err := sc.Connect(ctx, host); err != nil {
			return nil, err
		}
		return sc, nil
	}
}

// tofuHostKeyCallback pins the first host key seen this run: every
// not-yet-re-identified clone presents the template's identical baked-in key,
// so the first contact defines the expectation and every later clone must
// match it. Deliberate trust-on-first-use — the template's key is not known
// before the first dial (the build regenerated nothing), the bridge is
// lab-internal, and the re-identify script replaces each clone's keys
// immediately after this one connection.
func tofuHostKeyCallback() gossh.HostKeyCallback {
	var mu sync.Mutex
	var pinned gossh.PublicKey
	return func(hostname string, _ net.Addr, key gossh.PublicKey) error {
		mu.Lock()
		defer mu.Unlock()
		if pinned == nil {
			pinned = key
			return nil
		}
		if pinned.Type() == key.Type() && bytes.Equal(pinned.Marshal(), key.Marshal()) {
			return nil
		}
		return fmt.Errorf("clone at %s presented a host key that differs from the template's first-seen key (%s)", hostname, key.Type())
	}
}
