package qemu

import (
	"context"
	"fmt"
	"net/http"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/internal/svcutil"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/tasks"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/types"
)

// MigrateSpec is the body of POST /nodes/{node}/qemu/{vmid}/migrate. Target is
// the destination node and is required; Online requests a live migration. Pass
// it to Migrate by pointer.
type MigrateSpec struct {
	Target         string         `json:"target"`
	Online         *types.PVEBool `json:"online,omitempty"`
	WithLocalDisks *types.PVEBool `json:"with-local-disks,omitempty"`
	TargetStorage  string         `json:"targetstorage,omitempty"`
	Force          *types.PVEBool `json:"force,omitempty"`
	// Extra carries PVE parameters the SDK does not model.
	Extra map[string]string `json:"-"`
}

// Migrate moves a VM to another node — live when Online is set — and returns
// the migration task, which runs on the source node.
func (s *Service) Migrate(ctx context.Context, vmid int, spec *MigrateSpec) (tasks.Ref, error) {
	if spec == nil {
		return tasks.Ref{}, fmt.Errorf("qemu.Migrate: %w", svcutil.ErrNilSpec)
	}
	if spec.Target == "" {
		return tasks.Ref{}, fmt.Errorf("qemu.Migrate: target node: %w", svcutil.ErrMissingField)
	}
	body, err := svcutil.EncodeWithExtra(spec, spec.Extra)
	if err != nil {
		return tasks.Ref{}, fmt.Errorf("qemu.Migrate: %w", err)
	}
	var upid string
	if derr := s.c.DoRequest(ctx, http.MethodPost, s.vmPath(vmid)+"/migrate", body, &upid); derr != nil {
		return tasks.Ref{}, fmt.Errorf("qemu.Migrate: %w", derr)
	}
	return svcutil.TaskRef("qemu.Migrate", upid)
}
