package ha_test

import (
	"context"
	"errors"
	"testing"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/ha"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/mockpve"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/pverr"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/types"
)

func TestListReplicationJobs(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddReplicationJob("100-0", "pve2", "*/15")
	mock.AddReplicationJob("101-0", "pve3", "*/30")
	svc := newService(t, mock)

	jobs, err := svc.ListReplicationJobs(context.Background())
	if err != nil {
		t.Fatalf("ListReplicationJobs: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("ListReplicationJobs returned %d, want 2", len(jobs))
	}
}

func TestGetReplicationJob(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddReplicationJob("100-0", "pve2", "*/15")
	svc := newService(t, mock)

	job, err := svc.GetReplicationJob(context.Background(), "100-0")
	if err != nil {
		t.Fatalf("GetReplicationJob: %v", err)
	}
	if job.ID != "100-0" || job.Target != "pve2" || job.Type != "local" {
		t.Errorf("job = %+v, want id=100-0 target=pve2 type=local", job)
	}
}

func TestGetReplicationJobNotFound(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)

	if _, err := svc.GetReplicationJob(context.Background(), "999-0"); !errors.Is(err, pverr.ErrNotFound) {
		t.Fatalf("GetReplicationJob(ghost) = %v, want ErrNotFound", err)
	}
}

func TestCreateReplicationJob(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)
	ctx := context.Background()

	err := svc.CreateReplicationJob(ctx, &ha.ReplicationSpec{
		ID:       "100-0",
		Target:   "pve2",
		Schedule: "*/15",
		Rate:     10,
	})
	if err != nil {
		t.Fatalf("CreateReplicationJob: %v", err)
	}

	job, err := svc.GetReplicationJob(ctx, "100-0")
	if err != nil {
		t.Fatalf("GetReplicationJob after create: %v", err)
	}
	if job.Target != "pve2" || job.Rate != 10 {
		t.Errorf("job = %+v, want target=pve2 rate=10", job)
	}
}

func TestCreateReplicationJobValidation(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)
	ctx := context.Background()

	if err := svc.CreateReplicationJob(ctx, nil); err == nil {
		t.Error("CreateReplicationJob(nil) error = nil, want non-nil")
	}
	if err := svc.CreateReplicationJob(ctx, &ha.ReplicationSpec{Target: "pve2"}); err == nil {
		t.Error("CreateReplicationJob(no id) error = nil, want non-nil")
	}
	if err := svc.CreateReplicationJob(ctx, &ha.ReplicationSpec{ID: "100-0"}); err == nil {
		t.Error("CreateReplicationJob(no target) error = nil, want non-nil")
	}
}

func TestUpdateReplicationJob(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddReplicationJob("100-0", "pve2", "*/15")
	svc := newService(t, mock)
	ctx := context.Background()

	disabled := types.PVEBool(true)
	if err := svc.UpdateReplicationJob(ctx, "100-0", &ha.ReplicationUpdate{
		Schedule: "*/30",
		Disable:  &disabled,
	}); err != nil {
		t.Fatalf("UpdateReplicationJob: %v", err)
	}

	job, err := svc.GetReplicationJob(ctx, "100-0")
	if err != nil {
		t.Fatalf("GetReplicationJob after update: %v", err)
	}
	if job.Schedule != "*/30" || !bool(job.Disable) {
		t.Errorf("job after update = %+v, want schedule=*/30 disable=true", job)
	}
}

func TestUpdateReplicationJobValidation(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)
	ctx := context.Background()

	if err := svc.UpdateReplicationJob(ctx, "100-0", nil); err == nil {
		t.Error("UpdateReplicationJob(nil) error = nil, want non-nil")
	}
	if err := svc.UpdateReplicationJob(ctx, "", &ha.ReplicationUpdate{Schedule: "x"}); err == nil {
		t.Error("UpdateReplicationJob(no id) error = nil, want non-nil")
	}
}

func TestDeleteReplicationJob(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddReplicationJob("100-0", "pve2", "*/15")
	svc := newService(t, mock)
	ctx := context.Background()

	if err := svc.DeleteReplicationJob(ctx, "100-0"); err != nil {
		t.Fatalf("DeleteReplicationJob: %v", err)
	}
	if _, err := svc.GetReplicationJob(ctx, "100-0"); !errors.Is(err, pverr.ErrNotFound) {
		t.Fatalf("GetReplicationJob after delete = %v, want ErrNotFound", err)
	}
}

func TestDeleteReplicationJobNotFound(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)

	if err := svc.DeleteReplicationJob(context.Background(), "999-0"); !errors.Is(err, pverr.ErrNotFound) {
		t.Fatalf("DeleteReplicationJob(ghost) = %v, want ErrNotFound", err)
	}
}
