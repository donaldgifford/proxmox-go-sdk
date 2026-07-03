package ceph

import (
	"encoding/json"
	"fmt"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/internal/svcutil"
)

// Pool is one Ceph pool from GET /nodes/{node}/ceph/pools[/{name}]. Reads are
// lossless: the many placement/replication keys land in Extra.
type Pool struct {
	Name    string `json:"pool_name"`
	Size    int    `json:"size,omitempty"`
	MinSize int    `json:"min_size,omitempty"`
	PGNum   int    `json:"pg_num,omitempty"`
	Type    string `json:"type,omitempty"` // replicated or erasure.
	// Extra carries pool keys the SDK does not model (crush_rule,
	// application_metadata, autoscale, …).
	Extra map[string]string `json:"-"`
}

var poolKnownFields = map[string]bool{
	"pool_name": true, "size": true, "min_size": true, "pg_num": true, "type": true,
}

// UnmarshalJSON decodes the modelled fields and routes unknown keys into Extra.
func (p *Pool) UnmarshalJSON(data []byte) error {
	type alias Pool
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("decode ceph pool: %w", err)
	}
	*p = Pool(a)
	extra, err := svcutil.DecodeExtra(data, poolKnownFields)
	if err != nil {
		return fmt.Errorf("decode ceph pool: %w", err)
	}
	p.Extra = extra
	return nil
}

// PoolSpec is the body of POST /nodes/{node}/ceph/pools. Name is required. Pass
// it by pointer.
type PoolSpec struct {
	Name        string `json:"name"`
	Size        int    `json:"size,omitempty"`
	MinSize     int    `json:"min_size,omitempty"`
	PGNum       int    `json:"pg_num,omitempty"`
	CrushRule   string `json:"crush_rule,omitempty"`
	Application string `json:"application,omitempty"` // rbd, cephfs, or rgw.
	// Extra carries PVE parameters the SDK does not model.
	Extra map[string]string `json:"-"`
}

// OSD is one node in the CRUSH/OSD tree from GET /nodes/{node}/ceph/osd. The
// tree is recursive (root → host → osd); leaves carry a Status. Reads are
// lossless.
type OSD struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"` // root, host, or osd.
	Status   string `json:"status,omitempty"`
	Children []OSD  `json:"children,omitempty"`
	// Extra carries OSD keys the SDK does not model (in/out, reweight, …).
	Extra map[string]string `json:"-"`
}

var osdKnownFields = map[string]bool{
	"id": true, "name": true, "type": true, "status": true, "children": true,
}

// UnmarshalJSON decodes the modelled fields (children recursively) and routes
// unknown keys into Extra.
func (o *OSD) UnmarshalJSON(data []byte) error {
	type alias OSD
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("decode ceph osd: %w", err)
	}
	*o = OSD(a)
	extra, err := svcutil.DecodeExtra(data, osdKnownFields)
	if err != nil {
		return fmt.Errorf("decode ceph osd: %w", err)
	}
	o.Extra = extra
	return nil
}

// OSDTree is the payload of GET /nodes/{node}/ceph/osd: the CRUSH tree plus the
// cluster-wide OSD flags. Reads are lossless.
type OSDTree struct {
	Flags string `json:"flags,omitempty"`
	Root  *OSD   `json:"root,omitempty"`
	// Extra carries top-level keys the SDK does not model.
	Extra map[string]string `json:"-"`
}

var osdTreeKnownFields = map[string]bool{
	"flags": true, "root": true,
}

// UnmarshalJSON decodes the modelled fields and routes unknown keys into Extra.
func (t *OSDTree) UnmarshalJSON(data []byte) error {
	type alias OSDTree
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("decode ceph osd tree: %w", err)
	}
	*t = OSDTree(a)
	extra, err := svcutil.DecodeExtra(data, osdTreeKnownFields)
	if err != nil {
		return fmt.Errorf("decode ceph osd tree: %w", err)
	}
	t.Extra = extra
	return nil
}

// OSDSpec is the body of POST /nodes/{node}/ceph/osd, creating an OSD on a block
// device. DevPath (e.g. "/dev/sdb") is required. Pass it by pointer.
type OSDSpec struct {
	DevPath string `json:"dev"`
	DBDev   string `json:"db_dev,omitempty"`  // optional separate DB device.
	WALDev  string `json:"wal_dev,omitempty"` // optional separate WAL device.
	// Extra carries PVE parameters the SDK does not model (crush-device-class, …).
	Extra map[string]string `json:"-"`
}

// HealthStatus is the Ceph health block nested in Status.
type HealthStatus struct {
	Status string `json:"status,omitempty"` // HEALTH_OK, HEALTH_WARN, HEALTH_ERR.
}

// Status is the payload of GET /nodes/{node}/ceph/status: the live cluster
// health and maps. Reads are lossless — pgmap/monmap/osdmap and the rest land in
// Extra.
type Status struct {
	FSID   string        `json:"fsid,omitempty"`
	Health *HealthStatus `json:"health,omitempty"`
	// Extra carries status keys the SDK does not model.
	Extra map[string]string `json:"-"`
}

var statusKnownFields = map[string]bool{
	"fsid": true, "health": true,
}

// UnmarshalJSON decodes the modelled fields and routes unknown keys into Extra.
func (st *Status) UnmarshalJSON(data []byte) error {
	type alias Status
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("decode ceph status: %w", err)
	}
	*st = Status(a)
	extra, err := svcutil.DecodeExtra(data, statusKnownFields)
	if err != nil {
		return fmt.Errorf("decode ceph status: %w", err)
	}
	st.Extra = extra
	return nil
}
