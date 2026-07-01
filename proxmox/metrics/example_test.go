package metrics_test

import (
	"context"
	"fmt"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/api"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/metrics"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/mockpve"
)

// Example reads a node's status and its RRD series, then lists the configured
// external metric servers.
func Example() {
	// mockpve stands in for a live cluster so the example is self-contained;
	// against a real node, pass its URL and a real token to proxmox.NewClient.
	mock := mockpve.New()
	mock.AddMetricServer("influx1", "influxdb", "10.0.0.9", 8086)
	ts := mock.Serve()
	defer ts.Close()

	ctx := context.Background()
	c, err := proxmox.NewClient(ctx, ts.URL, api.TokenCredentials("root@pam!sdk", "secret"))
	if err != nil {
		fmt.Println("connect:", err)
		return
	}
	m := c.Metrics()

	status, err := m.GetNodeStatus(ctx, "pve")
	if err != nil {
		fmt.Println("node status:", err)
		return
	}
	series, err := m.GetNodeRRD(ctx, "pve", metrics.WithTimeframe(metrics.TimeframeHour))
	if err != nil {
		fmt.Println("node rrd:", err)
		return
	}
	servers, err := m.ListMetricServers(ctx)
	if err != nil {
		fmt.Println("metric servers:", err)
		return
	}
	fmt.Printf("uptime %ds, %d rrd sample(s), %d metric server(s)\n",
		status.Uptime, len(series), len(servers))
	// Output:
	// uptime 123456s, 2 rrd sample(s), 1 metric server(s)
}
