package nodes

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/internal/svcutil"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/tasks"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/types"
)

// Disk is one physical disk from GET /nodes/{node}/disks/list. Reads are
// lossless: keys outside the typed set land in Extra.
type Disk struct {
	DevPath string        `json:"devpath"`
	Model   string        `json:"model,omitempty"`
	Serial  string        `json:"serial,omitempty"`
	Vendor  string        `json:"vendor,omitempty"`
	Size    int64         `json:"size,omitempty"` // bytes.
	Type    string        `json:"type,omitempty"` // ssd, hdd, nvme, usb.
	Used    string        `json:"used,omitempty"` // e.g. "LVM", "ZFS", "partitions".
	Health  string        `json:"health,omitempty"`
	Wearout int           `json:"wearout,omitempty"` // SSD wear indicator, percent.
	RPM     int           `json:"rpm,omitempty"`
	WWN     string        `json:"wwn,omitempty"`
	GPT     types.PVEBool `json:"gpt,omitempty"`
	OSDID   int           `json:"osdid,omitempty"` // Ceph OSD id, -1 when unused.
	// Extra carries disk keys the SDK does not model.
	Extra map[string]string `json:"-"`
}

var diskKnownFields = map[string]bool{
	"devpath": true, "model": true, "serial": true, "vendor": true,
	"size": true, "type": true, "used": true, "health": true,
	"wearout": true, "rpm": true, "wwn": true, "gpt": true, "osdid": true,
}

// UnmarshalJSON decodes the modelled fields and routes unknown keys into Extra.
func (d *Disk) UnmarshalJSON(data []byte) error {
	type alias Disk
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("decode disk: %w", err)
	}
	*d = Disk(a)
	extra, err := svcutil.DecodeExtra(data, diskKnownFields)
	if err != nil {
		return fmt.Errorf("decode disk: %w", err)
	}
	d.Extra = extra
	return nil
}

// SMARTAttribute is one row of a disk's SMART attribute table.
type SMARTAttribute struct {
	ID        int    `json:"id,omitempty"`
	Name      string `json:"name,omitempty"`
	Value     int    `json:"value,omitempty"`
	Worst     int    `json:"worst,omitempty"`
	Threshold int    `json:"threshold,omitempty"`
	Raw       string `json:"raw,omitempty"`
	Flags     string `json:"flags,omitempty"`
	// Extra carries attribute keys the SDK does not model.
	Extra map[string]string `json:"-"`
}

// SMART is the payload of GET /nodes/{node}/disks/smart. Health is the overall
// self-assessment ("PASSED"/"FAILED"); Attributes is the per-attribute table for
// ATA disks, absent for others (Text then carries the raw smartctl output).
//
// This is REST-with-caveat: the endpoint is real, but the attribute-table shape
// is device-dependent and was not confirmed against a live node. Unmodelled keys
// are preserved in Extra.
type SMART struct {
	Health     string           `json:"health,omitempty"`
	Type       string           `json:"type,omitempty"`
	Attributes []SMARTAttribute `json:"attributes,omitempty"`
	Text       string           `json:"text,omitempty"`
	// Extra carries top-level keys the SDK does not model.
	Extra map[string]string `json:"-"`
}

var smartKnownFields = map[string]bool{
	"health": true, "type": true, "attributes": true, "text": true,
}

// UnmarshalJSON decodes the modelled fields and routes unknown keys into Extra.
func (sm *SMART) UnmarshalJSON(data []byte) error {
	type alias SMART
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("decode smart: %w", err)
	}
	*sm = SMART(a)
	extra, err := svcutil.DecodeExtra(data, smartKnownFields)
	if err != nil {
		return fmt.Errorf("decode smart: %w", err)
	}
	sm.Extra = extra
	return nil
}

// ListDisks returns node's physical disks and their basic health.
func (s *Service) ListDisks(ctx context.Context, node string) ([]Disk, error) {
	var disks []Disk
	if err := s.c.DoRequest(ctx, http.MethodGet, nodeDisksListPath(node), nil, &disks); err != nil {
		return nil, fmt.Errorf("nodes.ListDisks: %w", err)
	}
	return disks, nil
}

// GetDiskSMART returns the SMART self-assessment for one disk (e.g. "/dev/sda").
// See the SMART doc-comment for the REST-with-caveat status of the attribute
// table.
func (s *Service) GetDiskSMART(ctx context.Context, node, disk string) (*SMART, error) {
	if disk == "" {
		return nil, fmt.Errorf("nodes.GetDiskSMART: disk: %w", svcutil.ErrMissingField)
	}
	path := nodeDisksSMARTPath(node) + "?" + url.Values{"disk": {disk}}.Encode()
	var out SMART
	if err := s.c.DoRequest(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, fmt.Errorf("nodes.GetDiskSMART: %w", err)
	}
	return &out, nil
}

// InitializeDisk writes a fresh GPT partition table to disk (e.g. "/dev/sdb"),
// wiping it (POST /nodes/{node}/disks/initgpt). It runs as a worker; the
// returned tasks.Ref is awaited for completion.
func (s *Service) InitializeDisk(ctx context.Context, node, disk string) (tasks.Ref, error) {
	if disk == "" {
		return tasks.Ref{}, fmt.Errorf("nodes.InitializeDisk: disk: %w", svcutil.ErrMissingField)
	}
	body := url.Values{"disk": {disk}}
	var upid string
	if err := s.c.DoRequest(ctx, http.MethodPost, nodeDisksInitGPTPath(node), body, &upid); err != nil {
		return tasks.Ref{}, fmt.Errorf("nodes.InitializeDisk: %w", err)
	}
	return svcutil.TaskRef("nodes.InitializeDisk", upid)
}
