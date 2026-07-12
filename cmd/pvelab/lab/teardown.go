package lab

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/pverr"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/qemu"
)

// teardownPerOp bounds every teardown operation (the integration suite's
// cleanupCtx pattern): a wedged stop or delete fails fast instead of hanging.
const teardownPerOp = 3 * time.Minute

// ErrNotOurs marks a refusal by the blast-radius guards: the target VM is
// outside the reserved VMID block or is not named with the harness prefix.
// It is never tolerated by TeardownOptions.Force — Force forgives "already
// gone", never "not ours".
var ErrNotOurs = errors.New("refusing to touch a VM the harness does not own")

// TeardownOptions control `pvelab down`.
type TeardownOptions struct {
	// Force tolerates missing or half-created VMs (a failed `up` leaves
	// gaps); ownership refusals are still fatal.
	Force bool
	// PurgeISOs also deletes the prepared installer ISO for cfg's version.
	PurgeISOs bool
}

// Teardown stops and deletes every configured node VM, in parallel, each op
// on a bounded context. The config is the source of truth (VMIDs are
// declared, not discovered), so this also serves `down -no-state` — note it
// deletes what the config says, not what a previous `up` actually created.
// Independent nodes' errors are joined, so one stuck node doesn't hide
// another's failure.
func Teardown(ctx context.Context, c *proxmox.Client, cfg *Config, opts TeardownOptions, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	fns := make([]func() error, 0, len(cfg.Nested.Nodes))
	for _, n := range cfg.Nested.Nodes {
		fns = append(fns, func() error {
			return teardownNode(ctx, c, cfg, n, opts, logger)
		})
	}
	err := parallel(fns...)

	if opts.PurgeISOs {
		if perr := purgeISO(ctx, c, cfg, opts, logger); perr != nil {
			err = errors.Join(err, perr)
		}
	}
	return err
}

// teardownNode stops (best-effort) then deletes one node VM, ownership-
// checked first.
func teardownNode(ctx context.Context, c *proxmox.Client, cfg *Config, n Node, opts TeardownOptions, logger *slog.Logger) error {
	svc := c.QEMU(cfg.Outer.Node)

	if err := checkOwnership(ctx, svc, n.VMID); err != nil {
		if opts.Force && pverr.IsNotFound(err) {
			logger.Warn("vm already gone — skipping (force)", "node", n.Name, "vmid", n.VMID)
			return nil
		}
		return err
	}

	// Stop is best-effort: the VM may already be stopped or half-created —
	// delete is the operation that matters.
	stopCtx, cancelStop := context.WithTimeout(ctx, teardownPerOp)
	defer cancelStop()
	if ref, err := svc.Stop(stopCtx, n.VMID); err != nil {
		logger.Warn("stop failed (may already be stopped) — continuing to delete",
			"vmid", n.VMID, "err", err)
	} else if _, err := c.Tasks().Wait(stopCtx, ref); err != nil {
		logger.Warn("stop task did not finish cleanly — continuing to delete",
			"vmid", n.VMID, "err", err)
	}

	delCtx, cancelDel := context.WithTimeout(ctx, teardownPerOp)
	defer cancelDel()
	ref, err := svc.Delete(delCtx, n.VMID)
	if err != nil {
		if opts.Force && pverr.IsNotFound(err) {
			logger.Warn("vm vanished before delete — skipping (force)", "node", n.Name, "vmid", n.VMID)
			return nil
		}
		return fmt.Errorf("delete vm %d (%s): %w", n.VMID, n.Name, err)
	}
	if _, err := c.Tasks().Wait(delCtx, ref); err != nil {
		return fmt.Errorf("wait delete vm %d (%s): %w", n.VMID, n.Name, err)
	}
	logger.Info("node VM deleted", "node", n.Name, "vmid", n.VMID)
	return nil
}

// checkOwnership enforces both blast-radius guards before any destructive
// call: the VMID must be inside the reserved pvelab block (defense in depth —
// Config.Validate already refuses these at load) AND the VM's live name must
// carry the harness prefix. Neither refusal is skippable by Force.
func checkOwnership(ctx context.Context, svc *qemu.Service, vmid int) error {
	if vmid < vmidRangeLo || vmid > vmidRangeHi {
		return fmt.Errorf("vmid %d is outside the reserved pvelab block %d-%d: %w",
			vmid, vmidRangeLo, vmidRangeHi, ErrNotOurs)
	}
	vmCfg, err := svc.Config(ctx, vmid)
	if err != nil {
		return fmt.Errorf("read vm %d config before delete: %w", vmid, err)
	}
	if !strings.HasPrefix(vmCfg.Name, ownedNamePrefix) {
		return fmt.Errorf("vm %d is named %q, not %q-prefixed: %w",
			vmid, vmCfg.Name, ownedNamePrefix, ErrNotOurs)
	}
	return nil
}

// purgeISO deletes the prepared installer ISO for cfg's version. The volid is
// harness-owned by construction (the pvelab- filename prefix).
func purgeISO(ctx context.Context, c *proxmox.Client, cfg *Config, opts TeardownOptions, logger *slog.Logger) error {
	volid := PreparedISOVolid(cfg.Outer.ISOStorage, cfg.Nested.PVEVersion)
	delCtx, cancel := context.WithTimeout(ctx, teardownPerOp)
	defer cancel()
	ref, err := c.Storage().DeleteVolume(delCtx, cfg.Outer.Node, cfg.Outer.ISOStorage, volid)
	if err != nil {
		if opts.Force && pverr.IsNotFound(err) {
			logger.Warn("prepared ISO already gone — skipping (force)", "volid", volid)
			return nil
		}
		return fmt.Errorf("purge prepared ISO %s: %w", volid, err)
	}
	if !ref.IsZero() {
		if _, err := c.Tasks().Wait(delCtx, ref); err != nil {
			return fmt.Errorf("wait purge of %s: %w", volid, err)
		}
	}
	logger.Info("prepared ISO purged", "volid", volid)
	return nil
}
