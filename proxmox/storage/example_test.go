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

// Example uploads an ISO, allocates a disk volume, takes a volume-chain snapshot
// of it (a 9.1+ capability), then cleans both up — awaiting each PVE task. It is
// the Phase 3 storage flow end to end.
func Example() {
	// mockpve stands in for a live cluster so the example is self-contained;
	// against a real node, pass its URL and a real token to proxmox.NewClient.
	mock := mockpve.New()
	mock.SeedVersion("9.1.0", "9.1", "mockpve") // 9.1 enables volume-chain snapshots.
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
		Filename: "debian-12.iso",
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

	// Allocate a disk volume to snapshot (allocation is synchronous → volid).
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

	// Snapshot the volume and await the snapshot task.
	ref, err = s.CreateVolumeSnapshot(ctx, "pve", "local", volid,
		&storage.VolumeSnapshotSpec{Name: "pre-change"})
	if err != nil {
		fmt.Println("snapshot:", err)
		return
	}
	if _, err := c.Tasks().Wait(ctx, ref); err != nil {
		fmt.Println("await snapshot:", err)
		return
	}

	snaps, err := s.VolumeSnapshots(ctx, "pve", "local", volid)
	if err != nil {
		fmt.Println("list snapshots:", err)
		return
	}
	fmt.Println(snaps[0].Name)

	// Clean up: drop the snapshot, then free the volume, awaiting each task.
	ref, err = s.DeleteVolumeSnapshot(ctx, "pve", "local", volid, "pre-change")
	if err != nil {
		fmt.Println("delete snapshot:", err)
		return
	}
	if _, err := c.Tasks().Wait(ctx, ref); err != nil {
		fmt.Println("await delete snapshot:", err)
		return
	}
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
	// pre-change
}
