package pbs

import (
	"encoding/json"
	"fmt"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/internal/svcutil"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/types"
)

// BackupJob is one scheduled backup job from GET /cluster/backup[/{id}]. Reads
// are lossless: the many selection/retention keys land in Extra.
type BackupJob struct {
	ID       string        `json:"id"`
	Schedule string        `json:"schedule,omitempty"`
	Storage  string        `json:"storage,omitempty"`
	Mode     string        `json:"mode,omitempty"` // snapshot, suspend, or stop.
	Enabled  types.PVEBool `json:"enabled,omitempty"`
	Mailto   string        `json:"mailto,omitempty"`
	Comment  string        `json:"comment,omitempty"`
	// Extra carries job keys the SDK does not model (all, vmid, exclude,
	// prune-backups, notes-template, …).
	Extra map[string]string `json:"-"`
}

var backupJobKnownFields = map[string]bool{
	"id": true, "schedule": true, "storage": true, "mode": true,
	"enabled": true, "mailto": true, "comment": true,
}

// UnmarshalJSON decodes the modelled fields and routes unknown keys into Extra.
func (j *BackupJob) UnmarshalJSON(data []byte) error {
	type alias BackupJob
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("decode backup job: %w", err)
	}
	*j = BackupJob(a)
	extra, err := svcutil.DecodeExtra(data, backupJobKnownFields)
	if err != nil {
		return fmt.Errorf("decode backup job: %w", err)
	}
	j.Extra = extra
	return nil
}

// BackupJobSpec is the body of POST /cluster/backup. Storage is required; ID is
// optional (PVE assigns one when empty). VMID is a CSV of guest ids, mutually
// exclusive with All. Pass it by pointer.
type BackupJobSpec struct {
	ID       string         `json:"id,omitempty"`
	Schedule string         `json:"schedule,omitempty"`
	Storage  string         `json:"storage"`
	Mode     string         `json:"mode,omitempty"`
	Enabled  *types.PVEBool `json:"enabled,omitempty"`
	All      *types.PVEBool `json:"all,omitempty"`
	VMID     string         `json:"vmid,omitempty"`
	Mailto   string         `json:"mailto,omitempty"`
	Comment  string         `json:"comment,omitempty"`
	// Extra carries PVE parameters the SDK does not model.
	Extra map[string]string `json:"-"`
}

// BackupJobUpdate is the body of PUT /cluster/backup/{id}. All fields optional;
// use Delete to unset keys. Pass it by pointer.
type BackupJobUpdate struct {
	Schedule string         `json:"schedule,omitempty"`
	Storage  string         `json:"storage,omitempty"`
	Mode     string         `json:"mode,omitempty"`
	Enabled  *types.PVEBool `json:"enabled,omitempty"`
	Mailto   string         `json:"mailto,omitempty"`
	Comment  string         `json:"comment,omitempty"`
	Delete   string         `json:"delete,omitempty"`
	// Extra carries PVE parameters the SDK does not model.
	Extra map[string]string `json:"-"`
}

// Backup is one backup archive from the storage content listing
// (GET /nodes/{node}/storage/{storage}/content?content=backup). Reads are
// lossless.
type Backup struct {
	VolID  string `json:"volid"`
	Format string `json:"format,omitempty"`
	Size   int64  `json:"size,omitempty"`
	CTime  int64  `json:"ctime,omitempty"` // unix epoch.
	VMID   int    `json:"vmid,omitempty"`
	Notes  string `json:"notes,omitempty"`
	// Extra carries content keys the SDK does not model (verification, protected,
	// …).
	Extra map[string]string `json:"-"`
}

var backupKnownFields = map[string]bool{
	"volid": true, "format": true, "size": true, "ctime": true,
	"vmid": true, "notes": true, "content": true,
}

// UnmarshalJSON decodes the modelled fields and routes unknown keys into Extra.
func (b *Backup) UnmarshalJSON(data []byte) error {
	type alias Backup
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("decode backup: %w", err)
	}
	*b = Backup(a)
	extra, err := svcutil.DecodeExtra(data, backupKnownFields)
	if err != nil {
		return fmt.Errorf("decode backup: %w", err)
	}
	b.Extra = extra
	return nil
}

// VzdumpSpec is the body of POST /nodes/{node}/vzdump — an immediate backup.
// Storage is required; VMID (a CSV of ids) or All selects the guests. Pass it by
// pointer.
type VzdumpSpec struct {
	VMID     string         `json:"vmid,omitempty"`
	All      *types.PVEBool `json:"all,omitempty"`
	Storage  string         `json:"storage"`
	Mode     string         `json:"mode,omitempty"`     // snapshot, suspend, or stop.
	Compress string         `json:"compress,omitempty"` // 0, 1, gzip, lzo, or zstd.
	Notes    string         `json:"notes-template,omitempty"`
	// Extra carries PVE parameters the SDK does not model.
	Extra map[string]string `json:"-"`
}

// RestoreSpec is the body of a restore — creating a guest from a backup archive.
// VMID and Archive (a backup volid) are required. Pass it by pointer.
type RestoreSpec struct {
	VMID    types.VMID     `json:"vmid"`
	Archive string         `json:"-"` // placed into "archive" (QEMU) or "ostemplate" (LXC).
	Storage string         `json:"storage,omitempty"`
	Force   *types.PVEBool `json:"force,omitempty"`
	// Extra carries PVE parameters the SDK does not model.
	Extra map[string]string `json:"-"`
}
