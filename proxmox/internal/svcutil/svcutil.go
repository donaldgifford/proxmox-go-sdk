// Package svcutil holds unexported helpers shared by the typed service packages
// (qemu, lxc, storage, …). It is internal to proxmox/ and not part of the SDK's
// public surface; consumers must not depend on it.
package svcutil

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/tasks"
)

// Shared spec-validation sentinels. Services wrap these with their op-qualified
// prefix (e.g. "qemu.Create: %w"), so the message stays succinct.
var (
	// ErrNilSpec is wrapped when a write operation is called with a nil spec.
	ErrNilSpec = errors.New("nil spec")
	// ErrMissingField is wrapped when a required spec field is empty.
	ErrMissingField = errors.New("missing required field")
)

// TaskRef parses the UPID PVE returns for a task-starting operation into a
// tasks.Ref, tagging any parse failure with the calling op (e.g. "qemu.Create").
func TaskRef(op, upid string) (tasks.Ref, error) {
	ref, err := tasks.NewRef(upid)
	if err != nil {
		return tasks.Ref{}, fmt.Errorf("%s: %w", op, err)
	}
	return ref, nil
}

// EncodeWithExtra flattens a JSON-tagged spec to url.Values, then merges the
// caller's Extra params on top. The transport accepts url.Values directly, so
// this is how a typed spec and its unmodelled-key escape hatch reach one form
// body. PVE request bodies are flat, so the spec must encode to a JSON object.
func EncodeWithExtra(spec any, extra map[string]string) (url.Values, error) {
	data, err := json.Marshal(spec)
	if err != nil {
		return nil, fmt.Errorf("encode spec: %w", err)
	}
	var flat map[string]json.RawMessage
	if err := json.Unmarshal(data, &flat); err != nil {
		return nil, fmt.Errorf("spec must encode to a JSON object: %w", err)
	}

	vals := make(url.Values, len(flat)+len(extra))
	for key, raw := range flat {
		s := string(raw)
		if s != "" && s[0] == '"' {
			var str string
			if err := json.Unmarshal(raw, &str); err != nil {
				return nil, fmt.Errorf("decode spec field %q: %w", key, err)
			}
			s = str
		}
		vals.Set(key, s)
	}
	for key, val := range extra {
		vals.Set(key, val)
	}
	return vals, nil
}
