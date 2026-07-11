// Command pvelab-spike is the THROWAWAY Phase 0 driver for IMPL-0002's
// dogfood harness: it creates one nested PVE node VM on the outer host from a
// prepared auto-install ISO, times the unattended install until the nested
// API answers with password credentials, and tears the VM down again.
//
// SUPERSEDED by cmd/pvelab from IMPL-0002 Phase 1 — kept only as the record
// of the spike (IMPL-0002 IQ-5 = b). Do not grow this; fix the CLI instead.
//
// Configuration is env-driven (no YAML yet — that is Phase 1):
//
//	PVE_ENDPOINT / PVE_TOKEN_ID / PVE_TOKEN_SECRET / PVE_INSECURE_TLS
//	    outer-host API client (same vars as the integration suite)
//	SPIKE_NODE             outer node name (default "r740a")
//	SPIKE_VMID             nested VM's VMID (default 9201)
//	SPIKE_ISO              prepared-ISO volid, e.g. "local:iso/pve-auto-pve1.iso" (up)
//	SPIKE_STORAGE          storage for the nested VM's disk (default "local-zfs")
//	SPIKE_BRIDGE           bridge for net0 (default "vmbr0")
//	SPIKE_NESTED_ENDPOINT  nested API URL, e.g. "https://192.0.2.11:8006" (up)
//	PVELAB_ROOT_PW         nested root@pam password baked into the answer file (up)
//
// Usage: go run ./hack/pvelab-spike <up|down|status>
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/api"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/qemu"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/types"
)

const (
	cores    = 4
	memoryMB = 8192
	diskGB   = 32

	readyPollEvery = 15 * time.Second
	teardownPerOp  = 3 * time.Minute

	// The reserved pvelab VMID block (DESIGN-0002 OQ-10: nodes 9201–9203,
	// test scratch 93xx). The driver refuses to touch any VMID outside it so
	// a fat-fingered SPIKE_VMID can never reach a real guest on the outer
	// host.
	vmidRangeLo = 9200
	vmidRangeHi = 9399

	// Every VM this driver creates is named with this prefix, and down()
	// refuses to delete a VM whose name lacks it — we only delete what we
	// created.
	ownedNamePrefix = "pvelab-spike-"
)

func main() {
	wait := flag.Duration("wait", 30*time.Minute, "ceiling for the nested-API readiness poll")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(logger)

	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: pvelab-spike [-wait 30m] <up|down|status>")
		os.Exit(2)
	}
	if err := run(flag.Arg(0), *wait); err != nil {
		slog.Error("spike failed", "err", err)
		os.Exit(1)
	}
}

type spikeConfig struct {
	node           string
	vmid           int
	iso            string
	storage        string
	bridge         string
	nestedEndpoint string
	rootPW         string
}

func loadConfig() spikeConfig {
	cfg := spikeConfig{
		node:           envOr("SPIKE_NODE", "r740a"),
		iso:            os.Getenv("SPIKE_ISO"),
		storage:        envOr("SPIKE_STORAGE", "local-zfs"),
		bridge:         envOr("SPIKE_BRIDGE", "vmbr0"),
		nestedEndpoint: os.Getenv("SPIKE_NESTED_ENDPOINT"),
		rootPW:         os.Getenv("PVELAB_ROOT_PW"),
		vmid:           9201,
	}
	if v := os.Getenv("SPIKE_VMID"); v != "" {
		id, err := strconv.Atoi(v)
		if err != nil {
			slog.Error("SPIKE_VMID is not a number")
			os.Exit(2)
		}
		cfg.vmid = id
	}
	if cfg.vmid < vmidRangeLo || cfg.vmid > vmidRangeHi {
		slog.Error("refusing to operate outside the reserved pvelab VMID range",
			"vmid", cfg.vmid, "range_lo", vmidRangeLo, "range_hi", vmidRangeHi)
		os.Exit(2)
	}
	return cfg
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func outerClient(ctx context.Context) (*proxmox.Client, error) {
	endpoint := os.Getenv("PVE_ENDPOINT")
	tokenID := os.Getenv("PVE_TOKEN_ID")
	secret := os.Getenv("PVE_TOKEN_SECRET")
	if endpoint == "" || tokenID == "" || secret == "" {
		return nil, errors.New("PVE_ENDPOINT, PVE_TOKEN_ID and PVE_TOKEN_SECRET must be set")
	}
	var opts []proxmox.Option
	if insecure, err := strconv.ParseBool(os.Getenv("PVE_INSECURE_TLS")); err == nil && insecure {
		opts = append(opts, proxmox.WithInsecureSkipVerify(true))
	}
	return proxmox.NewClient(ctx, endpoint, api.TokenCredentials(tokenID, secret), opts...)
}

func run(cmd string, wait time.Duration) error {
	ctx := context.Background()
	cfg := loadConfig()

	c, err := outerClient(ctx)
	if err != nil {
		return fmt.Errorf("outer client: %w", err)
	}

	switch cmd {
	case "up":
		return up(ctx, c, &cfg, wait)
	case "down":
		return down(ctx, c, &cfg)
	case "status":
		return status(ctx, c, &cfg)
	default:
		return fmt.Errorf("unknown subcommand %q (want up, down or status)", cmd)
	}
}

// up creates + starts the nested node VM from the prepared ISO, then polls
// the nested API until root@pam password login answers /version — the Phase 0
// install wall-clock measurement.
func up(ctx context.Context, c *proxmox.Client, cfg *spikeConfig, wait time.Duration) error {
	if cfg.iso == "" || cfg.nestedEndpoint == "" || cfg.rootPW == "" {
		return errors.New("up needs SPIKE_ISO, SPIKE_NESTED_ENDPOINT and PVELAB_ROOT_PW")
	}

	svc := c.QEMU(cfg.node)
	spec := &qemu.CreateSpec{
		VMID:   types.VMID(cfg.vmid),
		Name:   fmt.Sprintf("%s%d", ownedNamePrefix, cfg.vmid),
		Cores:  cores,
		Memory: memoryMB,
		CPU:    "host", // pass through VMX for nested virt.
		OSType: "l26",
		SCSI0:  fmt.Sprintf("%s:%d", cfg.storage, diskGB),
		Net0:   "virtio,bridge=" + cfg.bridge,
		Boot:   "order=scsi0;ide2",
		Extra: map[string]string{
			"scsihw": "virtio-scsi-pci",
			"ide2":   cfg.iso + ",media=cdrom",
		},
	}
	slog.Info("creating nested node VM", "node", cfg.node, "vmid", cfg.vmid, "iso", cfg.iso)
	ref, err := svc.Create(ctx, spec)
	if err != nil {
		return fmt.Errorf("create vm %d: %w", cfg.vmid, err)
	}
	if _, err := c.Tasks().Wait(ctx, ref); err != nil {
		return fmt.Errorf("wait create task: %w", err)
	}

	slog.Info("starting VM — unattended install begins", "vmid", cfg.vmid)
	startedAt := time.Now()
	ref, err = svc.Start(ctx, cfg.vmid)
	if err != nil {
		return fmt.Errorf("start vm %d: %w", cfg.vmid, err)
	}
	if _, err := c.Tasks().Wait(ctx, ref); err != nil {
		return fmt.Errorf("wait start task: %w", err)
	}

	slog.Info("polling nested API with password credentials",
		"endpoint", cfg.nestedEndpoint, "every", readyPollEvery.String(), "ceiling", wait.String())
	deadline := time.Now().Add(wait)
	for {
		nested, err := proxmox.NewClient(ctx, cfg.nestedEndpoint,
			api.UserCredentials("root@pam", cfg.rootPW, ""),
			proxmox.WithInsecureSkipVerify(true))
		if err == nil {
			elapsed := time.Since(startedAt).Round(time.Second)
			_ = nested // NewClient already round-tripped /version with the ticket.
			slog.Info("NESTED NODE READY — /version answered with root@pam password creds",
				"elapsed_since_start", elapsed.String())
			fmt.Printf("install wall-clock (VM start -> /version): %s\n", elapsed)
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("nested API not ready after %s (last error: %w)", wait, err)
		}
		slog.Info("not ready yet — installer still running",
			"elapsed", time.Since(startedAt).Round(time.Second).String())
		time.Sleep(readyPollEvery)
	}
}

// down stops and deletes the nested node VM with a bounded context per op, so
// a wedged teardown fails fast instead of hanging. It refuses to delete a VM
// this driver did not create (ownership check on the name prefix).
func down(ctx context.Context, c *proxmox.Client, cfg *spikeConfig) error {
	svc := c.QEMU(cfg.node)

	vmCfg, err := svc.Config(ctx, cfg.vmid)
	if err != nil {
		return fmt.Errorf("read vm %d config before delete: %w", cfg.vmid, err)
	}
	if !strings.HasPrefix(vmCfg.Name, ownedNamePrefix) {
		return fmt.Errorf("vm %d is named %q, not %q-prefixed — not ours, refusing to delete",
			cfg.vmid, vmCfg.Name, ownedNamePrefix)
	}

	stopCtx, cancel := context.WithTimeout(ctx, teardownPerOp)
	defer cancel()
	if ref, err := svc.Stop(stopCtx, cfg.vmid); err != nil {
		slog.Warn("stop failed (may already be stopped) — continuing to delete", "vmid", cfg.vmid, "err", err)
	} else if _, err := c.Tasks().Wait(stopCtx, ref); err != nil {
		slog.Warn("stop task did not finish cleanly — continuing to delete", "vmid", cfg.vmid, "err", err)
	}

	delCtx, cancel := context.WithTimeout(ctx, teardownPerOp)
	defer cancel()
	ref, err := svc.Delete(delCtx, cfg.vmid)
	if err != nil {
		return fmt.Errorf("delete vm %d: %w", cfg.vmid, err)
	}
	if _, err := c.Tasks().Wait(delCtx, ref); err != nil {
		return fmt.Errorf("wait delete task: %w", err)
	}
	slog.Info("nested node VM deleted", "vmid", cfg.vmid)
	return nil
}

// status prints the outer view of the VM.
func status(ctx context.Context, c *proxmox.Client, cfg *spikeConfig) error {
	st, err := c.QEMU(cfg.node).Get(ctx, cfg.vmid)
	if err != nil {
		return fmt.Errorf("get vm %d: %w", cfg.vmid, err)
	}
	fmt.Printf("vmid=%d status=%s\n", cfg.vmid, st.Status)
	return nil
}
