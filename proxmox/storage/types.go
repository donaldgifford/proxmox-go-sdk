package storage

import (
	"encoding/json"
	"fmt"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/types"
)

// Datastore is one entry from GET /storage or GET /storage/{storage}. It is the
// cluster-wide storage configuration, not per-node usage (see StorageStatus).
// Unknown keys are preserved in Extra so reads are lossless.
type Datastore struct {
	Storage string        `json:"storage"`           // unique storage ID.
	Type    string        `json:"type"`              // "dir", "lvm", "lvmthin", "zfspool", "nfs", "cifs", "rbd", …
	Content string        `json:"content,omitempty"` // comma-separated: "images,iso,backup,vztmpl,snippets".
	Path    string        `json:"path,omitempty"`    // for type=dir.
	Pool    string        `json:"pool,omitempty"`    // for type=zfspool / lvmthin.
	Server  string        `json:"server,omitempty"`  // for type=nfs / cifs.
	Export  string        `json:"export,omitempty"`  // NFS export path.
	Share   string        `json:"share,omitempty"`   // CIFS share name.
	Nodes   string        `json:"nodes,omitempty"`   // comma-separated node restriction; empty = all.
	Shared  types.PVEBool `json:"shared,omitempty"`
	Disable types.PVEBool `json:"disable,omitempty"`
	// Extra carries datastore keys the SDK does not model.
	Extra map[string]string `json:"-"`
}

// datastoreKnownFields lists the JSON keys Datastore models directly; keep it in
// sync with the struct so UnmarshalJSON routes only the rest into Extra.
var datastoreKnownFields = map[string]bool{
	"storage": true, "type": true, "content": true, "path": true,
	"pool": true, "server": true, "export": true, "share": true,
	"nodes": true, "shared": true, "disable": true,
}

// UnmarshalJSON decodes the modelled fields and routes any unknown keys into
// Extra so a datastore read round-trips losslessly.
func (d *Datastore) UnmarshalJSON(data []byte) error {
	type alias Datastore
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("decode datastore: %w", err)
	}
	*d = Datastore(a)

	var all map[string]json.RawMessage
	if err := json.Unmarshal(data, &all); err != nil {
		return fmt.Errorf("decode datastore map: %w", err)
	}
	for key, raw := range all {
		if datastoreKnownFields[key] {
			continue
		}
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			s = string(raw) // non-string field: keep the raw token.
		}
		if d.Extra == nil {
			d.Extra = make(map[string]string)
		}
		d.Extra[key] = s
	}
	return nil
}

// StorageStatus is one entry from GET /nodes/{node}/storage or the data of
// GET /nodes/{node}/storage/{storage}/status — per-node activation and usage.
type StorageStatus struct {
	Storage string        `json:"storage"`
	Type    string        `json:"type"`
	Content string        `json:"content,omitempty"`
	Active  types.PVEBool `json:"active,omitempty"`
	Enabled types.PVEBool `json:"enabled,omitempty"`
	Shared  types.PVEBool `json:"shared,omitempty"`
	Total   int64         `json:"total,omitempty"` // bytes.
	Used    int64         `json:"used,omitempty"`  // bytes.
	Avail   int64         `json:"avail,omitempty"` // bytes.
}

// Content is one entry from GET /nodes/{node}/storage/{storage}/content: a
// stored object (disk image, ISO, container template, backup, or snippet).
type Content struct {
	Volid     string        `json:"volid"`             // e.g. "local:iso/debian-12.iso".
	Content   string        `json:"content,omitempty"` // the content type: "iso", "images", "backup", …
	Format    string        `json:"format,omitempty"`  // "raw", "qcow2", "iso", "tgz", …
	Size      int64         `json:"size,omitempty"`    // bytes.
	Used      int64         `json:"used,omitempty"`    // bytes (thin-provisioned).
	CTime     int64         `json:"ctime,omitempty"`   // Unix seconds.
	VMID      int           `json:"vmid,omitempty"`    // owning guest; 0 if unowned.
	Notes     string        `json:"notes,omitempty"`
	Protected types.PVEBool `json:"protected,omitempty"`
}
