package metrics

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/types"
)

// RRDOption tunes an RRD query (timeframe and consolidation function).
type RRDOption func(*rrdQuery)

type rrdQuery struct {
	timeframe Timeframe
	cf        ConsolidationFunc
}

// WithTimeframe sets the RRD window (default TimeframeHour when unset).
func WithTimeframe(tf Timeframe) RRDOption {
	return func(q *rrdQuery) { q.timeframe = tf }
}

// WithConsolidation sets the RRD consolidation function (AVERAGE or MAX). When
// unset, PVE applies its own default.
func WithConsolidation(cf ConsolidationFunc) RRDOption {
	return func(q *rrdQuery) { q.cf = cf }
}

// query resolves the options into the URL query params. timeframe defaults to
// hour (PVE requires it); cf is sent only when set.
func (q rrdQuery) values() url.Values {
	tf := q.timeframe
	if tf == "" {
		tf = TimeframeHour
	}
	v := url.Values{"timeframe": {string(tf)}}
	if q.cf != "" {
		v.Set("cf", string(q.cf))
	}
	return v
}

// GetNodeRRD returns node's RRD time series (CPU, memory, network, disk). Pass
// WithTimeframe / WithConsolidation to tune the window; the default is the last
// hour.
func (s *Service) GetNodeRRD(ctx context.Context, node string, opts ...RRDOption) ([]RRDPoint, error) {
	path := nodeRRDPath(node) + "?" + resolveRRD(opts).Encode()
	var points []RRDPoint
	if err := s.c.DoRequest(ctx, http.MethodGet, path, nil, &points); err != nil {
		return nil, fmt.Errorf("metrics.GetNodeRRD: %w", err)
	}
	return points, nil
}

// GetVMRRD returns a guest's RRD time series. kind selects QEMU or LXC. The
// series carries VM-scoped metrics (cpu, mem, disk, net); pressure-stall and
// ZFS-ARC counters, where present, are REST-with-caveat and land in Extra.
func (s *Service) GetVMRRD(ctx context.Context, node string, kind VMKind, vmid types.VMID, opts ...RRDOption) ([]RRDPoint, error) {
	switch kind {
	case KindQEMU, KindLXC:
	default:
		return nil, fmt.Errorf("metrics.GetVMRRD: kind %q: %w", kind, errBadKind)
	}
	path := vmRRDPath(node, kind, vmid) + "?" + resolveRRD(opts).Encode()
	var points []RRDPoint
	if err := s.c.DoRequest(ctx, http.MethodGet, path, nil, &points); err != nil {
		return nil, fmt.Errorf("metrics.GetVMRRD: %w", err)
	}
	return points, nil
}

func resolveRRD(opts []RRDOption) url.Values {
	var q rrdQuery
	for _, opt := range opts {
		opt(&q)
	}
	return q.values()
}
