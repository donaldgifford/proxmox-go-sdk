package nodes

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/internal/svcutil"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/tasks"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/types"
)

// AptUpdate is one pending package update from GET /nodes/{node}/apt/update.
// Reads are lossless: keys outside the typed set land in Extra.
type AptUpdate struct {
	Package     string `json:"Package"`
	Title       string `json:"Title,omitempty"`
	Version     string `json:"Version,omitempty"`    // candidate version.
	OldVersion  string `json:"OldVersion,omitempty"` // currently installed.
	Arch        string `json:"Arch,omitempty"`
	Priority    string `json:"Priority,omitempty"`
	Section     string `json:"Section,omitempty"`
	Origin      string `json:"Origin,omitempty"`
	Description string `json:"Description,omitempty"`
	// Extra carries update keys the SDK does not model.
	Extra map[string]string `json:"-"`
}

var aptUpdateKnownFields = map[string]bool{
	"Package": true, "Title": true, "Version": true, "OldVersion": true,
	"Arch": true, "Priority": true, "Section": true, "Origin": true,
	"Description": true,
}

// UnmarshalJSON decodes the modelled fields and routes unknown keys into Extra.
func (u *AptUpdate) UnmarshalJSON(data []byte) error {
	type alias AptUpdate
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("decode apt update: %w", err)
	}
	*u = AptUpdate(a)
	extra, err := svcutil.DecodeExtra(data, aptUpdateKnownFields)
	if err != nil {
		return fmt.Errorf("decode apt update: %w", err)
	}
	u.Extra = extra
	return nil
}

// ListAptUpdates returns the packages with pending updates on node (the cached
// apt state; call RefreshAptCache first to re-read the repositories).
func (s *Service) ListAptUpdates(ctx context.Context, node string) ([]AptUpdate, error) {
	var updates []AptUpdate
	if err := s.c.DoRequest(ctx, http.MethodGet, nodeAptUpdatePath(node), nil, &updates); err != nil {
		return nil, fmt.Errorf("nodes.ListAptUpdates: %w", err)
	}
	return updates, nil
}

// RefreshAptCache triggers `apt update` on node (POST /nodes/{node}/apt/update),
// re-reading the configured repositories. It runs as a worker; the returned
// tasks.Ref is awaited for completion.
func (s *Service) RefreshAptCache(ctx context.Context, node string) (tasks.Ref, error) {
	var upid string
	if err := s.c.DoRequest(ctx, http.MethodPost, nodeAptUpdatePath(node), nil, &upid); err != nil {
		return tasks.Ref{}, fmt.Errorf("nodes.RefreshAptCache: %w", err)
	}
	return svcutil.TaskRef("nodes.RefreshAptCache", upid)
}

// Repository is one configured apt repository entry, as returned inside a
// RepositoryFile by GET /nodes/{node}/apt/repositories. PVE 9.x favours the
// DEB822 (.sources) format, so Types/URIs/Suites/Components are lists. Reads are
// lossless.
type Repository struct {
	Types      []string `json:"Types,omitempty"`
	URIs       []string `json:"URIs,omitempty"`
	Suites     []string `json:"Suites,omitempty"`
	Components []string `json:"Components,omitempty"`
	Enabled    int      `json:"Enabled,omitempty"` // 1 = enabled (PVE emits 0/1 as a number here).
	Comment    string   `json:"Comment,omitempty"`
	FileType   string   `json:"FileType,omitempty"`
	// Extra carries repository keys the SDK does not model.
	Extra map[string]string `json:"-"`
}

// RepositoryFile is one apt source file (a `.list` or `.sources`) and the
// repositories it declares, from GET /nodes/{node}/apt/repositories.
type RepositoryFile struct {
	Path         string       `json:"path"`
	FileType     string       `json:"file-type,omitempty"`
	Repositories []Repository `json:"repositories,omitempty"`
	// Extra carries file keys the SDK does not model.
	Extra map[string]string `json:"-"`
}

// Repositories is the payload of GET /nodes/{node}/apt/repositories: the source
// files, plus the standard-repo catalogue and any parse errors. Reads are
// lossless.
//
// The DEB822 field shapes here are provisional (REST-with-caveat): the path is a
// real 9.x endpoint, but the exact key set was not confirmed against a live
// node. Unmodelled keys are preserved in Extra so nothing is lost meanwhile.
type Repositories struct {
	Files  []RepositoryFile `json:"files,omitempty"`
	Errors []string         `json:"errors,omitempty"`
	Digest string           `json:"digest,omitempty"`
	// Extra carries top-level keys the SDK does not model (e.g. "standard-repos").
	Extra map[string]string `json:"-"`
}

var repositoriesKnownFields = map[string]bool{
	"files": true, "errors": true, "digest": true,
}

// UnmarshalJSON decodes the modelled fields and routes unknown keys into Extra.
func (r *Repositories) UnmarshalJSON(data []byte) error {
	type alias Repositories
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("decode apt repositories: %w", err)
	}
	*r = Repositories(a)
	extra, err := svcutil.DecodeExtra(data, repositoriesKnownFields)
	if err != nil {
		return fmt.Errorf("decode apt repositories: %w", err)
	}
	r.Extra = extra
	return nil
}

// RepositoryUpdate is the body of POST /nodes/{node}/apt/repositories, toggling
// one repository (identified by its source file Path and Index within that file)
// on or off. The write is synchronous (no task). Pass it by pointer.
//
// This is REST-with-caveat: the endpoint is real, but the DEB822 field names are
// provisional pending live confirmation.
type RepositoryUpdate struct {
	Path    string         `json:"path"`
	Index   int            `json:"index"`
	Enabled *types.PVEBool `json:"enabled,omitempty"`
	Digest  string         `json:"digest,omitempty"`
	// Extra carries PVE parameters the SDK does not model.
	Extra map[string]string `json:"-"`
}

// ListRepositories returns node's configured apt source files. See the
// Repositories doc-comment for the REST-with-caveat status of the field shapes.
func (s *Service) ListRepositories(ctx context.Context, node string) (*Repositories, error) {
	var repos Repositories
	if err := s.c.DoRequest(ctx, http.MethodGet, nodeAptRepoPath(node), nil, &repos); err != nil {
		return nil, fmt.Errorf("nodes.ListRepositories: %w", err)
	}
	return &repos, nil
}

// UpdateRepository enables or disables one repository in a source file. The
// write is synchronous (no task). Path is required.
func (s *Service) UpdateRepository(ctx context.Context, node string, update *RepositoryUpdate) error {
	if update == nil {
		return fmt.Errorf("nodes.UpdateRepository: %w", svcutil.ErrNilSpec)
	}
	if update.Path == "" {
		return fmt.Errorf("nodes.UpdateRepository: path: %w", svcutil.ErrMissingField)
	}
	body, err := svcutil.EncodeWithExtra(update, update.Extra)
	if err != nil {
		return fmt.Errorf("nodes.UpdateRepository: %w", err)
	}
	if err := s.c.DoRequest(ctx, http.MethodPost, nodeAptRepoPath(node), body, nil); err != nil {
		return fmt.Errorf("nodes.UpdateRepository: %w", err)
	}
	return nil
}
