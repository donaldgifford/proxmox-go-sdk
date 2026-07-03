package pbs_test

import (
	"context"
	"errors"
	"testing"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/mockpve"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/pbs"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/pverr"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/tasks"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/types"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/version"
)

const testNode = "pve"

func newService(t *testing.T, mock *mockpve.Server) *pbs.Service {
	t.Helper()
	c, cleanup := mock.NewClient()
	t.Cleanup(cleanup)
	return pbs.NewService(c, version.Capabilities{})
}

func newServiceAndTasks(t *testing.T, mock *mockpve.Server) (*pbs.Service, *tasks.Service) {
	t.Helper()
	c, cleanup := mock.NewClient()
	t.Cleanup(cleanup)
	return pbs.NewService(c, version.Capabilities{}), tasks.NewService(c)
}

func TestBackupJobs(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddBackupJob("backup-0", "pbs-store", "02:00")
	svc := newService(t, mock)
	ctx := context.Background()

	jobs, err := svc.ListBackupJobs(ctx)
	if err != nil {
		t.Fatalf("ListBackupJobs: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("ListBackupJobs returned %d, want 1", len(jobs))
	}

	if err := svc.CreateBackupJob(ctx, &pbs.BackupJobSpec{
		ID: "nightly", Storage: "pbs-store", Schedule: "03:00", Mode: "snapshot",
	}); err != nil {
		t.Fatalf("CreateBackupJob: %v", err)
	}
	job, err := svc.GetBackupJob(ctx, "nightly")
	if err != nil {
		t.Fatalf("GetBackupJob: %v", err)
	}
	if job.Storage != "pbs-store" || job.Schedule != "03:00" {
		t.Errorf("created job = %+v, want pbs-store @ 03:00", job)
	}

	if err := svc.UpdateBackupJob(ctx, "nightly", &pbs.BackupJobUpdate{Schedule: "04:00"}); err != nil {
		t.Fatalf("UpdateBackupJob: %v", err)
	}
	job, err = svc.GetBackupJob(ctx, "nightly")
	if err != nil {
		t.Fatalf("GetBackupJob after update: %v", err)
	}
	if job.Schedule != "04:00" {
		t.Errorf("schedule after update = %q, want 04:00", job.Schedule)
	}

	if err := svc.DeleteBackupJob(ctx, "nightly"); err != nil {
		t.Fatalf("DeleteBackupJob: %v", err)
	}
	if _, err := svc.GetBackupJob(ctx, "nightly"); !errors.Is(err, pverr.ErrNotFound) {
		t.Fatalf("GetBackupJob after delete = %v, want ErrNotFound", err)
	}
}

func TestBackupJobValidation(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)
	ctx := context.Background()

	if err := svc.CreateBackupJob(ctx, nil); err == nil {
		t.Error("CreateBackupJob(nil) error = nil, want non-nil")
	}
	if err := svc.CreateBackupJob(ctx, &pbs.BackupJobSpec{}); err == nil {
		t.Error("CreateBackupJob(no storage) error = nil, want non-nil")
	}
}

func TestListNodeBackups(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddStorage("pbs-store", "pbs", "backup", 1<<40, 0)
	mock.AddVolume(testNode, "pbs-store", "pbs-store:backup/vzdump-qemu-100-2026_07_01.vma.zst", "backup", "vma.zst", 1<<30)
	mock.AddVolume(testNode, "pbs-store", "pbs-store:iso/debian.iso", "iso", "iso", 1<<29)
	svc := newService(t, mock)

	backups, err := svc.ListNodeBackups(context.Background(), testNode, "pbs-store")
	if err != nil {
		t.Fatalf("ListNodeBackups: %v", err)
	}
	// Only the backup volume, not the ISO, is returned.
	if len(backups) != 1 {
		t.Fatalf("ListNodeBackups returned %d, want 1 (backup only)", len(backups))
	}
	if backups[0].Format != "vma.zst" {
		t.Errorf("backup format = %q, want vma.zst", backups[0].Format)
	}
}

func TestCreateBackup(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc, ts := newServiceAndTasks(t, mock)
	ctx := context.Background()

	ref, err := svc.CreateBackup(ctx, testNode, &pbs.VzdumpSpec{
		VMID: "100", Storage: "pbs-store", Mode: "snapshot", Compress: "zstd",
	})
	if err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}
	st, err := ts.Wait(ctx, ref)
	if err != nil {
		t.Fatalf("Wait(vzdump): %v", err)
	}
	if !st.OK() {
		t.Errorf("vzdump task exit = %q, want OK", st.ExitStatus)
	}
}

func TestCreateBackupValidation(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)
	ctx := context.Background()

	if _, err := svc.CreateBackup(ctx, testNode, nil); err == nil {
		t.Error("CreateBackup(nil) error = nil, want non-nil")
	}
	if _, err := svc.CreateBackup(ctx, testNode, &pbs.VzdumpSpec{VMID: "100"}); err == nil {
		t.Error("CreateBackup(no storage) error = nil, want non-nil")
	}
}

func TestRestore(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc, ts := newServiceAndTasks(t, mock)
	ctx := context.Background()

	qref, err := svc.RestoreQEMU(ctx, testNode, &pbs.RestoreSpec{
		VMID: types.VMID(200), Archive: "pbs-store:backup/vzdump-qemu-100.vma.zst", Storage: "local-lvm",
	})
	if err != nil {
		t.Fatalf("RestoreQEMU: %v", err)
	}
	if _, err := ts.Wait(ctx, qref); err != nil {
		t.Fatalf("Wait(restore qemu): %v", err)
	}

	lref, err := svc.RestoreLXC(ctx, testNode, &pbs.RestoreSpec{
		VMID: types.VMID(201), Archive: "pbs-store:backup/vzdump-lxc-101.tar.zst", Storage: "local-lvm",
	})
	if err != nil {
		t.Fatalf("RestoreLXC: %v", err)
	}
	if _, err := ts.Wait(ctx, lref); err != nil {
		t.Fatalf("Wait(restore lxc): %v", err)
	}
}

func TestRestoreValidation(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)
	ctx := context.Background()

	if _, err := svc.RestoreQEMU(ctx, testNode, nil); err == nil {
		t.Error("RestoreQEMU(nil) error = nil, want non-nil")
	}
	if _, err := svc.RestoreQEMU(ctx, testNode, &pbs.RestoreSpec{Archive: "a"}); err == nil {
		t.Error("RestoreQEMU(no vmid) error = nil, want non-nil")
	}
	if _, err := svc.RestoreLXC(ctx, testNode, &pbs.RestoreSpec{VMID: types.VMID(1)}); err == nil {
		t.Error("RestoreLXC(no archive) error = nil, want non-nil")
	}
}

func TestVerifyBackupUnsupported(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)

	if _, err := svc.VerifyBackup(context.Background(), testNode, "pbs-store", "pbs-store:backup/x"); !errors.Is(err, pverr.ErrUnsupported) {
		t.Errorf("VerifyBackup = %v, want ErrUnsupported", err)
	}
}
