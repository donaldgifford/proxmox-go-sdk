package schema

import (
	"encoding/json"
	"fmt"
)

// MarshalBaseline renders an endpoint set as the indented JSON stored on disk as
// the committed baseline. The input is sorted first so the baseline is stable
// across runs (a clean diff when it changes).
func MarshalBaseline(eps []Endpoint) ([]byte, error) {
	sorted := make([]Endpoint, len(eps))
	copy(sorted, eps)
	sorted = sortSlice(sorted)
	out, err := json.MarshalIndent(sorted, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("schema: marshal baseline: %w", err)
	}
	return append(out, '\n'), nil
}

// UnmarshalBaseline parses a committed baseline file into an endpoint set.
func UnmarshalBaseline(data []byte) ([]Endpoint, error) {
	var eps []Endpoint
	if err := json.Unmarshal(data, &eps); err != nil {
		return nil, fmt.Errorf("schema: parse baseline: %w", err)
	}
	return eps, nil
}
