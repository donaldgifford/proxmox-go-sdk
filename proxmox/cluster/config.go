package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/internal/svcutil"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/types"
)

// ClusterCreateSpec is the body of POST /cluster/config: the parameters for
// forming a new cluster on the responding node. Name is corosync's cluster
// name (required). Corosync links (link0, link1, …), an explicit nodeid, and
// votes go through Extra. Pass it by pointer.
type ClusterCreateSpec struct {
	Name string `json:"clustername"`
	// Extra carries PVE parameters the SDK does not model (link0…linkN,
	// nodeid, votes).
	Extra map[string]string `json:"-"`
}

// JoinNode is one nodelist entry from GET /cluster/config/join: an existing
// cluster member as advertised to a joining node. The wire shape is
// REST-with-caveat (unverified against live PVE), so only the string fields
// the join flow needs are modelled; everything else (nodeid, quorum_votes,
// ring addresses, …) is preserved in Extra.
type JoinNode struct {
	Name           string `json:"name,omitempty"`
	PVEAddr        string `json:"pve_addr,omitempty"`
	PVEFingerprint string `json:"pve_fp,omitempty"`
	// Extra carries nodelist keys the SDK does not model.
	Extra map[string]string `json:"-"`
}

var joinNodeKnownFields = map[string]bool{
	"name": true, "pve_addr": true, "pve_fp": true,
}

// UnmarshalJSON decodes the modelled fields and routes unknown keys into Extra.
func (n *JoinNode) UnmarshalJSON(data []byte) error {
	type alias JoinNode
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("decode join node: %w", err)
	}
	*n = JoinNode(a)
	extra, err := svcutil.DecodeExtra(data, joinNodeKnownFields)
	if err != nil {
		return fmt.Errorf("decode join node: %w", err)
	}
	n.Extra = extra
	return nil
}

// JoinInfo is the response of GET /cluster/config/join: what a joining node
// needs to know about the cluster — the member nodelist (with each member's
// certificate fingerprint), the preferred contact node, and the corosync
// config digest. Reads are lossless: unmodelled keys (totem, …) are preserved
// in Extra. Use Fingerprint for the value JoinSpec.Fingerprint expects.
type JoinInfo struct {
	PreferredNode string     `json:"preferred_node,omitempty"`
	ConfigDigest  string     `json:"config_digest,omitempty"`
	Nodelist      []JoinNode `json:"nodelist,omitempty"`
	// Extra carries join-info keys the SDK does not model (e.g. totem, kept
	// as its raw JSON token).
	Extra map[string]string `json:"-"`
}

var joinInfoKnownFields = map[string]bool{
	"preferred_node": true, "config_digest": true, "nodelist": true,
}

// UnmarshalJSON decodes the modelled fields and routes unknown keys into Extra.
func (i *JoinInfo) UnmarshalJSON(data []byte) error {
	type alias JoinInfo
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("decode join info: %w", err)
	}
	*i = JoinInfo(a)
	extra, err := svcutil.DecodeExtra(data, joinInfoKnownFields)
	if err != nil {
		return fmt.Errorf("decode join info: %w", err)
	}
	i.Extra = extra
	return nil
}

// Fingerprint returns the cluster certificate fingerprint a join must present:
// the preferred node's, falling back to the first nodelist entry's. Empty when
// the nodelist is empty. (The wire shape carries per-node pve_fp values inside
// nodelist, not a top-level fingerprint field.)
func (i *JoinInfo) Fingerprint() string {
	for _, n := range i.Nodelist {
		if n.Name == i.PreferredNode && n.PVEFingerprint != "" {
			return n.PVEFingerprint
		}
	}
	if len(i.Nodelist) > 0 {
		return i.Nodelist[0].PVEFingerprint
	}
	return ""
}

// JoinSpec is the body of POST /cluster/config/join, issued ON THE JOINING
// NODE: Hostname is an existing cluster member to contact (IP or resolvable
// name), Password is that member's root@pam password, and Fingerprint is the
// cluster certificate fingerprint from JoinInfo. Corosync links go through
// Extra (link0…linkN). Pass it by pointer.
type JoinSpec struct {
	Hostname    string         `json:"hostname"`
	Password    string         `json:"password"`
	Fingerprint string         `json:"fingerprint"`
	Force       *types.PVEBool `json:"force,omitempty"`
	// Extra carries PVE parameters the SDK does not model (link0…linkN,
	// nodeid, votes).
	Extra map[string]string `json:"-"`
}

// ConfigNode is one entry from GET /cluster/config/nodes — a member of the
// corosync nodelist. The wire shape is REST-with-caveat (unverified against
// live PVE; the key may be "node" or "name"), so both are modelled and
// NodeName returns whichever is set; the rest is preserved in Extra.
type ConfigNode struct {
	Node string `json:"node,omitempty"`
	Name string `json:"name,omitempty"`
	// Extra carries nodelist keys the SDK does not model (nodeid,
	// quorum_votes, ring addresses, …).
	Extra map[string]string `json:"-"`
}

var configNodeKnownFields = map[string]bool{
	"node": true, "name": true,
}

// UnmarshalJSON decodes the modelled fields and routes unknown keys into Extra.
func (n *ConfigNode) UnmarshalJSON(data []byte) error {
	type alias ConfigNode
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("decode config node: %w", err)
	}
	*n = ConfigNode(a)
	extra, err := svcutil.DecodeExtra(data, configNodeKnownFields)
	if err != nil {
		return fmt.Errorf("decode config node: %w", err)
	}
	n.Extra = extra
	return nil
}

// NodeName returns the member's node name, whichever wire key carried it.
func (n *ConfigNode) NodeName() string {
	if n.Node != "" {
		return n.Node
	}
	return n.Name
}

// CreateCluster forms a new cluster on the responding node
// (POST /cluster/config). The write is fire-and-poll: PVE's return shape is
// unverified (a UPID may or may not come back, and cluster formation restarts
// pmxcfs underneath it), so the response body is ignored beyond error status —
// poll ListConfigNodes until the node appears to observe convergence.
func (s *Service) CreateCluster(ctx context.Context, spec *ClusterCreateSpec) error {
	if spec == nil {
		return fmt.Errorf("cluster.CreateCluster: %w", svcutil.ErrNilSpec)
	}
	if spec.Name == "" {
		return fmt.Errorf("cluster.CreateCluster: Name: %w", svcutil.ErrMissingField)
	}
	body, err := svcutil.EncodeWithExtra(spec, spec.Extra)
	if err != nil {
		return fmt.Errorf("cluster.CreateCluster: %w", err)
	}
	if err := s.c.DoRequest(ctx, http.MethodPost, clusterConfigPath(), body, nil); err != nil {
		return fmt.Errorf("cluster.CreateCluster: %w", err)
	}
	return nil
}

// JoinInfo returns what a joining node needs to know about this node's
// cluster (GET /cluster/config/join): the member nodelist with certificate
// fingerprints, the preferred contact node, and the config digest. Call it on
// an existing cluster member; a standalone node errors.
func (s *Service) JoinInfo(ctx context.Context) (*JoinInfo, error) {
	var info JoinInfo
	if err := s.c.DoRequest(ctx, http.MethodGet, clusterConfigJoinPath(), nil, &info); err != nil {
		return nil, fmt.Errorf("cluster.JoinInfo: %w", err)
	}
	return &info, nil
}

// JoinCluster joins the responding node to an existing cluster
// (POST /cluster/config/join). Call it ON THE JOINING NODE, which must be
// FRESH: joining wipes the node's local pmxcfs config — its users and API
// tokens do not survive (root@pam, being a PAM account, does) — and restarts
// the API daemons mid-call, so the connection may drop before a response
// arrives. The write is therefore fire-and-poll: the response body is ignored
// beyond error status, a dropped connection is expected, and convergence is
// observed by polling an existing member's ListConfigNodes until the joined
// node appears.
func (s *Service) JoinCluster(ctx context.Context, spec *JoinSpec) error {
	if spec == nil {
		return fmt.Errorf("cluster.JoinCluster: %w", svcutil.ErrNilSpec)
	}
	if spec.Hostname == "" {
		return fmt.Errorf("cluster.JoinCluster: Hostname: %w", svcutil.ErrMissingField)
	}
	if spec.Password == "" {
		return fmt.Errorf("cluster.JoinCluster: Password: %w", svcutil.ErrMissingField)
	}
	if spec.Fingerprint == "" {
		return fmt.Errorf("cluster.JoinCluster: Fingerprint: %w", svcutil.ErrMissingField)
	}
	body, err := svcutil.EncodeWithExtra(spec, spec.Extra)
	if err != nil {
		return fmt.Errorf("cluster.JoinCluster: %w", err)
	}
	if err := s.c.DoRequest(ctx, http.MethodPost, clusterConfigJoinPath(), body, nil); err != nil {
		return fmt.Errorf("cluster.JoinCluster: %w", err)
	}
	return nil
}

// ListConfigNodes returns the corosync nodelist (GET /cluster/config/nodes) —
// the cluster membership as configured. It is the convergence signal for
// CreateCluster and JoinCluster: poll it until the expected node appears.
func (s *Service) ListConfigNodes(ctx context.Context) ([]ConfigNode, error) {
	var nodes []ConfigNode
	if err := s.c.DoRequest(ctx, http.MethodGet, clusterConfigNodesPath(), nil, &nodes); err != nil {
		return nil, fmt.Errorf("cluster.ListConfigNodes: %w", err)
	}
	return nodes, nil
}
