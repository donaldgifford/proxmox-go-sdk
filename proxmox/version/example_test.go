package version_test

import (
	"fmt"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/version"
)

func ExampleCapabilities() {
	caps, _ := version.Parse("9.2.1")
	fmt.Println(caps.AtLeast(9, 1))
	fmt.Println(caps.DynamicLoadBalancer()) // gated at 9.2+
	// Output:
	// true
	// true
}
