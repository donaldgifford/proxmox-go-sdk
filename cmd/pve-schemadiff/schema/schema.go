// Package schema parses a Proxmox VE apidoc.js API-schema dump into a flat set
// of (method, path) endpoints and diffs it against a stored baseline, so CI can
// flag when the 9.x REST surface drifts across minor releases (OQ-7 / IMPL-0001).
//
// It is deliberately transport-free: it reads bytes, so it can run against a
// committed apidoc.js fixture in unit tests and against a freshly fetched dump
// in CI without a live node.
package schema

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
)

// httpMethods is the set of info keys treated as endpoints; apidoc.js keys its
// per-path info map by HTTP verb.
var httpMethods = map[string]bool{
	"GET": true, "POST": true, "PUT": true, "DELETE": true,
}

// Endpoint is one (method, path) pair of the REST surface.
type Endpoint struct {
	Method string `json:"method"`
	Path   string `json:"path"`
}

// node mirrors the apidoc.js tree, keeping only the fields the diff needs.
type node struct {
	Path     string                     `json:"path"`
	Info     map[string]json.RawMessage `json:"info"`
	Children []node                     `json:"children"`
}

// Parse extracts the endpoint set from apidoc.js content. PVE ships the schema
// as a JavaScript assignment (e.g. `const apiSchema = [ … ];`) followed by the
// API-viewer application code, so the array's closing bracket is NOT the last
// ']' in the file. Parse decodes the first complete JSON value starting at the
// first '[' and ignores everything after it. The result is sorted and
// de-duplicated.
func Parse(apidocJS []byte) ([]Endpoint, error) {
	start := bytes.IndexByte(apidocJS, '[')
	if start < 0 {
		return nil, fmt.Errorf("schema: no JSON array found in apidoc.js")
	}
	var roots []node
	if err := json.NewDecoder(bytes.NewReader(apidocJS[start:])).Decode(&roots); err != nil {
		return nil, fmt.Errorf("schema: parse apidoc.js array: %w", err)
	}
	seen := make(map[string]Endpoint)
	for i := range roots {
		walk(&roots[i], seen)
	}
	return sortedEndpoints(seen), nil
}

// walk flattens a node subtree into the seen set, keyed to de-duplicate.
func walk(n *node, seen map[string]Endpoint) {
	if n.Path != "" {
		for method := range n.Info {
			if !httpMethods[method] {
				continue
			}
			ep := Endpoint{Method: method, Path: n.Path}
			seen[key(ep)] = ep
		}
	}
	for i := range n.Children {
		walk(&n.Children[i], seen)
	}
}

// Report is the outcome of a Diff: endpoints present in the new schema but not
// the baseline (Added), and in the baseline but gone from the new schema
// (Removed).
type Report struct {
	Added   []Endpoint
	Removed []Endpoint
}

// Empty reports whether the two schemas cover the same endpoint set.
func (r Report) Empty() bool { return len(r.Added) == 0 && len(r.Removed) == 0 }

// Diff compares a new schema against a baseline and reports the drift.
func Diff(baseline, current []Endpoint) Report {
	base := index(baseline)
	cur := index(current)
	var rep Report
	for k, ep := range cur {
		if _, ok := base[k]; !ok {
			rep.Added = append(rep.Added, ep)
		}
	}
	for k, ep := range base {
		if _, ok := cur[k]; !ok {
			rep.Removed = append(rep.Removed, ep)
		}
	}
	rep.Added = sortSlice(rep.Added)
	rep.Removed = sortSlice(rep.Removed)
	return rep
}

// index keys a slice by "METHOD path" for set membership.
func index(eps []Endpoint) map[string]Endpoint {
	m := make(map[string]Endpoint, len(eps))
	for _, ep := range eps {
		m[key(ep)] = ep
	}
	return m
}

func key(ep Endpoint) string { return ep.Method + " " + ep.Path }

func sortedEndpoints(m map[string]Endpoint) []Endpoint {
	out := make([]Endpoint, 0, len(m))
	for _, ep := range m {
		out = append(out, ep)
	}
	return sortSlice(out)
}

func sortSlice(eps []Endpoint) []Endpoint {
	sort.Slice(eps, func(i, j int) bool {
		if eps[i].Path != eps[j].Path {
			return eps[i].Path < eps[j].Path
		}
		return eps[i].Method < eps[j].Method
	})
	return eps
}
