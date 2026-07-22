package ha_test

import (
	"context"
	"errors"
	"testing"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/ha"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/pverr"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/version"
)

// The Dynamic Load Balancer ops are reclassified to ErrUnsupported — PVE has
// no DLB REST endpoint (INV-0004 Finding 4; the provisional
// /cluster/ha/lbalancer path would 404 live). The service is built with a nil
// api.Client, so any attempt to issue a request would panic: passing proves
// the ops return before touching the wire, on any version.
func TestDLBUnsupported(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, ver := range []string{"9.0.4", "9.2.1"} {
		caps, err := version.Parse(ver)
		if err != nil {
			t.Fatalf("version.Parse(%q): %v", ver, err)
		}
		svc := ha.NewService(nil, caps)

		if _, err := svc.GetDLBStatus(ctx); !errors.Is(err, pverr.ErrUnsupported) {
			t.Errorf("GetDLBStatus on %s = %v, want ErrUnsupported", ver, err)
		}
		if err := svc.SetDLBConfig(ctx, &ha.DLBConfig{Enabled: true}); !errors.Is(err, pverr.ErrUnsupported) {
			t.Errorf("SetDLBConfig on %s = %v, want ErrUnsupported", ver, err)
		}
		// Even a nil config resolves to ErrUnsupported (no endpoint to
		// validate against) — the storage.VolumeSnapshots precedent.
		if err := svc.SetDLBConfig(ctx, nil); !errors.Is(err, pverr.ErrUnsupported) {
			t.Errorf("SetDLBConfig(nil) on %s = %v, want ErrUnsupported", ver, err)
		}
	}
}
