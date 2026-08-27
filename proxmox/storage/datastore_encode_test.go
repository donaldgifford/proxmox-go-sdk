package storage

import (
	"testing"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/types"
)

// pveBool returns a pointer for DatastoreUpdate's tri-state booleans.
func pveBool(b bool) *types.PVEBool {
	v := types.PVEBool(b)
	return &v
}

// TestEncodeDatastoreSpec pins the create wire form byte-for-byte
// (url.Values.Encode sorts keys, so the strings are canonical). The first
// case is the hoomlab zfspool shape — issue #28's consumer — verbatim.
func TestEncodeDatastoreSpec(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		spec *DatastoreSpec
		want string
	}{
		{
			name: "hoomlab zfspool shape",
			spec: &DatastoreSpec{
				Storage:   "fast-vm",
				Type:      "zfspool",
				Pool:      "fast/vm",
				Sparse:    true,
				Blocksize: "16k",
				Content:   []string{"images", "rootdir"},
				Nodes:     []string{"pve1", "pve2"},
			},
			want: "blocksize=16k&content=images%2Crootdir&nodes=pve1%2Cpve2" +
				"&pool=fast%2Fvm&sparse=1&storage=fast-vm&type=zfspool",
		},
		{
			name: "minimal dir entry, false booleans omitted",
			spec: &DatastoreSpec{Storage: "scratch", Type: "dir", Path: "/srv/scratch"},
			want: "path=%2Fsrv%2Fscratch&storage=scratch&type=dir",
		},
		{
			name: "extra key wins over its typed field",
			spec: &DatastoreSpec{
				Storage: "tank-vm", Type: "zfspool", Pool: "typed/loses",
				Extra: map[string]string{"pool": "tank/vm", "preallocation": "metadata"},
			},
			want: "pool=tank%2Fvm&preallocation=metadata&storage=tank-vm&type=zfspool",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			body, err := encodeDatastoreSpec(tt.spec)
			if err != nil {
				t.Fatalf("encodeDatastoreSpec: %v", err)
			}
			if got := body.Encode(); got != tt.want {
				t.Errorf("wire form:\n got  %s\n want %s", got, tt.want)
			}
		})
	}
}

// TestEncodeDatastoreUpdate pins update semantics: the zero update sends
// nothing, unset pointer booleans are absent while false-and-set renders as
// 0, and delete + digest ride the wire as PVE's params.
func TestEncodeDatastoreUpdate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		update *DatastoreUpdate
		want   string
	}{
		{
			name:   "zero update sends nothing",
			update: &DatastoreUpdate{},
			want:   "",
		},
		{
			name: "only set fields go on the wire",
			update: &DatastoreUpdate{
				Content: []string{"images"},
				Disable: pveBool(false), // set-to-false renders; Sparse stays absent
			},
			want: "content=images&disable=0",
		},
		{
			name: "delete and digest render",
			update: &DatastoreUpdate{
				Delete: []string{"nodes", "blocksize"},
				Digest: "921a2c39e40935cc1d681235282a3f4359c66196",
			},
			want: "delete=nodes%2Cblocksize" +
				"&digest=921a2c39e40935cc1d681235282a3f4359c66196",
		},
		{
			name: "drift-correct with guard",
			update: &DatastoreUpdate{
				Content: []string{"images", "rootdir"},
				Nodes:   []string{"pve1"},
				Sparse:  pveBool(true),
				Digest:  "abc123",
			},
			want: "content=images%2Crootdir&digest=abc123&nodes=pve1&sparse=1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			body, err := encodeDatastoreUpdate(tt.update)
			if err != nil {
				t.Fatalf("encodeDatastoreUpdate: %v", err)
			}
			if got := body.Encode(); got != tt.want {
				t.Errorf("wire form:\n got  %s\n want %s", got, tt.want)
			}
		})
	}
}
