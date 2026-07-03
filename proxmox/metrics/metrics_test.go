package metrics_test

import (
	"context"
	"errors"
	"testing"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/metrics"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/mockpve"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/pverr"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/types"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/version"
)

const testNode = "pve"

func newService(t *testing.T, mock *mockpve.Server) *metrics.Service {
	t.Helper()
	c, cleanup := mock.NewClient()
	t.Cleanup(cleanup)
	return metrics.NewService(c, version.Capabilities{})
}

func TestGetNodeRRD(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)

	points, err := svc.GetNodeRRD(context.Background(), testNode,
		metrics.WithTimeframe(metrics.TimeframeDay), metrics.WithConsolidation(metrics.CFAverage))
	if err != nil {
		t.Fatalf("GetNodeRRD: %v", err)
	}
	if len(points) != 2 {
		t.Fatalf("GetNodeRRD returned %d points, want 2", len(points))
	}
	if points[0].MaxCPU != 4 {
		t.Errorf("point[0].MaxCPU = %v, want 4", points[0].MaxCPU)
	}
}

func TestGetVMRRD(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)
	ctx := context.Background()

	for _, kind := range []metrics.VMKind{metrics.KindQEMU, metrics.KindLXC} {
		points, err := svc.GetVMRRD(ctx, testNode, kind, types.VMID(100))
		if err != nil {
			t.Fatalf("GetVMRRD(%s): %v", kind, err)
		}
		if len(points) != 2 {
			t.Errorf("GetVMRRD(%s) returned %d points, want 2", kind, len(points))
		}
	}
}

func TestGetVMRRDBadKind(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)

	if _, err := svc.GetVMRRD(context.Background(), testNode, "bogus", types.VMID(100)); err == nil {
		t.Fatal("GetVMRRD(bogus kind) error = nil, want non-nil")
	}
}

func TestGetNodeStatus(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)

	st, err := svc.GetNodeStatus(context.Background(), testNode)
	if err != nil {
		t.Fatalf("GetNodeStatus: %v", err)
	}
	if st.Uptime == 0 || st.Memory == nil || st.Memory.Total == 0 {
		t.Errorf("GetNodeStatus = %+v, want uptime + memory set", st)
	}
}

func TestGetNodeStatusValidation(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)

	if _, err := svc.GetNodeStatus(context.Background(), ""); err == nil {
		t.Error("GetNodeStatus(empty node) error = nil, want non-nil")
	}
}

func TestMetricServers(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddMetricServer("influx1", "influxdb", "10.0.0.9", 8086)
	svc := newService(t, mock)
	ctx := context.Background()

	servers, err := svc.ListMetricServers(ctx)
	if err != nil {
		t.Fatalf("ListMetricServers: %v", err)
	}
	if len(servers) != 1 {
		t.Fatalf("ListMetricServers returned %d, want 1", len(servers))
	}

	if err := svc.CreateMetricServer(ctx, &metrics.MetricServerSpec{
		ID: "graphite1", Type: "graphite", Server: "10.0.0.10", Port: 2003,
	}); err != nil {
		t.Fatalf("CreateMetricServer: %v", err)
	}

	srv, err := svc.GetMetricServer(ctx, "graphite1")
	if err != nil {
		t.Fatalf("GetMetricServer: %v", err)
	}
	if srv.Type != "graphite" || srv.Port != 2003 {
		t.Errorf("created server = %+v, want graphite:2003", srv)
	}

	if err := svc.UpdateMetricServer(ctx, "graphite1", &metrics.MetricServerUpdate{Port: 2004}); err != nil {
		t.Fatalf("UpdateMetricServer: %v", err)
	}
	srv, err = svc.GetMetricServer(ctx, "graphite1")
	if err != nil {
		t.Fatalf("GetMetricServer after update: %v", err)
	}
	if srv.Port != 2004 {
		t.Errorf("port after update = %d, want 2004", srv.Port)
	}

	if err := svc.DeleteMetricServer(ctx, "graphite1"); err != nil {
		t.Fatalf("DeleteMetricServer: %v", err)
	}
	if _, err := svc.GetMetricServer(ctx, "graphite1"); !errors.Is(err, pverr.ErrNotFound) {
		t.Fatalf("GetMetricServer after delete = %v, want ErrNotFound", err)
	}
}

func TestMetricServerValidation(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)
	ctx := context.Background()

	if err := svc.CreateMetricServer(ctx, nil); err == nil {
		t.Error("CreateMetricServer(nil) error = nil, want non-nil")
	}
	if err := svc.CreateMetricServer(ctx, &metrics.MetricServerSpec{Type: "influxdb"}); err == nil {
		t.Error("CreateMetricServer(no id) error = nil, want non-nil")
	}
	if err := svc.CreateMetricServer(ctx, &metrics.MetricServerSpec{ID: "x", Server: "h"}); err == nil {
		t.Error("CreateMetricServer(no type) error = nil, want non-nil")
	}
}

func TestOTelConfigUnsupported(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)
	ctx := context.Background()

	if _, err := svc.GetOTelConfig(ctx, testNode); !errors.Is(err, pverr.ErrUnsupported) {
		t.Errorf("GetOTelConfig = %v, want ErrUnsupported", err)
	}
	if err := svc.SetOTelConfig(ctx, testNode, &metrics.OTelConfig{}); !errors.Is(err, pverr.ErrUnsupported) {
		t.Errorf("SetOTelConfig = %v, want ErrUnsupported", err)
	}
}
