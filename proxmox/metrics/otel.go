package metrics

import (
	"context"
	"fmt"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/pverr"
)

// GetOTelConfig would return the node's OpenTelemetry exporter configuration.
//
// PVE 9.1 ships an OpenTelemetry metrics exporter, but it is configured through
// files (not the REST API): there is no confirmed /nodes/{node}/... endpoint to
// read it. Rather than fabricate a path that would 404 against a real node,
// GetOTelConfig returns a pverr.ErrUnsupported-wrapped error (like ha.ArmHA).
// The version.Capabilities.OTelExporter gate (9.1) is available for the day PVE
// exposes a REST surface, at which point this becomes a real call without a
// signature change.
func (*Service) GetOTelConfig(_ context.Context, _ string) (*OTelConfig, error) {
	return nil, fmt.Errorf(
		"metrics.GetOTelConfig: the 9.x OpenTelemetry exporter is file-configured "+
			"and has no REST endpoint: %w", pverr.ErrUnsupported,
	)
}

// SetOTelConfig would write the node's OpenTelemetry exporter configuration.
// Same caveat as GetOTelConfig: no REST endpoint exists in 9.x, so it returns
// pverr.ErrUnsupported. Configure the exporter via its files meanwhile.
func (*Service) SetOTelConfig(_ context.Context, _ string, _ *OTelConfig) error {
	return fmt.Errorf(
		"metrics.SetOTelConfig: the 9.x OpenTelemetry exporter is file-configured "+
			"and has no REST endpoint: %w", pverr.ErrUnsupported,
	)
}
