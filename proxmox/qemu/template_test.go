package qemu_test

import (
	"context"
	"errors"
	"testing"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/mockpve"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/pverr"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/qemu"
)

// TestConvertToTemplate converts a stopped VM and observes the template flag
// through both the list and the config read.
func TestConvertToTemplate(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	const vmid = 9210
	mock.AddVM(testNode, vmid, "tmpl-src", "stopped")
	svc, ts := newServices(t, mock)
	ctx := context.Background()

	ref, err := svc.ConvertToTemplate(ctx, vmid)
	if err != nil {
		t.Fatalf("ConvertToTemplate: %v", err)
	}
	// The mock emulates PVE's task-returning shape; the SDK contract only
	// promises a maybe-UPID, so await only when one came back.
	if !ref.IsZero() {
		awaitOK(t, ts, ref)
	}

	vms, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	found := false
	for i := range vms {
		if int(vms[i].VMID) == vmid {
			found = true
			if !vms[i].Template.Bool() {
				t.Errorf("List entry %d Template = false, want true", vmid)
			}
		}
	}
	if !found {
		t.Fatalf("List: VM %d missing", vmid)
	}

	cfg, err := svc.Config(ctx, vmid)
	if err != nil {
		t.Fatalf("Config: %v", err)
	}
	if !cfg.Template.Bool() {
		t.Errorf("Config Template = false, want true")
	}
}

// TestConvertToTemplateWithDisk exercises the disk-scoped option end to end;
// the mock accepts (and ignores) the disk param like it does other unmodelled
// form fields.
func TestConvertToTemplateWithDisk(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	const vmid = 9211
	mock.AddVM(testNode, vmid, "tmpl-disk", "stopped")
	svc, _ := newServices(t, mock)

	if _, err := svc.ConvertToTemplate(context.Background(), vmid, qemu.WithTemplateDisk("scsi0")); err != nil {
		t.Fatalf("ConvertToTemplate(WithTemplateDisk): %v", err)
	}
}

// TestConvertToTemplateRunningRejected mirrors real PVE: a running VM cannot
// become a template.
func TestConvertToTemplateRunningRejected(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	const vmid = 9212
	mock.AddVM(testNode, vmid, "tmpl-running", "running")
	svc, _ := newServices(t, mock)

	_, err := svc.ConvertToTemplate(context.Background(), vmid)
	if err == nil {
		t.Fatal("ConvertToTemplate on a running VM: want error, got nil")
	}
	var pe *pverr.Error
	if !errors.As(err, &pe) || pe.Status != 400 {
		t.Fatalf("ConvertToTemplate error = %v, want *pverr.Error with status 400", err)
	}
}

// TestConvertToTemplateNotFound classifies an unknown VMID to ErrNotFound.
func TestConvertToTemplateNotFound(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc, _ := newServices(t, mock)

	_, err := svc.ConvertToTemplate(context.Background(), 9999)
	if !errors.Is(err, pverr.ErrNotFound) {
		t.Fatalf("ConvertToTemplate(9999) = %v, want ErrNotFound", err)
	}
}
