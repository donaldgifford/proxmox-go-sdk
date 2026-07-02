package pbs

import (
	"context"
	"fmt"
	"net/http"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/internal/svcutil"
)

// ListBackupJobs returns the scheduled backup jobs.
func (s *Service) ListBackupJobs(ctx context.Context) ([]BackupJob, error) {
	var jobs []BackupJob
	if err := s.c.DoRequest(ctx, http.MethodGet, clusterBackupPath(), nil, &jobs); err != nil {
		return nil, fmt.Errorf("pbs.ListBackupJobs: %w", err)
	}
	return jobs, nil
}

// GetBackupJob returns one scheduled backup job by id.
func (s *Service) GetBackupJob(ctx context.Context, id string) (*BackupJob, error) {
	if id == "" {
		return nil, fmt.Errorf("pbs.GetBackupJob: id: %w", svcutil.ErrMissingField)
	}
	var j BackupJob
	if err := s.c.DoRequest(ctx, http.MethodGet, clusterBackupJobPath(id), nil, &j); err != nil {
		return nil, fmt.Errorf("pbs.GetBackupJob: %w", err)
	}
	if j.ID == "" {
		j.ID = id
	}
	return &j, nil
}

// CreateBackupJob creates a scheduled backup job. Storage is required. The write
// is synchronous (no task).
func (s *Service) CreateBackupJob(ctx context.Context, spec *BackupJobSpec) error {
	if spec == nil {
		return fmt.Errorf("pbs.CreateBackupJob: %w", svcutil.ErrNilSpec)
	}
	if spec.Storage == "" {
		return fmt.Errorf("pbs.CreateBackupJob: storage: %w", svcutil.ErrMissingField)
	}
	body, err := svcutil.EncodeWithExtra(spec, spec.Extra)
	if err != nil {
		return fmt.Errorf("pbs.CreateBackupJob: %w", err)
	}
	if err := s.c.DoRequest(ctx, http.MethodPost, clusterBackupPath(), body, nil); err != nil {
		return fmt.Errorf("pbs.CreateBackupJob: %w", err)
	}
	return nil
}

// UpdateBackupJob changes a scheduled backup job. The write is synchronous (no
// task).
func (s *Service) UpdateBackupJob(ctx context.Context, id string, update *BackupJobUpdate) error {
	if update == nil {
		return fmt.Errorf("pbs.UpdateBackupJob: %w", svcutil.ErrNilSpec)
	}
	if id == "" {
		return fmt.Errorf("pbs.UpdateBackupJob: id: %w", svcutil.ErrMissingField)
	}
	body, err := svcutil.EncodeWithExtra(update, update.Extra)
	if err != nil {
		return fmt.Errorf("pbs.UpdateBackupJob: %w", err)
	}
	if err := s.c.DoRequest(ctx, http.MethodPut, clusterBackupJobPath(id), body, nil); err != nil {
		return fmt.Errorf("pbs.UpdateBackupJob: %w", err)
	}
	return nil
}

// DeleteBackupJob removes a scheduled backup job. The write is synchronous (no
// task).
func (s *Service) DeleteBackupJob(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("pbs.DeleteBackupJob: id: %w", svcutil.ErrMissingField)
	}
	if err := s.c.DoRequest(ctx, http.MethodDelete, clusterBackupJobPath(id), nil, nil); err != nil {
		return fmt.Errorf("pbs.DeleteBackupJob: %w", err)
	}
	return nil
}
