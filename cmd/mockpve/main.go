// Command mockpve runs the in-memory Proxmox VE responder as a standalone
// server, so consumers can integration-test against a fake PVE without a live
// cluster. The responder itself lives in the importable package
// github.com/donaldgifford/proxmox-go-sdk/proxmox/mockpve.
package main

import (
	"context"
	"flag"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/mockpve"
)

// Injected at build time via -ldflags (see .goreleaser.yml / Dockerfile).
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	addr := flag.String("addr", ":8006", "listen address")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	slog.SetDefault(logger)
	slog.Info("mockpve starting", "version", version, "commit", commit, "date", date)

	srv := mockpve.New(mockpve.WithLogger(logger))

	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", *addr)
	if err != nil {
		slog.Error("listen failed", "err", err)
		os.Exit(1)
	}
	slog.Info("mockpve listening", "addr", ln.Addr().String())

	server := &http.Server{Handler: srv, ReadHeaderTimeout: 10 * time.Second}
	if err := server.Serve(ln); err != nil {
		slog.Error("serve failed", "err", err)
		os.Exit(1)
	}
}
