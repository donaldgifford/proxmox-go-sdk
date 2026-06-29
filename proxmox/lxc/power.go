package lxc

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/internal/svcutil"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/tasks"
)

// powerConfig is the opaque target the power WithX options write to, keeping the
// url.Values wire form out of the public signatures.
type powerConfig struct{ vals url.Values }

func newPowerConfig() powerConfig { return powerConfig{vals: url.Values{}} }

// StopOption configures Stop.
type StopOption func(*powerConfig)

// WithStopTimeout caps how long, in whole seconds, PVE waits for a clean stop
// before killing the container.
func WithStopTimeout(d time.Duration) StopOption {
	return func(c *powerConfig) { c.vals.Set("timeout", strconv.Itoa(int(d.Seconds()))) }
}

// ShutdownOption configures Shutdown.
type ShutdownOption func(*powerConfig)

// WithShutdownTimeout caps how long, in whole seconds, PVE waits for the
// container to shut down cleanly.
func WithShutdownTimeout(d time.Duration) ShutdownOption {
	return func(c *powerConfig) { c.vals.Set("timeout", strconv.Itoa(int(d.Seconds()))) }
}

// WithForceStop makes PVE force-stop the container if the clean shutdown does
// not complete within the timeout.
func WithForceStop() ShutdownOption {
	return func(c *powerConfig) { c.vals.Set("forceStop", "1") }
}

// Start powers on a container.
func (s *Service) Start(ctx context.Context, vmid int) (tasks.Ref, error) {
	return s.statusAction(ctx, "lxc.Start", "start", vmid, nil)
}

// Stop hard-stops a container. Use Shutdown for a clean shutdown.
func (s *Service) Stop(ctx context.Context, vmid int, opts ...StopOption) (tasks.Ref, error) {
	cfg := newPowerConfig()
	for _, opt := range opts {
		opt(&cfg)
	}
	return s.statusAction(ctx, "lxc.Stop", "stop", vmid, cfg.vals)
}

// Shutdown requests a clean shutdown of the container.
func (s *Service) Shutdown(ctx context.Context, vmid int, opts ...ShutdownOption) (tasks.Ref, error) {
	cfg := newPowerConfig()
	for _, opt := range opts {
		opt(&cfg)
	}
	return s.statusAction(ctx, "lxc.Shutdown", "shutdown", vmid, cfg.vals)
}

// Reboot reboots the container.
func (s *Service) Reboot(ctx context.Context, vmid int) (tasks.Ref, error) {
	return s.statusAction(ctx, "lxc.Reboot", "reboot", vmid, nil)
}

// Suspend freezes a running container.
func (s *Service) Suspend(ctx context.Context, vmid int) (tasks.Ref, error) {
	return s.statusAction(ctx, "lxc.Suspend", "suspend", vmid, nil)
}

// Resume unfreezes a suspended container.
func (s *Service) Resume(ctx context.Context, vmid int) (tasks.Ref, error) {
	return s.statusAction(ctx, "lxc.Resume", "resume", vmid, nil)
}

// statusAction POSTs to /status/{verb} and returns the resulting task. A nil or
// empty vals sends no body, so PVE applies its defaults.
func (s *Service) statusAction(ctx context.Context, label, verb string, vmid int, vals url.Values) (tasks.Ref, error) {
	var body any
	if len(vals) > 0 {
		body = vals
	}
	var upid string
	if err := s.c.DoRequest(ctx, http.MethodPost, s.ctPath(vmid)+"/status/"+verb, body, &upid); err != nil {
		return tasks.Ref{}, fmt.Errorf("%s: %w", label, err)
	}
	return svcutil.TaskRef(label, upid)
}
