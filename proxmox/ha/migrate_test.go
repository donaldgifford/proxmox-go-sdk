package ha_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/ha"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/internal/svcutil"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/mockpve"
)

// Migrate and relocate echo the accepted intent and move the resource's
// service row to the requested node, observable via HAStatusCurrent.
func TestMigrateRelocateMovesNode(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddHAResource("vm:100", "started")
	svc := newCappedService(t, mock, "9.2")
	ctx := context.Background()

	res, err := svc.MigrateResource(ctx, "vm:100", "pve2")
	if err != nil {
		t.Fatalf("MigrateResource: %v", err)
	}
	if res.SID != "vm:100" || res.RequestedNode != "pve2" {
		t.Errorf("migrate result = %+v, want sid vm:100 requested-node pve2", res)
	}
	if len(res.BlockingResources) != 0 {
		t.Errorf("BlockingResources = %v, want none (mock does not schedule)", res.BlockingResources)
	}
	entries, err := svc.HAStatusCurrent(ctx)
	if err != nil {
		t.Fatalf("HAStatusCurrent: %v", err)
	}
	if got := serviceEntry(t, entries, "vm:100").Node; got != "pve2" {
		t.Errorf("service node after migrate = %q, want pve2", got)
	}

	res, err = svc.RelocateResource(ctx, "vm:100", "pve3")
	if err != nil {
		t.Fatalf("RelocateResource: %v", err)
	}
	if res.RequestedNode != "pve3" {
		t.Errorf("relocate requested-node = %q, want pve3", res.RequestedNode)
	}
	entries, err = svc.HAStatusCurrent(ctx)
	if err != nil {
		t.Fatalf("HAStatusCurrent after relocate: %v", err)
	}
	if got := serviceEntry(t, entries, "vm:100").Node; got != "pve3" {
		t.Errorf("service node after relocate = %q, want pve3", got)
	}
}

// Missing sid or node is refused client-side before any request.
func TestMigrateValidation(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newCappedService(t, mock, "9.2")
	ctx := context.Background()

	if _, err := svc.MigrateResource(ctx, "", "pve2"); !errors.Is(err, svcutil.ErrMissingField) {
		t.Errorf("MigrateResource without sid = %v, want ErrMissingField", err)
	}
	if _, err := svc.RelocateResource(ctx, "vm:100", ""); !errors.Is(err, svcutil.ErrMissingField) {
		t.Errorf("RelocateResource without node = %v, want ErrMissingField", err)
	}
}

// MigrateResult decodes the affinity-aware body losslessly: typed
// blocking-resources with the cause enum, comigrated resources, and unknown
// keys preserved in Extra at both levels.
func TestMigrateResultLossless(t *testing.T) {
	t.Parallel()
	raw := `{
		"sid": "vm:100", "requested-node": "pve2",
		"blocking-resources": [{"sid": "vm:101", "cause": "resource-affinity", "rule": "keep-apart"}],
		"comigrated-resources": ["vm:102"],
		"hint": "conflict"
	}`
	var res ha.MigrateResult
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if res.SID != "vm:100" || res.RequestedNode != "pve2" {
		t.Errorf("typed fields mis-decoded: %+v", res)
	}
	if len(res.BlockingResources) != 1 ||
		res.BlockingResources[0].SID != "vm:101" ||
		res.BlockingResources[0].Cause != ha.BlockingCauseResourceAffinity {
		t.Errorf("BlockingResources = %+v, want vm:101/resource-affinity", res.BlockingResources)
	}
	if res.BlockingResources[0].Extra["rule"] != "keep-apart" {
		t.Errorf("blocking Extra[rule] = %q, want keep-apart", res.BlockingResources[0].Extra["rule"])
	}
	if len(res.ComigratedResources) != 1 || res.ComigratedResources[0] != "vm:102" {
		t.Errorf("ComigratedResources = %v, want [vm:102]", res.ComigratedResources)
	}
	if res.Extra["hint"] != "conflict" {
		t.Errorf("Extra[hint] = %q, want conflict", res.Extra["hint"])
	}
}
