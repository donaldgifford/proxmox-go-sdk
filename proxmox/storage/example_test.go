package storage_test

import (
	"context"
	"fmt"
	"strings"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/api"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/mockpve"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/storage"
)

// Example uploads an ISO, allocates a disk volume, then frees it — awaiting each
// PVE task. It is the Phase 3 storage flow end to end.
//
// Note there is no volume-snapshot step: PVE exposes no storage-level snapshot
// REST endpoint (storage.VolumeSnapshots returns ErrUnsupported). A volume is
// snapshotted through its owning guest — qemu.CreateSnapshot / lxc.CreateSnapshot
// — which builds the 9.1 volume chain underneath on supported storage.
func Example() {
	// mockpve stands in for a live cluster so the example is self-contained;
	// against a real node, pass its URL and a real token to proxmox.NewClient.
	mock := mockpve.New()
	mock.SeedVersion("9.2.0", "9.2", "mockpve")
	ts := mock.Serve()
	defer ts.Close()

	ctx := context.Background()
	c, err := proxmox.NewClient(ctx, ts.URL, api.TokenCredentials("root@pam!sdk", "secret"))
	if err != nil {
		fmt.Println("connect:", err)
		return
	}
	s := c.Storage()

	// Upload an ISO to the node's "local" storage and await the import task.
	ref, err := s.UploadISO(ctx, "pve", "local", &storage.UploadSpec{
		Filename: "debian-13.iso",
		Reader:   strings.NewReader("FAKE-ISO-BYTES"),
	})
	if err != nil {
		fmt.Println("upload:", err)
		return
	}
	if _, err := c.Tasks().Wait(ctx, ref); err != nil {
		fmt.Println("await upload:", err)
		return
	}

	// Allocate a disk volume (allocation is synchronous → volid).
	volid, err := s.CreateVolume(ctx, "pve", "local", &storage.VolumeCreateSpec{
		Filename: "vm-100-disk-0.qcow2",
		Size:     "8G",
		Format:   "qcow2",
		VMID:     100,
	})
	if err != nil {
		fmt.Println("create volume:", err)
		return
	}
	fmt.Println(volid)

	// Clean up: free the volume and await the deletion task.
	ref, err = s.DeleteVolume(ctx, "pve", "local", volid)
	if err != nil {
		fmt.Println("delete volume:", err)
		return
	}
	if _, err := c.Tasks().Wait(ctx, ref); err != nil {
		fmt.Println("await delete volume:", err)
		return
	}
	// Output:
	// local:vm-100-disk-0.qcow2
}

// Example_datastoreConfig manages a cluster storage entry end to end: create a
// zfspool datastore over an existing dataset, read it back, narrow its content
// restriction with a digest-guarded update, and remove the entry. All four
// operations are synchronous cluster-config edits (no tasks), and deleting the
// entry removes CONFIG only — the backing dataset survives.
func Example_datastoreConfig() {
	mock := mockpve.New()
	mock.SeedVersion("9.2.0", "9.2", "mockpve")
	ts := mock.Serve()
	defer ts.Close()

	ctx := context.Background()
	c, err := proxmox.NewClient(ctx, ts.URL, api.TokenCredentials("root@pam!sdk", "secret"))
	if err != nil {
		fmt.Println("connect:", err)
		return
	}
	s := c.Storage()

	// Create the entry over an EXISTING ZFS dataset (creating a datastore
	// never creates backing storage). Requires Datastore.Allocate.
	res, err := s.CreateDatastore(ctx, &storage.DatastoreSpec{
		Storage:   "fast-vm",
		Type:      "zfspool",
		Pool:      "fast/vm",
		Sparse:    true,
		Blocksize: "16k",
		Content:   []string{"images", "rootdir"},
	})
	if err != nil {
		fmt.Println("create:", err)
		return
	}
	fmt.Println("created:", res.Storage, res.Type)

	// Read it back. Content/Nodes are sets on the wire (order not preserved);
	// the read's digest is the guard for the update that follows.
	d, err := s.GetDatastore(ctx, "fast-vm")
	if err != nil {
		fmt.Println("get:", err)
		return
	}
	fmt.Println("content:", d.Content)

	// Narrow the content restriction, guarded by the digest of the read the
	// decision came from — a concurrent edit fails this write, never loses it.
	if _, err := s.UpdateDatastore(ctx, "fast-vm", &storage.DatastoreUpdate{
		Content: []string{"images"},
		Digest:  d.Digest,
	}); err != nil {
		fmt.Println("update:", err)
		return
	}
	updated, err := s.GetDatastore(ctx, "fast-vm")
	if err != nil {
		fmt.Println("get after update:", err)
		return
	}
	fmt.Println("content:", updated.Content)

	// Remove the entry: cluster config only, the dataset survives.
	if err := s.DeleteDatastore(ctx, "fast-vm"); err != nil {
		fmt.Println("delete:", err)
		return
	}
	fmt.Println("deleted")
	// Output:
	// created: fast-vm zfspool
	// content: images,rootdir
	// content: images
	// deleted
}
