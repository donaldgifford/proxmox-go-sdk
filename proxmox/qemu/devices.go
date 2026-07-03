package qemu

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/internal/svcutil"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/tasks"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/types"
)

// MoveDiskSpec is the body of POST /nodes/{node}/qemu/{vmid}/move_disk: relocate
// a VM disk to another storage. Disk and TargetStorage are required. Pass it by
// pointer.
type MoveDiskSpec struct {
	Disk          string         `json:"disk"`             // required, e.g. "scsi0".
	TargetStorage string         `json:"storage"`          // required, the destination storage.
	Format        string         `json:"format,omitempty"` // "raw", "qcow2", "vmdk".
	Delete        *types.PVEBool `json:"delete,omitempty"` // remove the source volume after the move.
	// Extra carries PVE parameters the SDK does not model.
	Extra map[string]string `json:"-"`
}

// MoveDisk relocates a VM disk to another storage and returns the move task.
// This is the guest-scoped counterpart to the storage service's allocate/free:
// PVE has no storage-level volume-move endpoint.
func (s *Service) MoveDisk(ctx context.Context, vmid int, spec *MoveDiskSpec) (tasks.Ref, error) {
	if spec == nil {
		return tasks.Ref{}, fmt.Errorf("qemu.MoveDisk: %w", svcutil.ErrNilSpec)
	}
	switch {
	case spec.Disk == "":
		return tasks.Ref{}, fmt.Errorf("qemu.MoveDisk: disk: %w", svcutil.ErrMissingField)
	case spec.TargetStorage == "":
		return tasks.Ref{}, fmt.Errorf("qemu.MoveDisk: storage: %w", svcutil.ErrMissingField)
	}
	body, err := svcutil.EncodeWithExtra(spec, spec.Extra)
	if err != nil {
		return tasks.Ref{}, fmt.Errorf("qemu.MoveDisk: %w", err)
	}
	var upid string
	if derr := s.c.DoRequest(ctx, http.MethodPost, s.vmPath(vmid)+"/move_disk", body, &upid); derr != nil {
		return tasks.Ref{}, fmt.Errorf("qemu.MoveDisk: %w", derr)
	}
	return svcutil.TaskRef("qemu.MoveDisk", upid)
}

// DiskSpec describes a disk to attach to a VM. AddDisk renders it to the PVE
// volume syntax "<storage>:<size-in-GiB>[,opt=val…]" and sets it on Slot.
type DiskSpec struct {
	Slot    string            // config key, e.g. "scsi1" or "virtio0".
	Storage string            // storage pool, e.g. "local-lvm".
	SizeGB  int               // new disk size in GiB.
	Options map[string]string // extra volume options, e.g. {"discard": "on"}.
}

func (d *DiskSpec) value() string {
	return appendOptions(d.Storage+":"+strconv.Itoa(d.SizeGB), d.Options)
}

// NICSpec describes a network interface. AddNIC renders it to the PVE syntax
// "<model>[,bridge=…][,tag=…][,opt=val…]" and sets it on Slot.
type NICSpec struct {
	Slot    string            // config key, e.g. "net0".
	Model   string            // device model, e.g. "virtio" or "e1000".
	Bridge  string            // host bridge, e.g. "vmbr0".
	VLAN    int               // 802.1q tag; 0 means untagged.
	Options map[string]string // extra options, e.g. {"firewall": "1"}.
}

func (n *NICSpec) value() string {
	opts := make(map[string]string, len(n.Options)+2)
	for k, v := range n.Options {
		opts[k] = v
	}
	if n.Bridge != "" {
		opts["bridge"] = n.Bridge
	}
	if n.VLAN > 0 {
		opts["tag"] = strconv.Itoa(n.VLAN)
	}
	return appendOptions(n.Model, opts)
}

// appendOptions appends ",k=v" pairs to base, sorted by key for deterministic
// output.
func appendOptions(base string, opts map[string]string) string {
	if len(opts) == 0 {
		return base
	}
	keys := make([]string, 0, len(opts))
	for k := range opts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(base)
	for _, k := range keys {
		b.WriteString("," + k + "=" + opts[k])
	}
	return b.String()
}

// AddDisk attaches a newly allocated disk described by spec. It is a config
// change, so PVE applies it synchronously (zero Ref) unless the running VM
// hot-plugs the disk, in which case the returned Ref tracks the work.
func (s *Service) AddDisk(ctx context.Context, vmid int, spec *DiskSpec) (tasks.Ref, error) {
	if spec == nil {
		return tasks.Ref{}, fmt.Errorf("qemu.AddDisk: %w", svcutil.ErrNilSpec)
	}
	if spec.Slot == "" || spec.Storage == "" || spec.SizeGB <= 0 {
		return tasks.Ref{}, fmt.Errorf("qemu.AddDisk: slot, storage, and a positive size: %w", svcutil.ErrMissingField)
	}
	return s.SetConfig(ctx, vmid, &ConfigUpdate{Extra: map[string]string{spec.Slot: spec.value()}})
}

// RemoveDisk detaches the disk at slot (e.g. "scsi1"). PVE moves it to an
// "unused" entry rather than deleting the underlying volume.
func (s *Service) RemoveDisk(ctx context.Context, vmid int, slot string) (tasks.Ref, error) {
	if slot == "" {
		return tasks.Ref{}, fmt.Errorf("qemu.RemoveDisk: slot: %w", svcutil.ErrMissingField)
	}
	return s.SetConfig(ctx, vmid, &ConfigUpdate{Delete: slot})
}

// AddNIC attaches a network interface described by spec.
func (s *Service) AddNIC(ctx context.Context, vmid int, spec *NICSpec) (tasks.Ref, error) {
	if spec == nil {
		return tasks.Ref{}, fmt.Errorf("qemu.AddNIC: %w", svcutil.ErrNilSpec)
	}
	if spec.Slot == "" || spec.Model == "" {
		return tasks.Ref{}, fmt.Errorf("qemu.AddNIC: slot and model: %w", svcutil.ErrMissingField)
	}
	return s.SetConfig(ctx, vmid, &ConfigUpdate{Extra: map[string]string{spec.Slot: spec.value()}})
}

// RemoveNIC detaches the network interface at slot (e.g. "net1").
func (s *Service) RemoveNIC(ctx context.Context, vmid int, slot string) (tasks.Ref, error) {
	if slot == "" {
		return tasks.Ref{}, fmt.Errorf("qemu.RemoveNIC: slot: %w", svcutil.ErrMissingField)
	}
	return s.SetConfig(ctx, vmid, &ConfigUpdate{Delete: slot})
}

// ResizeDisk grows the disk at slot. size is the PVE size expression: "+5G" to
// grow by 5 GiB, or an absolute "20G" (PVE only permits growing). The change is
// synchronous, so the returned Ref is usually the zero value.
func (s *Service) ResizeDisk(ctx context.Context, vmid int, disk, size string) (tasks.Ref, error) {
	if disk == "" || size == "" {
		return tasks.Ref{}, fmt.Errorf("qemu.ResizeDisk: disk and size: %w", svcutil.ErrMissingField)
	}
	body := url.Values{"disk": {disk}, "size": {size}}
	var upid string
	if err := s.c.DoRequest(ctx, http.MethodPut, s.vmPath(vmid)+"/resize", body, &upid); err != nil {
		return tasks.Ref{}, fmt.Errorf("qemu.ResizeDisk: %w", err)
	}
	if upid == "" {
		return tasks.Ref{}, nil
	}
	return svcutil.TaskRef("qemu.ResizeDisk", upid)
}
