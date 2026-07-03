// Package pbs wraps the Proxmox VE 9.x PVE-side backup surface: scheduled backup
// jobs, immediate (vzdump) backups, backup listing, and restore.
//
// The scope is mixed, so the Service binds no node:
//
//   - Scheduled backup jobs are cluster-scoped (/cluster/backup). ListBackupJobs
//     / GetBackupJob plus the Create/Update/Delete writes, which are synchronous
//     (they return an error, not a tasks.Ref).
//   - Node backups take the node per-call: ListNodeBackups reads the storage
//     content listing; CreateBackup (vzdump), RestoreQEMU, and RestoreLXC run as
//     workers and return a tasks.Ref to await. Restore is create-with-archive, so
//     it reuses the guest-create endpoints.
//
// This is the PVE side only. Talking to a Proxmox Backup Server directly — its
// own host, authentication, and datastore API, including verification and prune
// — is a separate concern reserved for a future pbsclient. Consequently
// VerifyBackup has no PVE REST endpoint and returns pverr.ErrUnsupported; its
// signature is stable so it becomes a real call if PVE ever proxies
// verification.
//
// Construct a Service with NewService or the root client's PBS accessor; one
// *Service is safe for concurrent use.
//
// See docs/design/0001-proxmox-sdk-package-layout.md and
// docs/impl/0001-proxmox-ve-9x-sdk-coverage.md.
package pbs
