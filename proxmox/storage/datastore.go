package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/types"
)

// ListDatastores returns the cluster's storage configuration (GET /storage).
func (s *Service) ListDatastores(ctx context.Context) ([]Datastore, error) {
	var ds []Datastore
	if err := s.c.DoRequest(ctx, http.MethodGet, datastoresPath(), nil, &ds); err != nil {
		return nil, fmt.Errorf("storage.ListDatastores: %w", err)
	}
	return ds, nil
}

// GetDatastore returns the configuration of one storage (GET /storage/{storage}).
func (s *Service) GetDatastore(ctx context.Context, storage string) (*Datastore, error) {
	var d Datastore
	if err := s.c.DoRequest(ctx, http.MethodGet, datastorePath(storage), nil, &d); err != nil {
		return nil, fmt.Errorf("storage.GetDatastore: %w", err)
	}
	return &d, nil
}

// DatastoreSpec creates a storage entry (CreateDatastore). Storage and Type
// are required; which of the remaining fields apply depends on Type (PVE
// validates server-side, so an inapplicable field is the server's error to
// raise, not silently dropped). Unmodelled per-type parameters ride Extra
// verbatim.
//
// Content, Nodes and the mock's read-back treat list-valued options as SETS:
// PVE does not preserve submission order, so compare them as sets, never as
// strings.
type DatastoreSpec struct {
	Storage string `json:"storage"` // unique storage ID (pve-storage-id).
	Type    string `json:"type"`    // "dir", "zfspool", "nfs", … — fixed for the entry's lifetime.

	Content []string `json:"-"` // allowed content types ("images", "rootdir", "iso", …); comma-joined.
	Nodes   []string `json:"-"` // node restriction; comma-joined; empty = all nodes.

	Path   string `json:"path,omitempty"`   // dir/btrfs backing path (create-fixed).
	Pool   string `json:"pool,omitempty"`   // zfspool dataset / RBD pool.
	Server string `json:"server,omitempty"` // nfs/cifs server.
	Export string `json:"export,omitempty"` // NFS export (create-fixed).
	Share  string `json:"share,omitempty"`  // CIFS share (create-fixed).

	Blocksize string        `json:"blocksize,omitempty"` // zfspool volblocksize, e.g. "16k".
	Sparse    types.PVEBool `json:"sparse,omitempty"`    // zfspool thin provisioning.
	Shared    types.PVEBool `json:"shared,omitempty"`
	Disable   types.PVEBool `json:"disable,omitempty"`

	// Extra carries parameters the SDK does not model ("preallocation",
	// "krbd", "monhost", "username", …); keys here win over typed fields.
	// Some storage types put credentials here (a CIFS or PBS "password"):
	// the SDK never logs request bodies, but whatever the consumer prints
	// of a spec is the consumer's own responsibility.
	Extra map[string]string `json:"-"`
}

// DatastoreUpdate changes an entry (UpdateDatastore). The zero value sends
// nothing: only set fields go on the wire, so an update cannot accidentally
// reset a key it did not name. Booleans are pointers because false-and-set
// and unset must differ on a partial write.
//
// Identity and backing location are fixed at creation — PVE's update schema
// has no type, path, export, share, target, portal, vgname, thinpool, base,
// datastore, iscsiprovider or authsupported parameters, so changing those
// means delete and recreate. To CLEAR a key (lift a nodes restriction, drop
// a content type list), name it in Delete; an empty typed field is "not
// sent", never "unset".
type DatastoreUpdate struct {
	Content []string `json:"-"`
	Nodes   []string `json:"-"`

	Pool      string         `json:"pool,omitempty"`
	Blocksize string         `json:"blocksize,omitempty"`
	Sparse    *types.PVEBool `json:"sparse,omitempty"`
	Shared    *types.PVEBool `json:"shared,omitempty"`
	Disable   *types.PVEBool `json:"disable,omitempty"`

	// Delete names settings to unset. It is CSV-joined into PVE's delete
	// parameter — clearing a key is a named action, never an empty-string
	// side effect.
	Delete []string `json:"-"`
	// Digest is the config digest from the read that informed this update.
	// When set, PVE refuses the write if the storage config changed since;
	// pass Datastore.Digest to make read-modify-write safe.
	Digest string `json:"digest,omitempty"`

	// Extra carries unmodelled parameters; see DatastoreSpec.Extra for the
	// credentials note.
	Extra map[string]string `json:"-"`
}

// DatastoreWriteResult is the response of a datastore create or update: the
// entry's id and type, plus any configuration PVE generated server-side.
type DatastoreWriteResult struct {
	Storage string `json:"storage"`
	Type    string `json:"type"`
	// Config carries server-generated properties. The schema names one:
	// "encryption-key", auto-generated when a PBS datastore is created with
	// encryption-key=autogen. It is returned HERE and not again — a caller
	// that discards it has lost the key material.
	Config map[string]string `json:"config,omitempty"`
}

// UnmarshalJSON decodes the result, keeping unexpected non-string config
// values as their raw tokens (the same tolerance as Extra reads) — the
// config member is declared open-ended (additionalProperties), so a new
// numeric or boolean property must not fail the write that succeeded.
func (r *DatastoreWriteResult) UnmarshalJSON(data []byte) error {
	var raw struct {
		Storage string                     `json:"storage"`
		Type    string                     `json:"type"`
		Config  map[string]json.RawMessage `json:"config"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("decode datastore write result: %w", err)
	}
	r.Storage, r.Type, r.Config = raw.Storage, raw.Type, nil
	for key, rawVal := range raw.Config {
		var s string
		if err := json.Unmarshal(rawVal, &s); err != nil {
			s = string(rawVal) // non-string value: keep the raw token.
		}
		if r.Config == nil {
			r.Config = make(map[string]string)
		}
		r.Config[key] = s
	}
	return nil
}

// ListNodeStorage returns the activation and usage status of every storage
// visible from node (GET /nodes/{node}/storage).
func (s *Service) ListNodeStorage(ctx context.Context, node string) ([]StorageStatus, error) {
	var st []StorageStatus
	if err := s.c.DoRequest(ctx, http.MethodGet, nodeStoragesPath(node), nil, &st); err != nil {
		return nil, fmt.Errorf("storage.ListNodeStorage: %w", err)
	}
	return st, nil
}

// NodeStorageStatus returns the usage status of one storage on node
// (GET /nodes/{node}/storage/{storage}/status).
func (s *Service) NodeStorageStatus(ctx context.Context, node, storage string) (*StorageStatus, error) {
	var st StorageStatus
	if err := s.c.DoRequest(ctx, http.MethodGet, nodeStoragePath(node, storage)+"/status", nil, &st); err != nil {
		return nil, fmt.Errorf("storage.NodeStorageStatus: %w", err)
	}
	return &st, nil
}
