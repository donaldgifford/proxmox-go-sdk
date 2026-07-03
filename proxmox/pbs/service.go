package pbs

import (
	"context"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/api"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/tasks"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/version"
)

// Service wraps the PVE-side backup surface: scheduled backup jobs, immediate
// vzdump backups, backup listing, and restore. Its scope is mixed — backup jobs
// are cluster-scoped (/cluster/backup) while vzdump/list/restore are node-scoped
// (node per-call) — so the service binds no node. One *Service is safe for
// concurrent use; construct it with NewService or the root client's PBS
// accessor.
//
// This is the PVE side only. Talking to a Proxmox Backup Server directly (its
// own host, auth, and datastore API — including verification and prune) is a
// separate concern reserved for a future pbsclient.
type Service struct {
	c    api.Client
	caps version.Capabilities
}

// NewService returns a pbs Service. caps is accepted for parity with the other
// services; the PVE-side backup endpoints are baseline 9.0 and gate nothing.
func NewService(c api.Client, caps version.Capabilities) *Service {
	return &Service{c: c, caps: caps}
}

// API is the pbs service contract, published so consumers can stand in a test
// double for *Service. Backup-job writes are cluster-scoped and synchronous
// (return an error); vzdump backup and restore are node-scoped workers (return a
// tasks.Ref). VerifyBackup is a PBS-native operation with no PVE REST endpoint
// and returns pverr.ErrUnsupported (see backups.go).
type API interface {
	// Scheduled backup jobs (cluster scope, synchronous writes).
	ListBackupJobs(ctx context.Context) ([]BackupJob, error)
	GetBackupJob(ctx context.Context, id string) (*BackupJob, error)
	CreateBackupJob(ctx context.Context, spec *BackupJobSpec) error
	UpdateBackupJob(ctx context.Context, id string, update *BackupJobUpdate) error
	DeleteBackupJob(ctx context.Context, id string) error

	// Node backups (node scope).
	ListNodeBackups(ctx context.Context, node, storage string) ([]Backup, error)
	CreateBackup(ctx context.Context, node string, spec *VzdumpSpec) (tasks.Ref, error)
	RestoreQEMU(ctx context.Context, node string, spec *RestoreSpec) (tasks.Ref, error)
	RestoreLXC(ctx context.Context, node string, spec *RestoreSpec) (tasks.Ref, error)

	// VerifyBackup has no PVE REST endpoint (PBS-native) and returns
	// pverr.ErrUnsupported.
	VerifyBackup(ctx context.Context, node, storage, volid string) (tasks.Ref, error)
}

// Compile-time assertion that *Service implements the published contract.
var _ API = (*Service)(nil)
