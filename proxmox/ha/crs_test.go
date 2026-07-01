package ha_test

import (
	"context"
	"testing"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/ha"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/mockpve"
)

func TestGetCRSSettings(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.SetCRS("ha=static,ha-rebalance-on-start=1")
	svc := newService(t, mock)

	crs, err := svc.GetCRSSettings(context.Background())
	if err != nil {
		t.Fatalf("GetCRSSettings: %v", err)
	}
	if crs.Mode != "static" || !crs.HARebalanceOnStart {
		t.Errorf("crs = %+v, want mode=static ha-rebalance-on-start=true", crs)
	}
}

func TestGetCRSSettingsDefault(t *testing.T) {
	t.Parallel()
	mock := mockpve.New() // no crs seeded.
	svc := newService(t, mock)

	crs, err := svc.GetCRSSettings(context.Background())
	if err != nil {
		t.Fatalf("GetCRSSettings: %v", err)
	}
	if crs.Mode != "" || crs.HARebalanceOnStart {
		t.Errorf("default crs = %+v, want zero values", crs)
	}
}

func TestSetCRSSettings(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)
	ctx := context.Background()

	rebalance := true
	if err := svc.SetCRSSettings(ctx, &ha.CRSSettingsUpdate{
		Mode:               "static",
		HARebalanceOnStart: &rebalance,
	}); err != nil {
		t.Fatalf("SetCRSSettings: %v", err)
	}

	crs, err := svc.GetCRSSettings(ctx)
	if err != nil {
		t.Fatalf("GetCRSSettings after set: %v", err)
	}
	if crs.Mode != "static" || !crs.HARebalanceOnStart {
		t.Errorf("crs after set = %+v, want mode=static ha-rebalance-on-start=true", crs)
	}
}

func TestSetCRSSettingsValidation(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)
	ctx := context.Background()

	if err := svc.SetCRSSettings(ctx, nil); err == nil {
		t.Error("SetCRSSettings(nil) error = nil, want non-nil")
	}
	if err := svc.SetCRSSettings(ctx, &ha.CRSSettingsUpdate{}); err == nil {
		t.Error("SetCRSSettings(empty) error = nil, want non-nil")
	}
}
