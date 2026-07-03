package lxc

import (
	"context"
	"fmt"
	"net/http"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/internal/svcutil"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/tasks"
)

// OCITemplateSpec describes an OCI image to pull into a storage as an LXC
// template. Storage, Reference, and Filename are required; pass the spec to
// PullOCITemplate by pointer.
//
// Reference is an OCI image reference understood by PVE's downloader (for
// example "docker://library/alpine:3.20"). The image is fetched into the
// storage's vztmpl content as Filename; the resulting volume id
// ("<storage>:vztmpl/<filename>") is then usable as CreateSpec.OSTemplate.
type OCITemplateSpec struct {
	// Storage is the target storage that holds vztmpl content. It is a path
	// segment, not a form field.
	Storage string `json:"-"`
	// Reference is the OCI image reference to pull (PVE's download "url").
	Reference string `json:"url"`
	// Filename is the template filename to store the pulled image as.
	Filename string `json:"filename"`
	// Extra carries PVE parameters the SDK does not model (e.g. checksum,
	// verify-certificates).
	Extra map[string]string `json:"-"`
}

// PullOCITemplate pulls an OCI image into spec.Storage as an LXC template and
// returns the download task. Await it with the client's task service; the
// resulting vztmpl volume id is usable as CreateSpec.OSTemplate.
//
// OCI-based container templates are a PVE 9.1 tech-preview: the call is gated on
// version.Capabilities.OCITemplates and returns a pverr.ErrUnsupported-wrapped
// error on older nodes. Under the hood it drives the node's storage
// download-url endpoint with vztmpl content; the generic storage surface lands
// with the storage service.
func (s *Service) PullOCITemplate(ctx context.Context, spec *OCITemplateSpec) (tasks.Ref, error) {
	if spec == nil {
		return tasks.Ref{}, fmt.Errorf("lxc.PullOCITemplate: %w", svcutil.ErrNilSpec)
	}
	if err := s.caps.Require("LXC OCI templates", "9.1"); err != nil {
		return tasks.Ref{}, fmt.Errorf("lxc.PullOCITemplate: %w", err)
	}
	switch {
	case spec.Storage == "":
		return tasks.Ref{}, fmt.Errorf("lxc.PullOCITemplate: storage: %w", svcutil.ErrMissingField)
	case spec.Reference == "":
		return tasks.Ref{}, fmt.Errorf("lxc.PullOCITemplate: reference: %w", svcutil.ErrMissingField)
	case spec.Filename == "":
		return tasks.Ref{}, fmt.Errorf("lxc.PullOCITemplate: filename: %w", svcutil.ErrMissingField)
	}
	body, err := svcutil.EncodeWithExtra(spec, spec.Extra)
	if err != nil {
		return tasks.Ref{}, fmt.Errorf("lxc.PullOCITemplate: %w", err)
	}
	body.Set("content", "vztmpl")
	path := "/nodes/" + s.node + "/storage/" + spec.Storage + "/download-url"
	var upid string
	if derr := s.c.DoRequest(ctx, http.MethodPost, path, body, &upid); derr != nil {
		return tasks.Ref{}, fmt.Errorf("lxc.PullOCITemplate: %w", derr)
	}
	return svcutil.TaskRef("lxc.PullOCITemplate", upid)
}
