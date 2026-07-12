package qemu

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/internal/svcutil"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/tasks"
)

// convertConfig accumulates the optional parameters of a template conversion.
// It is the opaque target ConvertOption writes to, so the form encoding stays
// out of the public signatures.
type convertConfig struct{ vals url.Values }

// ConvertOption configures ConvertToTemplate.
type ConvertOption func(*convertConfig)

// WithTemplateDisk converts only the named disk (e.g. "scsi0") to a base
// image, leaving the other disks untouched. Without it PVE converts every
// disk.
func WithTemplateDisk(disk string) ConvertOption {
	return func(c *convertConfig) { c.vals.Set("disk", disk) }
}

// ConvertToTemplate marks a VM as a template (POST
// /nodes/{node}/qemu/{vmid}/template). PVE requires the VM to be stopped, and
// the conversion is one-way: a template cannot be turned back into a regular
// VM (clone it instead). Templates are the source for linked clones — see
// [CloneSpec.Full].
//
// The endpoint's return shape is unconfirmed on 9.x (PVE may answer with a
// conversion task UPID or synchronously with null), so callers must check
// [tasks.Ref.IsZero] before awaiting the returned Ref — the
// nodes.ApplyNetworkConfig precedent.
func (s *Service) ConvertToTemplate(ctx context.Context, vmid int, opts ...ConvertOption) (tasks.Ref, error) {
	cfg := convertConfig{vals: url.Values{}}
	for _, opt := range opts {
		opt(&cfg)
	}
	var body any
	if len(cfg.vals) > 0 {
		body = cfg.vals
	}
	var upid *string
	if err := s.c.DoRequest(ctx, http.MethodPost, s.vmPath(vmid)+"/template", body, &upid); err != nil {
		return tasks.Ref{}, fmt.Errorf("qemu.ConvertToTemplate: %w", err)
	}
	if upid == nil || *upid == "" {
		return tasks.Ref{}, nil // synchronous conversion: no task to await.
	}
	return svcutil.TaskRef("qemu.ConvertToTemplate", *upid)
}
