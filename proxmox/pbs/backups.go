package pbs

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/internal/svcutil"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/pverr"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/tasks"
)

// ListNodeBackups returns the backup archives on node's storage (the storage
// content listing filtered to content=backup).
func (s *Service) ListNodeBackups(ctx context.Context, node, storage string) ([]Backup, error) {
	if storage == "" {
		return nil, fmt.Errorf("pbs.ListNodeBackups: storage: %w", svcutil.ErrMissingField)
	}
	path := nodeStorageContentPath(node, storage) + "?" + url.Values{"content": {"backup"}}.Encode()
	var backups []Backup
	if err := s.c.DoRequest(ctx, http.MethodGet, path, nil, &backups); err != nil {
		return nil, fmt.Errorf("pbs.ListNodeBackups: %w", err)
	}
	return backups, nil
}

// CreateBackup starts an immediate backup on node (POST /nodes/{node}/vzdump).
// It runs as a worker; the returned tasks.Ref is awaited for completion.
func (s *Service) CreateBackup(ctx context.Context, node string, spec *VzdumpSpec) (tasks.Ref, error) {
	if spec == nil {
		return tasks.Ref{}, fmt.Errorf("pbs.CreateBackup: %w", svcutil.ErrNilSpec)
	}
	if spec.Storage == "" {
		return tasks.Ref{}, fmt.Errorf("pbs.CreateBackup: storage: %w", svcutil.ErrMissingField)
	}
	body, err := svcutil.EncodeWithExtra(spec, spec.Extra)
	if err != nil {
		return tasks.Ref{}, fmt.Errorf("pbs.CreateBackup: %w", err)
	}
	var upid string
	if err := s.c.DoRequest(ctx, http.MethodPost, nodeVzdumpPath(node), body, &upid); err != nil {
		return tasks.Ref{}, fmt.Errorf("pbs.CreateBackup: %w", err)
	}
	return svcutil.TaskRef("pbs.CreateBackup", upid)
}

// RestoreQEMU restores a QEMU VM from a backup archive (POST /nodes/{node}/qemu
// with the archive as its source). It runs as a worker; the returned tasks.Ref
// is awaited for completion.
func (s *Service) RestoreQEMU(ctx context.Context, node string, spec *RestoreSpec) (tasks.Ref, error) {
	body, err := s.restoreBody("pbs.RestoreQEMU", spec)
	if err != nil {
		return tasks.Ref{}, err
	}
	body.Set("archive", spec.Archive)
	var upid string
	if err := s.c.DoRequest(ctx, http.MethodPost, nodeQEMUPath(node), body, &upid); err != nil {
		return tasks.Ref{}, fmt.Errorf("pbs.RestoreQEMU: %w", err)
	}
	return svcutil.TaskRef("pbs.RestoreQEMU", upid)
}

// RestoreLXC restores an LXC container from a backup archive (POST
// /nodes/{node}/lxc with restore=1). It runs as a worker; the returned tasks.Ref
// is awaited for completion.
func (s *Service) RestoreLXC(ctx context.Context, node string, spec *RestoreSpec) (tasks.Ref, error) {
	body, err := s.restoreBody("pbs.RestoreLXC", spec)
	if err != nil {
		return tasks.Ref{}, err
	}
	// LXC restore takes the archive as ostemplate and the restore flag.
	body.Set("ostemplate", spec.Archive)
	body.Set("restore", "1")
	var upid string
	if err := s.c.DoRequest(ctx, http.MethodPost, nodeLXCPath(node), body, &upid); err != nil {
		return tasks.Ref{}, fmt.Errorf("pbs.RestoreLXC: %w", err)
	}
	return svcutil.TaskRef("pbs.RestoreLXC", upid)
}

// restoreBody validates a RestoreSpec and encodes its common fields (the archive
// param is added by the caller, since QEMU and LXC name it differently).
func (*Service) restoreBody(op string, spec *RestoreSpec) (url.Values, error) {
	if spec == nil {
		return nil, fmt.Errorf("%s: %w", op, svcutil.ErrNilSpec)
	}
	switch {
	case spec.VMID == 0:
		return nil, fmt.Errorf("%s: vmid: %w", op, svcutil.ErrMissingField)
	case spec.Archive == "":
		return nil, fmt.Errorf("%s: archive: %w", op, svcutil.ErrMissingField)
	}
	body, err := svcutil.EncodeWithExtra(spec, spec.Extra)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	return body, nil
}

// VerifyBackup would verify a backup archive's integrity.
//
// Backup verification is a Proxmox Backup Server operation (the PBS-native
// /admin/datastore/{store}/verify API), not a PVE-side one: Proxmox VE 9.x
// exposes no confirmed REST endpoint to trigger it. Rather than fabricate a path
// that would 404, VerifyBackup returns a pverr.ErrUnsupported-wrapped error. Use
// the PBS server (a future pbsclient) or the GUI meanwhile; the signature is
// stable, so this becomes a real call if PVE ever proxies verification.
func (*Service) VerifyBackup(_ context.Context, _, _, _ string) (tasks.Ref, error) {
	return tasks.Ref{}, fmt.Errorf(
		"pbs.VerifyBackup: backup verification is a PBS-native operation with no "+
			"PVE REST endpoint; use the PBS server: %w", pverr.ErrUnsupported,
	)
}
