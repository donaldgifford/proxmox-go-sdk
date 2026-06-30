package storage

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/internal/svcutil"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/pverr"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/tasks"
)

// ZFSPool is one entry from GET /nodes/{node}/disks/zfs — a node's local ZFS
// pools. It is the listing view; GetZFSPool returns the per-pool device tree.
type ZFSPool struct {
	Name   string  `json:"name"`             // pool name.
	Size   int64   `json:"size,omitempty"`   // total bytes.
	Free   int64   `json:"free,omitempty"`   // free bytes.
	Alloc  int64   `json:"alloc,omitempty"`  // allocated bytes.
	Frag   int     `json:"frag,omitempty"`   // fragmentation percent.
	Dedup  float64 `json:"dedup,omitempty"`  // dedup ratio.
	Health string  `json:"health,omitempty"` // "ONLINE", "DEGRADED", "FAULTED", …
}

// ZFSPoolStatus is GET /nodes/{node}/disks/zfs/{name}: a pool's state plus its
// vdev tree (the parsed `zpool status`). Children nests the vdevs.
type ZFSPoolStatus struct {
	Name     string    `json:"name"`               // pool name.
	State    string    `json:"state,omitempty"`    // "ONLINE", "DEGRADED", "FAULTED".
	Scan     string    `json:"scan,omitempty"`     // last scrub/resilver summary.
	Errors   string    `json:"errors,omitempty"`   // "No known data errors", …
	Children []ZFSVdev `json:"children,omitempty"` // the vdev tree.
}

// ZFSVdev is a node in a pool's device tree: a top-level vdev (mirror, raidz, a
// bare disk) or a leaf device. It nests via Children.
type ZFSVdev struct {
	Name     string    `json:"name"`               // vdev or leaf device name.
	State    string    `json:"state,omitempty"`    // "ONLINE", "DEGRADED", "FAULTED".
	Read     int64     `json:"read,omitempty"`     // read error count.
	Write    int64     `json:"write,omitempty"`    // write error count.
	Cksum    int64     `json:"cksum,omitempty"`    // checksum error count.
	Msg      string    `json:"msg,omitempty"`      // status note, when present.
	Children []ZFSVdev `json:"children,omitempty"` // nested vdevs/devices.
}

// ZFSPoolSpec is the body of POST /nodes/{node}/disks/zfs. Name, RAIDLevel, and
// Devices are required. Pass it by pointer. Pool creation runs as a worker, so
// CreateZFSPool returns a tasks.Ref.
type ZFSPoolSpec struct {
	// Name is the pool name. Required.
	Name string `json:"name"`
	// RAIDLevel is the RAID topology. Required. One of "single", "mirror",
	// "raid10", "raidz", "raidz2", "raidz3", "draid", "draid2", "draid3".
	RAIDLevel string `json:"raidlevel"`
	// Ashift is the sector-size exponent (e.g. 12 for 4K sectors); 0 lets PVE
	// choose.
	Ashift int `json:"ashift,omitempty"`
	// Compression is the pool's compression algorithm ("on","off","lz4","zstd").
	Compression string `json:"compression,omitempty"`
	// Devices are the block devices to build the pool from (e.g. "/dev/sdb").
	// They are joined into PVE's comma-separated "devices" parameter rather than
	// JSON-encoded, so the field is excluded from struct marshalling.
	Devices []string `json:"-"`
	// Extra carries PVE parameters the SDK does not model (e.g. add_storage).
	Extra map[string]string `json:"-"`
}

// ListZFSPools lists the ZFS pools local to node.
func (s *Service) ListZFSPools(ctx context.Context, node string) ([]ZFSPool, error) {
	var pools []ZFSPool
	if err := s.c.DoRequest(ctx, http.MethodGet, nodeZFSPath(node), nil, &pools); err != nil {
		return nil, fmt.Errorf("storage.ListZFSPools: %w", err)
	}
	return pools, nil
}

// GetZFSPool returns one pool's state and device tree.
func (s *Service) GetZFSPool(ctx context.Context, node, name string) (*ZFSPoolStatus, error) {
	var status ZFSPoolStatus
	if err := s.c.DoRequest(ctx, http.MethodGet, nodeZFSPoolPath(node, name), nil, &status); err != nil {
		return nil, fmt.Errorf("storage.GetZFSPool: %w", err)
	}
	return &status, nil
}

// CreateZFSPool builds a ZFS pool on node from the spec's devices and returns
// the creation task.
func (s *Service) CreateZFSPool(ctx context.Context, node string, spec *ZFSPoolSpec) (tasks.Ref, error) {
	if spec == nil {
		return tasks.Ref{}, fmt.Errorf("storage.CreateZFSPool: %w", svcutil.ErrNilSpec)
	}
	switch {
	case spec.Name == "":
		return tasks.Ref{}, fmt.Errorf("storage.CreateZFSPool: name: %w", svcutil.ErrMissingField)
	case spec.RAIDLevel == "":
		return tasks.Ref{}, fmt.Errorf("storage.CreateZFSPool: raidlevel: %w", svcutil.ErrMissingField)
	case len(spec.Devices) == 0:
		return tasks.Ref{}, fmt.Errorf("storage.CreateZFSPool: devices: %w", svcutil.ErrMissingField)
	}
	body, err := svcutil.EncodeWithExtra(spec, spec.Extra)
	if err != nil {
		return tasks.Ref{}, fmt.Errorf("storage.CreateZFSPool: %w", err)
	}
	body.Set("devices", strings.Join(spec.Devices, ","))

	var upid string
	if derr := s.c.DoRequest(ctx, http.MethodPost, nodeZFSPath(node), body, &upid); derr != nil {
		return tasks.Ref{}, fmt.Errorf("storage.CreateZFSPool: %w", derr)
	}
	return svcutil.TaskRef("storage.CreateZFSPool", upid)
}

// RAIDZExpandSpec names a RAIDZ expansion: the pool and the device to attach to
// its RAIDZ vdev. Pass it by pointer.
type RAIDZExpandSpec struct {
	Pool   string `json:"-"` // target pool name.
	Device string `json:"-"` // block device to add to the RAIDZ vdev.
}

// ExpandRAIDZ would add a device to an existing RAIDZ vdev (OpenZFS 2.3,
// PVE 9.2). It is gated on the ZFSRAIDZExpansion capability, but Proxmox does
// not expose RAIDZ expansion over its REST API as of 9.x: it is a `zpool attach`
// operation. ExpandRAIDZ therefore always returns a pverr.ErrUnsupported-wrapped
// error — on a pre-9.2 cluster because the feature is absent, and on 9.2+ because
// there is no REST endpoint. Perform the expansion through the ssh side-channel
// (proxmox/ssh Client.Exec, "zpool attach <pool> <raidz-vdev> <device>") until a
// REST endpoint is confirmed against a live node.
//
// The signature returns a tasks.Ref so it can gain a real REST implementation
// later without a breaking change.
func (s *Service) ExpandRAIDZ(_ context.Context, _ string, spec *RAIDZExpandSpec) (tasks.Ref, error) {
	if spec == nil {
		return tasks.Ref{}, fmt.Errorf("storage.ExpandRAIDZ: %w", svcutil.ErrNilSpec)
	}
	if err := s.caps.Require("RAIDZ expansion", "9.2"); err != nil {
		return tasks.Ref{}, fmt.Errorf("storage.ExpandRAIDZ: %w", err)
	}
	return tasks.Ref{}, fmt.Errorf(
		"storage.ExpandRAIDZ: RAIDZ expansion has no PVE REST endpoint; "+
			"run `zpool attach %s` via the ssh side-channel: %w",
		spec.Pool, pverr.ErrUnsupported,
	)
}
