package qemu

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/tasks"
)

// powerConfig accumulates the optional parameters of a power action. It is the
// opaque target the WithX options write to, so the form encoding stays out of
// the public signatures.
type powerConfig struct{ vals url.Values }

func newPowerConfig() powerConfig { return powerConfig{vals: url.Values{}} }

// StopOption configures Stop.
type StopOption func(*powerConfig)

// WithStopTimeout caps how long, in whole seconds, PVE waits for a clean stop
// before killing the VM.
func WithStopTimeout(d time.Duration) StopOption {
	return func(c *powerConfig) { c.vals.Set("timeout", strconv.Itoa(int(d.Seconds()))) }
}

// ShutdownOption configures Shutdown.
type ShutdownOption func(*powerConfig)

// WithShutdownTimeout caps how long, in whole seconds, PVE waits for the guest
// to shut down cleanly.
func WithShutdownTimeout(d time.Duration) ShutdownOption {
	return func(c *powerConfig) { c.vals.Set("timeout", strconv.Itoa(int(d.Seconds()))) }
}

// WithForceStop makes PVE hard-stop the VM if the ACPI shutdown does not
// complete within the timeout.
func WithForceStop() ShutdownOption {
	return func(c *powerConfig) { c.vals.Set("forceStop", "1") }
}

// SuspendOption configures Suspend.
type SuspendOption func(*powerConfig)

// WithSuspendToDisk suspends to disk (hibernate) instead of to RAM, saving the
// VM state to storage. An empty storage lets PVE choose.
func WithSuspendToDisk(storage string) SuspendOption {
	return func(c *powerConfig) {
		c.vals.Set("todisk", "1")
		if storage != "" {
			c.vals.Set("statestorage", storage)
		}
	}
}

// Start powers on a VM.
func (s *Service) Start(ctx context.Context, vmid int) (tasks.Ref, error) {
	return s.statusAction(ctx, "qemu.Start", "start", vmid, nil)
}

// Stop hard-stops a VM (pulls the virtual power, like yanking the cord). Use
// Shutdown for a graceful ACPI shutdown.
func (s *Service) Stop(ctx context.Context, vmid int, opts ...StopOption) (tasks.Ref, error) {
	cfg := newPowerConfig()
	for _, opt := range opts {
		opt(&cfg)
	}
	return s.statusAction(ctx, "qemu.Stop", "stop", vmid, cfg.vals)
}

// Shutdown requests a graceful ACPI shutdown of the guest OS.
func (s *Service) Shutdown(ctx context.Context, vmid int, opts ...ShutdownOption) (tasks.Ref, error) {
	cfg := newPowerConfig()
	for _, opt := range opts {
		opt(&cfg)
	}
	return s.statusAction(ctx, "qemu.Shutdown", "shutdown", vmid, cfg.vals)
}

// Reboot requests a graceful reboot of the guest OS.
func (s *Service) Reboot(ctx context.Context, vmid int) (tasks.Ref, error) {
	return s.statusAction(ctx, "qemu.Reboot", "reboot", vmid, nil)
}

// Suspend pauses a VM. By default the state is held in RAM; use
// WithSuspendToDisk to hibernate.
func (s *Service) Suspend(ctx context.Context, vmid int, opts ...SuspendOption) (tasks.Ref, error) {
	cfg := newPowerConfig()
	for _, opt := range opts {
		opt(&cfg)
	}
	return s.statusAction(ctx, "qemu.Suspend", "suspend", vmid, cfg.vals)
}

// Resume returns a suspended VM to running.
func (s *Service) Resume(ctx context.Context, vmid int) (tasks.Ref, error) {
	return s.statusAction(ctx, "qemu.Resume", "resume", vmid, nil)
}

// statusAction POSTs to /status/{verb} and returns the resulting task. A nil or
// empty vals sends no body, so PVE applies its defaults.
func (s *Service) statusAction(ctx context.Context, label, verb string, vmid int, vals url.Values) (tasks.Ref, error) {
	var body any
	if len(vals) > 0 {
		body = vals
	}
	var upid string
	if err := s.c.DoRequest(ctx, http.MethodPost, s.vmPath(vmid)+"/status/"+verb, body, &upid); err != nil {
		return tasks.Ref{}, fmt.Errorf("%s: %w", label, err)
	}
	return toRef(label, upid)
}
