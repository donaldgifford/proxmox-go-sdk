package ha_test

import (
	"context"
	"errors"
	"testing"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/ha"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/internal/svcutil"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/mockpve"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/pverr"
)

// ArmHA is gated on the 9.2 HAClusterSwitch capability: a pre-9.2 cluster is
// refused with ErrUnsupported before any request is issued.
func TestArmHAGateRefusal(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newCappedService(t, mock, "9.1")
	if err := svc.ArmHA(context.Background()); !errors.Is(err, pverr.ErrUnsupported) {
		t.Errorf("ArmHA on 9.1 = %v, want ErrUnsupported", err)
	}
}

// DisarmHA shares the ArmHA gate and additionally requires a resource-mode:
// an empty mode is refused client-side before any request.
func TestDisarmHAValidation(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	ctx := context.Background()

	if err := newCappedService(t, mock, "9.1").DisarmHA(ctx, ha.ResourceModeFreeze); !errors.Is(err, pverr.ErrUnsupported) {
		t.Errorf("DisarmHA on 9.1 = %v, want ErrUnsupported", err)
	}
	if err := newCappedService(t, mock, "9.2").DisarmHA(ctx, ""); !errors.Is(err, svcutil.ErrMissingField) {
		t.Errorf("DisarmHA with empty mode = %v, want ErrMissingField", err)
	}
}
