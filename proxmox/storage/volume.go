package storage

import (
	"context"
	"fmt"
	"net/http"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/internal/svcutil"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/tasks"
)

// VolumeCreateSpec is the body of POST /nodes/{node}/storage/{storage}/content,
// which allocates a new volume. Filename and Size are required. Pass it by
// pointer.
//
// PVE allocates synchronously and answers with the new volume id, so
// CreateVolume returns the volid string rather than a task.
type VolumeCreateSpec struct {
	Filename string `json:"filename"`         // required, e.g. "vm-100-disk-1".
	Size     string `json:"size"`             // required, PVE size: "10G", "512M".
	Format   string `json:"format,omitempty"` // "raw", "qcow2", "vmdk"; storage default if empty.
	VMID     int    `json:"vmid,omitempty"`   // owning guest, when the volume backs one.
	// Extra carries PVE parameters the SDK does not model.
	Extra map[string]string `json:"-"`
}

// CreateVolume allocates a volume on node/storage and returns its volume id.
// Allocation is synchronous (no task).
func (s *Service) CreateVolume(ctx context.Context, node, storage string, spec *VolumeCreateSpec) (string, error) {
	if spec == nil {
		return "", fmt.Errorf("storage.CreateVolume: %w", svcutil.ErrNilSpec)
	}
	switch {
	case spec.Filename == "":
		return "", fmt.Errorf("storage.CreateVolume: filename: %w", svcutil.ErrMissingField)
	case spec.Size == "":
		return "", fmt.Errorf("storage.CreateVolume: size: %w", svcutil.ErrMissingField)
	}
	body, err := svcutil.EncodeWithExtra(spec, spec.Extra)
	if err != nil {
		return "", fmt.Errorf("storage.CreateVolume: %w", err)
	}
	var volid string
	if derr := s.c.DoRequest(ctx, http.MethodPost, nodeContentPath(node, storage), body, &volid); derr != nil {
		return "", fmt.Errorf("storage.CreateVolume: %w", derr)
	}
	return volid, nil
}

// DeleteVolume frees a volume and returns the removal task. PVE may answer a
// synchronous free with no task (the returned Ref is then the zero value).
func (s *Service) DeleteVolume(ctx context.Context, node, storage, volid string) (tasks.Ref, error) {
	var upid string
	if err := s.c.DoRequest(ctx, http.MethodDelete, nodeVolumePath(node, storage, volid), nil, &upid); err != nil {
		return tasks.Ref{}, fmt.Errorf("storage.DeleteVolume: %w", err)
	}
	if upid == "" {
		return tasks.Ref{}, nil
	}
	return svcutil.TaskRef("storage.DeleteVolume", upid)
}
