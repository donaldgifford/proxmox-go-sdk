package lab

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/qemu"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/types"
)

// templateShutdownTimeout is the graceful ACPI window the freshly installed
// node gets before PVE hard-stops it (WithForceStop) ahead of conversion.
const templateShutdownTimeout = 2 * time.Minute

// TemplateName is the outer-host VM name of this version's nested-PVE
// template: pvelab-tmpl-<version> with dots dashed (the name doubles as the
// template guest's hostname label during the build install, and dots are
// invalid there). Computed, never configured, so it cannot drift from the
// version it represents.
func TemplateName(cfg *Config) string { return ownedNamePrefix + templateNodeName(cfg) }

// templateNodeName is the synthetic node name — and so the guest hostname
// label — the template installs under.
func templateNodeName(cfg *Config) string {
	return "tmpl-" + strings.ReplaceAll(cfg.Nested.PVEVersion, ".", "-")
}

// TemplateConfig derives the synthetic single-node lab config the template
// build reuses the provision path with: the template's VMID/CIDR as the one
// node, everything else (sizing, answer server, ISO) inherited. It errors
// when nested.template is absent.
func TemplateConfig(cfg *Config) (*Config, error) {
	t := cfg.Nested.Template
	if t == nil {
		return nil, errors.New("nested.template is not configured — add it (see pvelab.example.yaml) to use `pvelab template build`")
	}
	tcfg := *cfg
	tcfg.Nested.Nodes = []Node{{Name: templateNodeName(cfg), VMID: t.VMID, CIDR: t.CIDR}}
	return &tcfg, nil
}

// FindTemplate looks this version's template up on the outer node by its
// computed name. found reports whether any VM carries the name; a found VM
// with Template false is a leftover half-built VM squatting on it.
func FindTemplate(ctx context.Context, c *proxmox.Client, cfg *Config) (vm qemu.VM, found bool, err error) {
	vms, err := c.QEMU(cfg.Outer.Node).List(ctx)
	if err != nil {
		return qemu.VM{}, false, fmt.Errorf("list VMs on %s: %w", cfg.Outer.Node, err)
	}
	name := TemplateName(cfg)
	for i := range vms {
		if vms[i].Name == name {
			return vms[i], true, nil
		}
	}
	return qemu.VM{}, false, nil
}

// BuildTemplate runs the unattended install once for cfg's PVE version and
// converts the result into the outer-host template linked clones boot from:
// install (the answer server must already be running for the synthetic
// TemplateConfig) → graceful shutdown → detach the installer ISO → convert.
// force deletes an existing template of this version first — conversion is
// one-way, so there is no in-place refresh.
func BuildTemplate(ctx context.Context, c *proxmox.Client, cfg *Config, rootPassword string, force bool, logger *slog.Logger) error {
	return buildTemplate(ctx, c, cfg, force, versionProbe(rootPassword), readyPollInterval, readyPollCeiling, logger)
}

// buildTemplate is BuildTemplate with the readiness probe and poll cadence
// injected, so tests drive it against mockpve without a real install.
func buildTemplate(ctx context.Context, c *proxmox.Client, cfg *Config, force bool,
	probe readyProbe, interval, ceiling time.Duration, logger *slog.Logger,
) error {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	tcfg, err := TemplateConfig(cfg)
	if err != nil {
		return err
	}
	tnode := tcfg.Nested.Nodes[0]

	if err := ensureTemplateSlotFree(ctx, c, cfg, tnode, force, logger); err != nil {
		return err
	}
	if err := EnsureISOPrepared(ctx, c, cfg); err != nil {
		return err
	}
	isoVolid := PreparedISOVolid(cfg.Outer.ISOStorage, cfg.Nested.PVEVersion)

	if err := CreateNodeVMs(ctx, c, tcfg, isoVolid, logger); err != nil {
		return err
	}
	if err := StartNodeVMs(ctx, c, tcfg, logger); err != nil {
		return err
	}
	if _, err := waitReady(ctx, tcfg, probe, interval, ceiling, logger); err != nil {
		return err
	}

	if err := finalizeTemplate(ctx, c, cfg, tnode, logger); err != nil {
		return err
	}
	logger.Info("template ready", "name", TemplateName(cfg), "vmid", tnode.VMID)
	return nil
}

// finalizeTemplate turns the freshly installed VM into the template: graceful
// shutdown, installer-ISO detach, conversion — awaiting each step's task
// (the detach and conversion may answer synchronously with a zero Ref).
func finalizeTemplate(ctx context.Context, c *proxmox.Client, cfg *Config, tnode Node, logger *slog.Logger) error {
	svc := c.QEMU(cfg.Outer.Node)
	logger.Info("install complete — shutting the template VM down", "vmid", tnode.VMID)
	ref, err := svc.Shutdown(ctx, tnode.VMID,
		qemu.WithShutdownTimeout(templateShutdownTimeout), qemu.WithForceStop())
	if err != nil {
		return fmt.Errorf("shutdown template vm %d: %w", tnode.VMID, err)
	}
	if _, err := c.Tasks().Wait(ctx, ref); err != nil {
		return fmt.Errorf("wait shutdown template vm %d: %w", tnode.VMID, err)
	}

	// Detach the installer ISO before converting so clones never reference it
	// (and `pvelab down -purge-isos` can drop it without breaking the chain).
	cref, err := svc.SetConfig(ctx, tnode.VMID, &qemu.ConfigUpdate{Delete: "ide2"})
	if err != nil {
		return fmt.Errorf("detach installer ISO from vm %d: %w", tnode.VMID, err)
	}
	if !cref.IsZero() {
		if _, err := c.Tasks().Wait(ctx, cref); err != nil {
			return fmt.Errorf("wait detach installer ISO from vm %d: %w", tnode.VMID, err)
		}
	}

	tref, err := svc.ConvertToTemplate(ctx, tnode.VMID)
	if err != nil {
		return fmt.Errorf("convert vm %d to template: %w", tnode.VMID, err)
	}
	if !tref.IsZero() {
		if _, err := c.Tasks().Wait(ctx, tref); err != nil {
			return fmt.Errorf("wait convert vm %d to template: %w", tnode.VMID, err)
		}
	}
	return nil
}

// ensureTemplateSlotFree enforces the build's collision rules from one VM
// listing: a VM already carrying the computed template name needs force (then
// it is deleted — the owned prefix is part of that name, so it is ours by
// construction); any OTHER VM on the template VMID is foreign and always an
// error (pvelab never adopts or deletes a guest it does not own).
func ensureTemplateSlotFree(ctx context.Context, c *proxmox.Client, cfg *Config, tnode Node, force bool, logger *slog.Logger) error {
	vms, err := c.QEMU(cfg.Outer.Node).List(ctx)
	if err != nil {
		return fmt.Errorf("list VMs on %s: %w", cfg.Outer.Node, err)
	}
	name := TemplateName(cfg)
	for i := range vms {
		switch {
		case vms[i].Name == name:
			if !force {
				return fmt.Errorf(
					"template %s already exists (vmid %d) — pass -force to rebuild (conversion is one-way, there is no in-place refresh)",
					name,
					vms[i].VMID,
				)
			}
			if err := deleteTemplateVM(ctx, c, cfg, &vms[i], logger); err != nil {
				return err
			}
		case int(vms[i].VMID) == tnode.VMID:
			return fmt.Errorf("vmid %d already exists on %s as %q — not this version's template, refusing to touch it",
				tnode.VMID, cfg.Outer.Node, vms[i].Name)
		}
	}
	return nil
}

// deleteTemplateVM force-removes an existing template (or the half-built VM
// squatting on its name): stop first when running — a converted template
// cannot run, but an interrupted build's VM can — then delete, awaiting both.
func deleteTemplateVM(ctx context.Context, c *proxmox.Client, cfg *Config, vm *qemu.VM, logger *slog.Logger) error {
	svc := c.QEMU(cfg.Outer.Node)
	vmid := int(vm.VMID)
	logger.Info("force: deleting existing template", "name", vm.Name, "vmid", vmid)
	if vm.Status == types.PowerStateRunning {
		ref, err := svc.Stop(ctx, vmid)
		if err != nil {
			return fmt.Errorf("stop leftover template vm %d: %w", vmid, err)
		}
		if _, err := c.Tasks().Wait(ctx, ref); err != nil {
			return fmt.Errorf("wait stop leftover template vm %d: %w", vmid, err)
		}
	}
	ref, err := svc.Delete(ctx, vmid)
	if err != nil {
		return fmt.Errorf("delete template vm %d: %w", vmid, err)
	}
	if _, err := c.Tasks().Wait(ctx, ref); err != nil {
		return fmt.Errorf("wait delete template vm %d: %w", vmid, err)
	}
	return nil
}
