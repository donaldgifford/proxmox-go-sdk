package ha

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/internal/svcutil"
)

// BlockingCause classifies why an HA rule blocked a migrate/relocate request
// (the cause field of BlockingResource).
type BlockingCause string

const (
	// BlockingCauseNodeAffinity — a node-affinity rule pins the resource away
	// from the requested node.
	BlockingCauseNodeAffinity BlockingCause = "node-affinity"
	// BlockingCauseResourceAffinity — a resource-affinity rule ties the
	// resource to (or apart from) another resource, conflicting with the
	// requested placement.
	BlockingCauseResourceAffinity BlockingCause = "resource-affinity"
)

// BlockingResource identifies one resource whose HA rule blocked a
// migrate/relocate request. Lossless: unknown keys are preserved in Extra.
type BlockingResource struct {
	SID   string        `json:"sid,omitempty"`
	Cause BlockingCause `json:"cause,omitempty"`
	// Extra carries fields the SDK does not model.
	Extra map[string]string `json:"-"`
}

// blockingResourceKnownFields lists the JSON keys BlockingResource models
// directly; keep it in sync with the struct.
var blockingResourceKnownFields = map[string]bool{"sid": true, "cause": true}

// UnmarshalJSON decodes the modelled fields and routes any unknown keys into
// Extra so the read round-trips losslessly.
func (b *BlockingResource) UnmarshalJSON(data []byte) error {
	type alias BlockingResource
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("decode blocking resource: %w", err)
	}
	*b = BlockingResource(a)
	extra, err := svcutil.DecodeExtra(data, blockingResourceKnownFields)
	if err != nil {
		return fmt.Errorf("decode blocking resource: %w", err)
	}
	b.Extra = extra
	return nil
}

// MigrateResult is the synchronous response of an HA migrate/relocate
// request: what was asked, and — when HA rules constrain the answer — which
// resources block it or must move with it. An accepted request is a CRM
// intent, not a completed migration; observe convergence via HAStatusCurrent
// (the service row's Node). The affinity-aware body is 9.2-observed and
// hedged by the lossless decode. Lossless: unknown keys are preserved in
// Extra.
type MigrateResult struct {
	SID           string `json:"sid,omitempty"`
	RequestedNode string `json:"requested-node,omitempty"`
	// BlockingResources is set when an HA rule refuses the placement.
	BlockingResources []BlockingResource `json:"blocking-resources,omitempty"`
	// ComigratedResources lists resources a positive resource-affinity rule
	// drags along with this one.
	ComigratedResources []string `json:"comigrated-resources,omitempty"`
	// Extra carries fields the SDK does not model.
	Extra map[string]string `json:"-"`
}

// migrateResultKnownFields lists the JSON keys MigrateResult models directly;
// keep it in sync with the struct.
var migrateResultKnownFields = map[string]bool{
	"sid": true, "requested-node": true,
	"blocking-resources": true, "comigrated-resources": true,
}

// UnmarshalJSON decodes the modelled fields and routes any unknown keys into
// Extra so the read round-trips losslessly.
func (m *MigrateResult) UnmarshalJSON(data []byte) error {
	type alias MigrateResult
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("decode migrate result: %w", err)
	}
	*m = MigrateResult(a)
	extra, err := svcutil.DecodeExtra(data, migrateResultKnownFields)
	if err != nil {
		return fmt.Errorf("decode migrate result: %w", err)
	}
	m.Extra = extra
	return nil
}

// resourceAction posts one CRM placement request (migrate or relocate) and
// decodes the shared MigrateResult body.
func (s *Service) resourceAction(ctx context.Context, op, path, sid, node string) (*MigrateResult, error) {
	if sid == "" {
		return nil, fmt.Errorf("%s: sid: %w", op, svcutil.ErrMissingField)
	}
	if node == "" {
		return nil, fmt.Errorf("%s: node: %w", op, svcutil.ErrMissingField)
	}
	body := url.Values{"node": {node}}
	var res MigrateResult
	if err := s.c.DoRequest(ctx, http.MethodPost, path, body, &res); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	return &res, nil
}

// MigrateResource asks the CRM to live-migrate an HA-managed resource to
// node (POST /cluster/ha/resources/{sid}/migrate — synchronous, no task; the
// guest keeps running). The result reports the accepted request and any
// rule conflicts; nil error does not mean the resource has moved yet — poll
// HAStatusCurrent until the service row's Node is the target. No version
// gate (baseline endpoint). Contrast RelocateResource, which stops and
// restarts the guest on the target instead of live-migrating.
func (s *Service) MigrateResource(ctx context.Context, sid, node string) (*MigrateResult, error) {
	return s.resourceAction(ctx, "ha.MigrateResource", haResourceMigratePath(sid), sid, node)
}

// RelocateResource asks the CRM to move an HA-managed resource to node by
// stop + restart on the target (POST /cluster/ha/resources/{sid}/relocate —
// synchronous, no task). Semantics otherwise match MigrateResource: the
// result is an accepted CRM intent, observed to convergence via
// HAStatusCurrent.
func (s *Service) RelocateResource(ctx context.Context, sid, node string) (*MigrateResult, error) {
	return s.resourceAction(ctx, "ha.RelocateResource", haResourceRelocatePath(sid), sid, node)
}
