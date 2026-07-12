package lab

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/api"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/qemu"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/types"
)

// Readiness poll cadence and ceiling, from Phase 0 measurements: install
// wall-clock 4m04s on r740a, effective poll period ~22 s (15 s sleep + ~7 s
// connection-refused) — 15 minutes is nearly 4× the measured install.
const (
	readyPollInterval = 15 * time.Second
	readyPollCeiling  = 15 * time.Minute
	probeTimeout      = 10 * time.Second // per /version attempt, so a hung dial can't stall the cadence.
)

// ownedNamePrefix marks every VM this harness creates ("pvelab-<node name>").
// It is the second blast-radius guard: teardown refuses to delete any VM
// whose live name lacks this prefix, even inside the reserved VMID block.
const ownedNamePrefix = "pvelab-"

// vmName is the outer-host VM name for a nested node.
func vmName(n Node) string { return ownedNamePrefix + n.Name }

// smbiosSerial renders the smbios1 config value that stamps the node's
// identity into the guest's DMI serial. The answer server matches installer
// requests by this serial (raw or base64 — QEMU decodes the base64=1 form, so
// the guest-visible serial is the raw node name). Base64-encoding is
// defensive: it keeps any node name valid inside PVE's property string.
func smbiosSerial(n Node) string {
	return "serial=" + base64.StdEncoding.EncodeToString([]byte(n.Name)) + ",base64=1"
}

// EnsureISOPrepared verifies the prepared http-mode ISO for cfg's PVE version
// is on the outer host, pointing the operator at `pvelab iso` if not.
func EnsureISOPrepared(ctx context.Context, c *proxmox.Client, cfg *Config) error {
	volid := PreparedISOVolid(cfg.Outer.ISOStorage, cfg.Nested.PVEVersion)
	present, err := isoPresent(ctx, c, cfg, volid)
	if err != nil {
		return err
	}
	if !present {
		return fmt.Errorf("prepared ISO %s not found on storage %s — run `pvelab iso` first", volid, cfg.Outer.ISOStorage)
	}
	return nil
}

// EnsureVMIDsFree is the live half of the VMID guard Config.Validate cannot
// do: it errors if any configured node's VMID already exists on the outer
// node. `pvelab up` never silently adopts leftovers (design OQ-7) — a stale
// lab is torn down with `pvelab down`, not reused.
func EnsureVMIDsFree(ctx context.Context, c *proxmox.Client, cfg *Config) error {
	vms, err := c.QEMU(cfg.Outer.Node).List(ctx)
	if err != nil {
		return fmt.Errorf("list VMs on %s: %w", cfg.Outer.Node, err)
	}
	existing := make(map[int]string, len(vms))
	for _, vm := range vms {
		existing[int(vm.VMID)] = vm.Name
	}
	var errs []error
	for _, n := range cfg.Nested.Nodes {
		if name, ok := existing[n.VMID]; ok {
			errs = append(errs, fmt.Errorf("vmid %d (node %s) already exists on %s as %q — run `pvelab down` first, up never adopts leftovers",
				n.VMID, n.Name, cfg.Outer.Node, name))
		}
	}
	return errors.Join(errs...)
}

// CreateNodeVMs creates (but does not start) every configured node's VM on
// the outer host, in parallel, awaiting each create task. The spec is the
// Phase 0 spike's: CPU host (VMX passthrough for nested virt), scsi0 disk on
// outer.storage, virtio NIC on outer.bridge, the prepared ISO on ide2, boot
// order scsi0;ide2 — plus the smbios1 serial the answer server matches on.
func CreateNodeVMs(ctx context.Context, c *proxmox.Client, cfg *Config, isoVolid string, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	svc := c.QEMU(cfg.Outer.Node)
	fns := make([]func() error, 0, len(cfg.Nested.Nodes))
	for _, n := range cfg.Nested.Nodes {
		fns = append(fns, func() error {
			spec := &qemu.CreateSpec{
				VMID:   types.VMID(n.VMID),
				Name:   vmName(n),
				Cores:  cfg.Nested.Cores,
				Memory: cfg.Nested.MemoryMB,
				CPU:    "host",
				OSType: "l26",
				SCSI0:  fmt.Sprintf("%s:%d", cfg.Outer.Storage, cfg.Nested.DiskGB),
				Net0:   "virtio,bridge=" + cfg.Outer.Bridge,
				Boot:   "order=scsi0;ide2",
				Extra: map[string]string{
					"scsihw":  "virtio-scsi-pci",
					"ide2":    isoVolid + ",media=cdrom",
					"smbios1": smbiosSerial(n),
				},
			}
			logger.Info("creating node VM", "node", n.Name, "vmid", n.VMID)
			ref, err := svc.Create(ctx, spec)
			if err != nil {
				return fmt.Errorf("create vm %d (%s): %w", n.VMID, n.Name, err)
			}
			if _, err := c.Tasks().Wait(ctx, ref); err != nil {
				return fmt.Errorf("wait create vm %d (%s): %w", n.VMID, n.Name, err)
			}
			return nil
		})
	}
	return parallel(fns...)
}

// CloneNodeVMs creates every configured node's VM as a clone of the version's
// template (Full unset — PVE's default against a template is a linked clone),
// in parallel, awaiting each clone task. The clones are left STOPPED: a clone
// boots the template's baked-in IP, so starting is serialized with the
// re-identify pass (ReidentifyClones), never done in bulk.
func CloneNodeVMs(ctx context.Context, c *proxmox.Client, cfg *Config, templateVMID int, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	svc := c.QEMU(cfg.Outer.Node)
	fns := make([]func() error, 0, len(cfg.Nested.Nodes))
	for _, n := range cfg.Nested.Nodes {
		fns = append(fns, func() error {
			logger.Info("cloning node VM from template", "node", n.Name, "vmid", n.VMID, "template", templateVMID)
			ref, err := svc.Clone(ctx, templateVMID, &qemu.CloneSpec{
				NewID: types.VMID(n.VMID),
				Name:  vmName(n),
			})
			if err != nil {
				return fmt.Errorf("clone vm %d (%s) from template %d: %w", n.VMID, n.Name, templateVMID, err)
			}
			if _, err := c.Tasks().Wait(ctx, ref); err != nil {
				return fmt.Errorf("wait clone vm %d (%s): %w", n.VMID, n.Name, err)
			}
			return nil
		})
	}
	return parallel(fns...)
}

// StartNodeVMs starts every configured node's VM in parallel, awaiting each
// start task. Starting boots the prepared ISO — the unattended installs (and
// the answer-server requests) begin here.
func StartNodeVMs(ctx context.Context, c *proxmox.Client, cfg *Config, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	svc := c.QEMU(cfg.Outer.Node)
	fns := make([]func() error, 0, len(cfg.Nested.Nodes))
	for _, n := range cfg.Nested.Nodes {
		fns = append(fns, func() error {
			logger.Info("starting node VM — unattended install begins", "node", n.Name, "vmid", n.VMID)
			ref, err := svc.Start(ctx, n.VMID)
			if err != nil {
				return fmt.Errorf("start vm %d (%s): %w", n.VMID, n.Name, err)
			}
			if _, err := c.Tasks().Wait(ctx, ref); err != nil {
				return fmt.Errorf("wait start vm %d (%s): %w", n.VMID, n.Name, err)
			}
			return nil
		})
	}
	return parallel(fns...)
}

// NodeReadiness is one node's install-completion measurement.
type NodeReadiness struct {
	Node    string
	Ready   bool
	Elapsed time.Duration
}

// readyProbe checks one nested node's API; nil means ready. A seam so the
// poll loop is testable without three installing VMs.
type readyProbe func(ctx context.Context, endpoint string) error

// WaitReady polls every node's own https://<ip>:8006/version with root@pam
// password credentials until it answers, per node in parallel, each bounded
// by the Phase 0 ceiling. It returns one NodeReadiness per node in cfg order;
// nodes that never answered have Ready false and a joined error names them.
func WaitReady(ctx context.Context, cfg *Config, rootPassword string, logger *slog.Logger) ([]NodeReadiness, error) {
	return waitReady(ctx, cfg, versionProbe(rootPassword), readyPollInterval, readyPollCeiling, logger)
}

// versionProbe builds the real readiness check: proxmox.NewClient round-trips
// /version with a password ticket. Insecure TLS is unconditional — a fresh
// install serves a self-signed certificate.
func versionProbe(rootPassword string) readyProbe {
	return func(ctx context.Context, endpoint string) error {
		_, err := proxmox.NewClient(ctx, endpoint,
			api.UserCredentials("root@pam", rootPassword, ""),
			proxmox.WithInsecureSkipVerify(true),
			proxmox.WithRequestTimeout(probeTimeout))
		return err
	}
}

// nodeEndpoint is the nested node's API URL, derived from its CIDR.
func nodeEndpoint(n Node) (string, error) {
	ip, err := n.IP()
	if err != nil {
		return "", err
	}
	return "https://" + ip + ":8006", nil
}

func waitReady(ctx context.Context, cfg *Config, probe readyProbe, interval, ceiling time.Duration, logger *slog.Logger) ([]NodeReadiness, error) {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	results := make([]NodeReadiness, len(cfg.Nested.Nodes))
	fns := make([]func() error, 0, len(cfg.Nested.Nodes))
	for i, n := range cfg.Nested.Nodes {
		results[i] = NodeReadiness{Node: n.Name}
		fns = append(fns, func() error {
			endpoint, err := nodeEndpoint(n)
			if err != nil {
				return err
			}
			perCtx, cancel := context.WithTimeout(ctx, ceiling)
			defer cancel()
			start := time.Now()
			for {
				if err := probe(perCtx, endpoint); err == nil {
					results[i].Ready = true
					results[i].Elapsed = time.Since(start)
					logger.Info("node ready", "node", n.Name,
						"elapsed", results[i].Elapsed.Round(time.Second).String())
					return nil
				}
				logger.Debug("node not ready yet — installer still running", "node", n.Name,
					"elapsed", time.Since(start).Round(time.Second).String())
				select {
				case <-perCtx.Done():
					return fmt.Errorf("node %s (%s) not ready within %s", n.Name, endpoint, ceiling)
				case <-time.After(interval):
				}
			}
		})
	}
	err := parallel(fns...)
	return results, err
}

// parallel runs fns concurrently and joins their errors. Node installs are
// independent (unlike Phase 2 cluster joins, which serialize), so one stuck
// node neither blocks nor hides another's failure.
func parallel(fns ...func() error) error {
	errs := make([]error, len(fns))
	var wg sync.WaitGroup
	for i, fn := range fns {
		wg.Go(func() { errs[i] = fn() })
	}
	wg.Wait()
	return errors.Join(errs...)
}
