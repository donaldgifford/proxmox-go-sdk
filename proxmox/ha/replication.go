package ha

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/internal/svcutil"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/types"
)

// ReplicationJob is one entry from GET /cluster/replication or
// /cluster/replication/{id} — a storage/ZFS replication job that ships a guest's
// volumes to another node on a schedule. Reads are lossless: keys outside the
// typed set are preserved in Extra.
//
// Managing replication requires the 9.x VM.Replicate privilege on the guest.
type ReplicationJob struct {
	ID       string        `json:"id"`                 // "<vmid>-<jobnum>", e.g. "100-0".
	Type     string        `json:"type,omitempty"`     // "local" (the only 9.x type).
	Target   string        `json:"target,omitempty"`   // target node.
	Schedule string        `json:"schedule,omitempty"` // systemd-calendar schedule.
	Rate     float64       `json:"rate,omitempty"`     // rate limit, MB/s; 0 = unlimited.
	Disable  types.PVEBool `json:"disable,omitempty"`  // PVE stores the disabled flag.
	Comment  string        `json:"comment,omitempty"`
	// Extra carries replication keys the SDK does not model.
	Extra map[string]string `json:"-"`
}

// replJobKnownFields lists the JSON keys ReplicationJob models directly; keep it
// in sync with the struct so UnmarshalJSON routes only the rest into Extra.
var replJobKnownFields = map[string]bool{
	"id": true, "type": true, "target": true, "schedule": true,
	"rate": true, "disable": true, "comment": true,
}

// UnmarshalJSON decodes the modelled fields and routes any unknown keys into
// Extra so a replication-job read round-trips losslessly.
func (j *ReplicationJob) UnmarshalJSON(data []byte) error {
	type alias ReplicationJob
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("decode replication job: %w", err)
	}
	*j = ReplicationJob(a)

	var all map[string]json.RawMessage
	if err := json.Unmarshal(data, &all); err != nil {
		return fmt.Errorf("decode replication job map: %w", err)
	}
	for key, raw := range all {
		if replJobKnownFields[key] {
			continue
		}
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			s = string(raw)
		}
		if j.Extra == nil {
			j.Extra = make(map[string]string)
		}
		j.Extra[key] = s
	}
	return nil
}

// ReplicationSpec is the body of POST /cluster/replication. ID and Target are
// required; ID is "<vmid>-<jobnum>" (e.g. "100-0"). Type defaults to "local"
// (the only 9.x type) when empty. Pass it by pointer.
type ReplicationSpec struct {
	ID       string  `json:"id"`
	Target   string  `json:"target"`
	Type     string  `json:"type,omitempty"`
	Schedule string  `json:"schedule,omitempty"`
	Rate     float64 `json:"rate,omitempty"`
	Comment  string  `json:"comment,omitempty"`
	// Extra carries PVE parameters the SDK does not model.
	Extra map[string]string `json:"-"`
}

// ReplicationUpdate is the body of PUT /cluster/replication/{id}. All fields are
// optional; only the set ones are sent. Use Delete to unset keys. Pass it by
// pointer.
type ReplicationUpdate struct {
	Target   string         `json:"target,omitempty"`
	Schedule string         `json:"schedule,omitempty"`
	Rate     *float64       `json:"rate,omitempty"`
	Disable  *types.PVEBool `json:"disable,omitempty"`
	Comment  string         `json:"comment,omitempty"`
	Delete   string         `json:"delete,omitempty"`
	// Extra carries PVE parameters the SDK does not model.
	Extra map[string]string `json:"-"`
}

// ListReplicationJobs returns every replication job in the cluster.
func (s *Service) ListReplicationJobs(ctx context.Context) ([]ReplicationJob, error) {
	var jobs []ReplicationJob
	if err := s.c.DoRequest(ctx, http.MethodGet, replJobsPath(), nil, &jobs); err != nil {
		return nil, fmt.Errorf("ha.ListReplicationJobs: %w", err)
	}
	return jobs, nil
}

// GetReplicationJob returns one replication job by ID (e.g. "100-0").
func (s *Service) GetReplicationJob(ctx context.Context, id string) (*ReplicationJob, error) {
	var job ReplicationJob
	if err := s.c.DoRequest(ctx, http.MethodGet, replJobPath(id), nil, &job); err != nil {
		return nil, fmt.Errorf("ha.GetReplicationJob: %w", err)
	}
	return &job, nil
}

// CreateReplicationJob defines a new replication job. Requires the VM.Replicate
// privilege on the guest. The write is synchronous (no task).
func (s *Service) CreateReplicationJob(ctx context.Context, spec *ReplicationSpec) error {
	if spec == nil {
		return fmt.Errorf("ha.CreateReplicationJob: %w", svcutil.ErrNilSpec)
	}
	switch {
	case spec.ID == "":
		return fmt.Errorf("ha.CreateReplicationJob: id: %w", svcutil.ErrMissingField)
	case spec.Target == "":
		return fmt.Errorf("ha.CreateReplicationJob: target: %w", svcutil.ErrMissingField)
	}
	body, err := svcutil.EncodeWithExtra(spec, spec.Extra)
	if err != nil {
		return fmt.Errorf("ha.CreateReplicationJob: %w", err)
	}
	if err := s.c.DoRequest(ctx, http.MethodPost, replJobsPath(), body, nil); err != nil {
		return fmt.Errorf("ha.CreateReplicationJob: %w", err)
	}
	return nil
}

// UpdateReplicationJob changes a replication job (schedule, rate, enable/
// disable). The write is synchronous (no task).
func (s *Service) UpdateReplicationJob(ctx context.Context, id string, update *ReplicationUpdate) error {
	if update == nil {
		return fmt.Errorf("ha.UpdateReplicationJob: %w", svcutil.ErrNilSpec)
	}
	if id == "" {
		return fmt.Errorf("ha.UpdateReplicationJob: id: %w", svcutil.ErrMissingField)
	}
	body, err := svcutil.EncodeWithExtra(update, update.Extra)
	if err != nil {
		return fmt.Errorf("ha.UpdateReplicationJob: %w", err)
	}
	if err := s.c.DoRequest(ctx, http.MethodPut, replJobPath(id), body, nil); err != nil {
		return fmt.Errorf("ha.UpdateReplicationJob: %w", err)
	}
	return nil
}

// DeleteReplicationJob removes a replication job. The write is synchronous (no
// task).
func (s *Service) DeleteReplicationJob(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("ha.DeleteReplicationJob: id: %w", svcutil.ErrMissingField)
	}
	if err := s.c.DoRequest(ctx, http.MethodDelete, replJobPath(id), nil, nil); err != nil {
		return fmt.Errorf("ha.DeleteReplicationJob: %w", err)
	}
	return nil
}
