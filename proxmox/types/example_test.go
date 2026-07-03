package types_test

import (
	"encoding/json"
	"fmt"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/types"
)

func ExamplePVEBool() {
	// Proxmox encodes booleans as 0/1; PVEBool normalises both directions.
	var cfg struct {
		OnBoot types.PVEBool `json:"onboot"`
	}
	_ = json.Unmarshal([]byte(`{"onboot":1}`), &cfg)
	fmt.Println(cfg.OnBoot.Bool())

	out, err := json.Marshal(cfg)
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println(string(out))
	// Output:
	// true
	// {"onboot":1}
}
