package ha_test

import (
	"context"
	"errors"
	"testing"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/mockpve"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/pverr"
)

// ArmHA/DisarmHA have no confirmed PVE REST endpoint, so they report
// ErrUnsupported regardless of the cluster version — even on 9.2 where the
// capability gate would otherwise pass.
func TestArmDisarmUnsupported(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newCappedService(t, mock, "9.2")
	ctx := context.Background()

	if err := svc.ArmHA(ctx); !errors.Is(err, pverr.ErrUnsupported) {
		t.Errorf("ArmHA = %v, want ErrUnsupported", err)
	}
	if err := svc.DisarmHA(ctx); !errors.Is(err, pverr.ErrUnsupported) {
		t.Errorf("DisarmHA = %v, want ErrUnsupported", err)
	}
}
