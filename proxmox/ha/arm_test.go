package ha_test

import (
	"context"
	"errors"
	"testing"

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

// DisarmHA still reports ErrUnsupported regardless of version until the real
// disarm op lands (IMPL-0005 task 2).
func TestDisarmUnsupported(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newCappedService(t, mock, "9.2")
	if err := svc.DisarmHA(context.Background()); !errors.Is(err, pverr.ErrUnsupported) {
		t.Errorf("DisarmHA = %v, want ErrUnsupported", err)
	}
}
