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

func TestDLBGatedPre92(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newCappedService(t, mock, "9.1") // below the 9.2 gate.
	ctx := context.Background()

	if _, err := svc.GetDLBStatus(ctx); !errors.Is(err, pverr.ErrUnsupported) {
		t.Errorf("GetDLBStatus on 9.1 = %v, want ErrUnsupported", err)
	}
	if err := svc.SetDLBConfig(ctx, &ha.DLBConfig{Enabled: true}); !errors.Is(err, pverr.ErrUnsupported) {
		t.Errorf("SetDLBConfig on 9.1 = %v, want ErrUnsupported", err)
	}
}

func TestDLBRoundTrip(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newCappedService(t, mock, "9.2") // gate satisfied.
	ctx := context.Background()

	status, err := svc.GetDLBStatus(ctx)
	if err != nil {
		t.Fatalf("GetDLBStatus: %v", err)
	}
	if bool(status.Enabled) {
		t.Errorf("default DLB enabled = true, want false")
	}

	if err := svc.SetDLBConfig(ctx, &ha.DLBConfig{Enabled: types.PVEBool(true), Mode: "static"}); err != nil {
		t.Fatalf("SetDLBConfig: %v", err)
	}

	status, err = svc.GetDLBStatus(ctx)
	if err != nil {
		t.Fatalf("GetDLBStatus after set: %v", err)
	}
	if !bool(status.Enabled) || status.Mode != "static" {
		t.Errorf("DLB status = %+v, want enabled=true mode=static", status)
	}
}

func TestSetDLBConfigValidation(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newCappedService(t, mock, "9.2")

	if err := svc.SetDLBConfig(context.Background(), nil); err == nil {
		t.Error("SetDLBConfig(nil) error = nil, want non-nil")
	}
}
