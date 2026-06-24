// Command mockpve runs the in-memory Proxmox VE responder as a standalone
// server, so consumers can integration-test against a fake PVE without a live
// cluster. The responder itself lives in the importable package
// github.com/donaldgifford/proxmox-go-sdk/proxmox/mockpve.
//
// Server wiring is not yet implemented — it lands with IMPL-0001 Phase 1.
package main

import (
	"fmt"
	"log/slog"
	"os"
)

// Injected at build time via -ldflags (see .goreleaser.yml / Dockerfile).
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	fmt.Printf("mockpve %s (%s, %s)\n", version, commit, date)
	fmt.Println("mockpve server not yet implemented (see IMPL-0001 Phase 1)")
}
