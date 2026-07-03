package ssh_test

import (
	"context"
	"fmt"
	"strings"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/ssh"
)

// Example shows the canonical side-channel flow: configure host-key
// verification and credentials, Connect to a node, upload a cloud-init snippet
// over SFTP, then Close. It has no Output directive because it needs a real PVE
// host; go test compiles it but does not run it.
func Example() {
	ctx := context.Background()

	// Host-key verification is mandatory — here via the caller's known_hosts.
	c := ssh.NewClient(
		ssh.WithUser("root"),
		ssh.WithPassword("s3cret"),
		ssh.WithKnownHostsFile("/home/me/.ssh/known_hosts"),
	)
	if err := c.Connect(ctx, "pve.example.com"); err != nil {
		fmt.Println("connect:", err)
		return
	}
	defer c.Close()

	snippet := strings.NewReader("#cloud-config\nhostname: web-1\n")
	if err := c.UploadSnippet(ctx, "/var/lib/vz/snippets/web-1.yaml", snippet); err != nil {
		fmt.Println("upload:", err)
		return
	}
}
