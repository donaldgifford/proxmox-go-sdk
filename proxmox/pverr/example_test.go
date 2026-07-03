package pverr_test

import (
	"errors"
	"fmt"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/pverr"
)

func ExampleClassify() {
	// The transport classifies an HTTP status + PVE body into the taxonomy.
	err := pverr.Classify(404, "/nodes/pve/qemu/999/status/current",
		pverr.PVEBody{Message: "VM 999 not found"}, nil)

	fmt.Println(errors.Is(err, pverr.ErrNotFound))

	var pe *pverr.Error
	if errors.As(err, &pe) {
		fmt.Println(pe.Status)
	}
	// Output:
	// true
	// 404
}
