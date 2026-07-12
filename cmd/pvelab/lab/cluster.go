package lab

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/api"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/cluster"
)

// Cluster-formation cadence (DESIGN-0002): joins are serialized (corosync
// membership changes must not race) and each converges in two stages, both
// bounded at ~3 min: the node appears in the first node's corosync nodelist,
// then the cluster reports quorate with every member so far online. The
// second stage exists because config presence precedes runtime health — see
// the gate note in formCluster.
const (
	memberPollInterval  = 5 * time.Second
	joinConvergeCeiling = 3 * time.Minute
	quorumCeiling       = 3 * time.Minute
)

// clusterDialer opens a cluster API session to one nested node's endpoint —
// the test seam. Production dials a fresh SDK client per call (cheap, and
// immune to tickets invalidated by the daemon restarts formation causes).
type clusterDialer func(ctx context.Context, endpoint string) (cluster.API, error)

// FormCluster turns the freshly installed standalone nodes into one quorate
// cluster: create on the first node → JoinInfo → serialized joins for the
// rest, each tolerating the mid-join connection drop and converging via the
// first node's corosync nodelist AND a /cluster/status quorum gate before the
// next join is issued (the last gate doubles as the final quorum check).
func FormCluster(ctx context.Context, cfg *Config, rootPassword string, logger *slog.Logger) error {
	return formCluster(ctx, cfg, rootPassword, nestedClusterDialer(rootPassword),
		memberPollInterval, joinConvergeCeiling, quorumCeiling, logger)
}

// nestedClusterDialer builds a fresh SDK client for a nested node's endpoint
// with root@pam password credentials (API tokens do not survive a join) and
// insecure TLS (fresh installs are self-signed; certs churn again at join).
func nestedClusterDialer(rootPassword string) clusterDialer {
	return func(ctx context.Context, endpoint string) (cluster.API, error) {
		c, err := proxmox.NewClient(ctx, endpoint,
			api.UserCredentials("root@pam", rootPassword, ""),
			proxmox.WithInsecureSkipVerify(true),
			proxmox.WithRequestTimeout(probeTimeout))
		if err != nil {
			return nil, err
		}
		return c.Cluster(), nil
	}
}

func formCluster(ctx context.Context, cfg *Config, rootPassword string, dial clusterDialer,
	interval, joinCeiling, quorumBound time.Duration, logger *slog.Logger,
) error {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	nodes := cfg.Nested.Nodes
	first := nodes[0]
	firstEndpoint, err := nodeEndpoint(first)
	if err != nil {
		return err
	}
	firstIP, err := first.IP()
	if err != nil {
		return err
	}

	svc, err := dial(ctx, firstEndpoint)
	if err != nil {
		return fmt.Errorf("dial %s (%s): %w", first.Name, firstEndpoint, err)
	}
	if err := svc.CreateCluster(ctx, &cluster.ClusterCreateSpec{Name: cfg.Nested.ClusterName}); err != nil {
		return fmt.Errorf("create cluster on %s: %w", first.Name, err)
	}
	logger.Info("cluster created", "name", cfg.Nested.ClusterName, "node", first.Name)
	if err := waitForMember(ctx, dial, firstEndpoint, first.Name, interval, joinCeiling, logger); err != nil {
		return err
	}

	// Fresh session for join-info: create may have churned the first node's
	// daemons/tickets underneath the session that issued it.
	svc, err = dial(ctx, firstEndpoint)
	if err != nil {
		return fmt.Errorf("dial %s (%s): %w", first.Name, firstEndpoint, err)
	}
	info, err := svc.JoinInfo(ctx)
	if err != nil {
		return fmt.Errorf("join-info from %s: %w", first.Name, err)
	}
	fingerprint := info.Fingerprint()
	if fingerprint == "" {
		return fmt.Errorf("join-info from %s carries no certificate fingerprint", first.Name)
	}

	// Serialized joins: corosync membership changes must not race.
	for i, n := range nodes[1:] {
		if err := joinNode(ctx, dial, n, firstEndpoint, firstIP, rootPassword,
			fingerprint, interval, joinCeiling, logger); err != nil {
			return err
		}
		// Config presence is not runtime health: the joined node lands in
		// corosync.conf (raising expected votes) BEFORE its corosync comes
		// online, and in that window the cluster is not quorate — pmxcfs is
		// read-only, so a join fired into it is accepted by the joining
		// node's API yet fails server-side, surfacing only as the next
		// membership timeout. Found live 2026-07-12: pve3's join issued ~1s
		// after pve2 entered the nodelist, and pve3 never converged. Gate on
		// the cluster actually being quorate with every member so far online
		// before touching the next node; the last iteration's gate is the
		// final quorum check.
		if err := waitForQuorum(ctx, dial, firstEndpoint, i+2,
			interval, quorumBound, logger); err != nil {
			return err
		}
		logger.Info("cluster quorate", "members", i+2)
	}

	logger.Info("cluster formed", "name", cfg.Nested.ClusterName, "nodes", len(nodes))
	return nil
}

// joinNode issues one node's join and waits for it to appear in the first
// node's corosync nodelist.
func joinNode(ctx context.Context, dial clusterDialer, n Node,
	firstEndpoint, firstIP, rootPassword, fingerprint string,
	interval, ceiling time.Duration, logger *slog.Logger,
) error {
	endpoint, err := nodeEndpoint(n)
	if err != nil {
		return err
	}
	joinSvc, err := dial(ctx, endpoint)
	if err != nil {
		return fmt.Errorf("dial %s (%s): %w", n.Name, endpoint, err)
	}
	// Password is the FIRST node's root@pam password — the join authenticates
	// against the contacted member; every node shares one root password in
	// this lab.
	err = joinSvc.JoinCluster(ctx, &cluster.JoinSpec{
		Hostname:    firstIP,
		Password:    rootPassword,
		Fingerprint: fingerprint,
	})
	if err != nil {
		// Expected failure mode: the join restarts the joining node's API
		// daemons mid-call and the connection drops. Convergence below is the
		// real signal; a genuinely rejected join never converges and surfaces
		// as the poll timeout.
		logger.Warn("join request errored — relying on membership convergence",
			"node", n.Name, "err", err)
	}
	if err := waitForMember(ctx, dial, firstEndpoint, n.Name, interval, ceiling, logger); err != nil {
		return fmt.Errorf("node %s never appeared in the cluster membership: %w", n.Name, err)
	}
	logger.Info("node joined", "node", n.Name)
	return nil
}

// waitForMember polls the corosync nodelist at endpoint until member appears,
// every interval, bounded by ceiling. Poll errors count as "not yet" — the
// polled node's API may itself be restarting.
func waitForMember(ctx context.Context, dial clusterDialer, endpoint, member string,
	interval, ceiling time.Duration, logger *slog.Logger,
) error {
	pollCtx, cancel := context.WithTimeout(ctx, ceiling)
	defer cancel()
	for {
		found, err := memberPresent(pollCtx, dial, endpoint, member)
		if err == nil && found {
			return nil
		}
		logger.Debug("membership not converged yet", "member", member, "err", err)
		select {
		case <-pollCtx.Done():
			return fmt.Errorf("member %s not in the corosync nodelist at %s within %s",
				member, endpoint, ceiling)
		case <-time.After(interval):
		}
	}
}

func memberPresent(ctx context.Context, dial clusterDialer, endpoint, member string) (bool, error) {
	svc, err := dial(ctx, endpoint)
	if err != nil {
		return false, err
	}
	members, err := svc.ListConfigNodes(ctx)
	if err != nil {
		return false, err
	}
	for i := range members {
		if members[i].NodeName() == member {
			return true, nil
		}
	}
	return false, nil
}

// waitForQuorum polls /cluster/status at endpoint until the cluster entry
// reports want members and quorate, with want node entries online.
func waitForQuorum(ctx context.Context, dial clusterDialer, endpoint string, want int,
	interval, ceiling time.Duration, logger *slog.Logger,
) error {
	pollCtx, cancel := context.WithTimeout(ctx, ceiling)
	defer cancel()
	for {
		ok, err := quorate(pollCtx, dial, endpoint, want)
		if err == nil && ok {
			return nil
		}
		logger.Debug("quorum not reached yet", "want", want, "err", err)
		select {
		case <-pollCtx.Done():
			return fmt.Errorf("cluster not quorate with %d nodes online within %s", want, ceiling)
		case <-time.After(interval):
		}
	}
}

func quorate(ctx context.Context, dial clusterDialer, endpoint string, want int) (bool, error) {
	svc, err := dial(ctx, endpoint)
	if err != nil {
		return false, err
	}
	entries, err := svc.GetStatus(ctx)
	if err != nil {
		return false, err
	}
	var clusterOK bool
	var online int
	for i := range entries {
		switch entries[i].Type {
		case "cluster":
			// >= not ==: the per-join gates ask for the members joined SO
			// FAR, and status may already know more configured nodes.
			clusterOK = entries[i].Quorate.Bool() && entries[i].Nodes >= want
		case "node":
			if entries[i].Online.Bool() {
				online++
			}
		}
	}
	return clusterOK && online >= want, nil
}
