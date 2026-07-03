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

// DecodeExtra returns every key in data not present in known, decoded to a
// string (non-string values keep their raw JSON token). It is the shared tail
// of the typed read structs' UnmarshalJSON: decode the modelled fields via a
// method-stripped alias, then route the remaining keys here so a config read
// round-trips losslessly. The result is nil when no unmodelled keys are present.
func DecodeExtra(data []byte, known map[string]bool) (map[string]string, error) {
	var all map[string]json.RawMessage
	if err := json.Unmarshal(data, &all); err != nil {
		return nil, fmt.Errorf("decode object map: %w", err)
	}
	var extra map[string]string
	for key, raw := range all {
		if known[key] {
			continue
		}
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			s = string(raw) // non-string field: keep the raw token.
		}
		if extra == nil {
			extra = make(map[string]string)
		}
		extra[key] = s
	}
	return extra, nil
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

	// No capacity hint: flat/extra are tiny (a spec's field count), so
	// preallocation buys nothing, and summing two lengths trips CodeQL's
	// allocation-size-overflow rule (CWE-190) for no real-world benefit.
	vals := make(url.Values)
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
