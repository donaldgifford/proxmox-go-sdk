package tasks_test

import (
	"fmt"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/tasks"
)

func ExampleParseUPID() {
	u, _ := tasks.ParseUPID("UPID:pve:000A1B2C:00ABCDEF:6489ABCD:qmstart:100:root@pam:")
	fmt.Println(u.Node, u.Type, u.ID)
	// Output: pve qmstart 100
}
