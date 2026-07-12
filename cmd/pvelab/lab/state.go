package lab

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// StateSchemaVersion is written into every state file; LoadState rejects a
// file written by a newer pvelab (unknown future shape) and accepts older
// ones (unknown keys are ignored by encoding/json).
const StateSchemaVersion = 1

// The two git-ignored files `up` writes in the working directory
// (DESIGN-0002): what was created, and the inner suite's environment.
const (
	DefaultStatePath = ".pvelab-state.json"
	DefaultEnvPath   = ".pvelab.env"
)

// Phase 3 test-gate values baked into .pvelab.env (IQ-6 = a): the placement
// pair and console scratch VMID live in the reserved 93xx scratch block
// (design OQ-10), and the nested nodes' ext4 default install yields
// local-lvm as the scratch storage.
const (
	placementVMID1    = 9301
	placementVMID2    = 9302
	consoleVMID       = 9303
	nestedTestStorage = "local-lvm"
)

// State is the .pvelab-state.json schema: evidence of what `up` created,
// updated after every stage so a mid-`up` failure still leaves the truth on
// disk (design OQ-7). `down` deletes from config, not state — state feeds
// `status` and `env`.
type State struct {
	SchemaVersion int         `json:"schema_version"`
	CreatedAt     time.Time   `json:"created_at"`
	ClusterName   string      `json:"cluster_name,omitempty"`
	PVEVersion    string      `json:"pve_version,omitempty"`
	ISOVolid      string      `json:"iso_volid,omitempty"`
	Clustered     bool        `json:"clustered,omitempty"` // formation completed quorate.
	Nodes         []NodeState `json:"nodes,omitempty"`
}

// NodeState is one nested node's provisioning progress.
type NodeState struct {
	Name         string  `json:"name"`
	VMID         int     `json:"vmid"`
	CIDR         string  `json:"cidr"`
	Created      bool    `json:"created,omitempty"`
	Started      bool    `json:"started,omitempty"`
	Ready        bool    `json:"ready,omitempty"`
	ReadySeconds float64 `json:"ready_seconds,omitempty"`
}

// SeedNodes fills st.Nodes from the config topology, keeping any existing
// per-node progress (idempotent across UpdateState calls).
func (st *State) SeedNodes(nodes []Node) {
	for _, n := range nodes {
		if st.FindNode(n.Name) == nil {
			st.Nodes = append(st.Nodes, NodeState{Name: n.Name, VMID: n.VMID, CIDR: n.CIDR})
		}
	}
}

// FindNode returns the node's state entry, or nil.
func (st *State) FindNode(name string) *NodeState {
	for i := range st.Nodes {
		if st.Nodes[i].Name == name {
			return &st.Nodes[i]
		}
	}
	return nil
}

// ApplyReadiness records WaitReady's measurements.
func (st *State) ApplyReadiness(rs []NodeReadiness) {
	for _, r := range rs {
		if ns := st.FindNode(r.Node); ns != nil {
			ns.Ready = r.Ready
			ns.ReadySeconds = r.Elapsed.Seconds()
		}
	}
}

// ErrNoState reports a missing state file — normal before the first `up`;
// callers branch with errors.Is rather than failing.
var ErrNoState = errors.New("pvelab: no state file")

// LoadState reads the state file at path; a missing file surfaces ErrNoState.
func LoadState(path string) (*State, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- the harness's own state path.
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("%w at %s", ErrNoState, path)
	}
	if err != nil {
		return nil, fmt.Errorf("read state %s: %w", path, err)
	}
	var st State
	if err := json.Unmarshal(raw, &st); err != nil {
		return nil, fmt.Errorf("parse state %s: %w", path, err)
	}
	if st.SchemaVersion > StateSchemaVersion {
		return nil, fmt.Errorf("state %s has schema_version %d, newer than this pvelab understands (%d) — use a newer pvelab or delete the file",
			path, st.SchemaVersion, StateSchemaVersion)
	}
	return &st, nil
}

// WriteState writes st to path (0600: lab topology, not secret, but private).
func WriteState(path string, st *State) error {
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
		return fmt.Errorf("write state %s: %w", path, err)
	}
	return nil
}

// UpdateState loads (or initialises) the state at path, applies mutate, and
// writes it back — called after every `up` stage so failure evidence is
// already on disk.
func UpdateState(path string, mutate func(*State)) (*State, error) {
	st, err := LoadState(path)
	switch {
	case errors.Is(err, ErrNoState):
		st = &State{SchemaVersion: StateSchemaVersion, CreatedAt: time.Now().UTC()}
	case err != nil:
		return nil, err
	}
	mutate(st)
	if err := WriteState(path, st); err != nil {
		return nil, err
	}
	return st, nil
}

// EnvFile is the .pvelab.env content: the inner test suite's environment,
// pointing at the nested cluster with root@pam password credentials plus the
// Phase 3 test gates. It carries the root password — written 0600, never
// logged.
type EnvFile struct {
	Endpoint       string // https://<first node ip>:8006
	Username       string // root@pam
	Password       string
	InsecureTLS    bool
	Node           string // first node's name (the suite's PVE_NODE)
	TestStorage    string
	PlacementVMID1 int
	PlacementVMID2 int
	ConsoleVMID    int
}

// NewEnvFile derives the inner-suite environment from the config and the
// resolved root password. The first configured node is the suite's entry
// point.
func NewEnvFile(cfg *Config, rootPassword string) (*EnvFile, error) {
	if len(cfg.Nested.Nodes) == 0 {
		return nil, errors.New("config has no nodes")
	}
	first := cfg.Nested.Nodes[0]
	endpoint, err := nodeEndpoint(first)
	if err != nil {
		return nil, err
	}
	return &EnvFile{
		Endpoint:       endpoint,
		Username:       "root@pam",
		Password:       rootPassword,
		InsecureTLS:    true,
		Node:           first.Name,
		TestStorage:    nestedTestStorage,
		PlacementVMID1: placementVMID1,
		PlacementVMID2: placementVMID2,
		ConsoleVMID:    consoleVMID,
	}, nil
}

// RenderEnv formats e as sourceable `export KEY='VALUE'` lines (the
// dogfood-test recipe sources this file).
func RenderEnv(e *EnvFile) []byte {
	var b strings.Builder
	set := func(key, value string) {
		b.WriteString("export " + key + "=" + shellQuote(value) + "\n")
	}
	set("PVE_ENDPOINT", e.Endpoint)
	set("PVE_USERNAME", e.Username)
	set("PVE_PASSWORD", e.Password)
	if e.InsecureTLS {
		set("PVE_INSECURE_TLS", "1")
	}
	set("PVE_NODE", e.Node)
	set("PVE_TEST_STORAGE", e.TestStorage)
	set("PVE_TEST_PLACEMENT_VMID_1", strconv.Itoa(e.PlacementVMID1))
	set("PVE_TEST_PLACEMENT_VMID_2", strconv.Itoa(e.PlacementVMID2))
	set("PVE_TEST_CONSOLE_VMID", strconv.Itoa(e.ConsoleVMID))
	return []byte(b.String())
}

// WriteEnvFile writes RenderEnv(e) to path, 0600 (it carries a password).
func WriteEnvFile(path string, e *EnvFile) error {
	if err := os.WriteFile(path, RenderEnv(e), 0o600); err != nil {
		return fmt.Errorf("write env %s: %w", path, err)
	}
	return nil
}
