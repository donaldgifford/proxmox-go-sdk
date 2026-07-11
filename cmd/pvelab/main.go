// Command pvelab provisions the ephemeral nested-PVE dogfood lab this repo's
// integration suite runs against (DESIGN-0002 / IMPL-0002): it prepares the
// auto-install ISO on the outer host, creates and boots the nested node VMs,
// forms the cluster, and tears everything down again. It is a development
// tool run via `go run` (never a released artifact); the reusable logic lives
// in the importable github.com/donaldgifford/proxmox-go-sdk/cmd/pvelab/lab
// package, following the cmd/pve-schemadiff/schema precedent.
//
// Usage:
//
//	pvelab <command> [flags]
//
//	iso     prepare the auto-install ISO on the outer host (assistant over SSH)
//	up      create node VMs -> wait ready -> cluster -> write state + env files
//	down    stop + delete the lab VMs (reads state; config-only via -no-state)
//	status  show the lab's current state (outer view + per-node readiness)
//	env     print the .pvelab.env exports for the inner test suite
//
// Every command reads the YAML config (default pvelab.yaml — settings only;
// secrets are env-var NAMES resolved at runtime, never values in the file).
package main

import (
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"runtime/debug"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: logLevel(),
	})))
	os.Exit(run(os.Args[1:]))
}

// logLevel enables debug logging when PVELAB_DEBUG is set (mirrors the
// integration suite's PVE_DEBUG convention).
func logLevel() slog.Level {
	if os.Getenv("PVELAB_DEBUG") != "" {
		return slog.LevelDebug
	}
	return slog.LevelInfo
}

// buildVersion reports the module version stamped by the Go toolchain.
// pvelab is `go run`-only (design OQ-2), so there are no ldflags: a tagged
// `go run <module>/cmd/pvelab@vX.Y.Z` reports vX.Y.Z, a branch run "(devel)".
func buildVersion() string {
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" {
		return bi.Main.Version
	}
	return "(devel)"
}

func usage() {
	fmt.Fprintf(os.Stderr, `pvelab %s — nested-PVE dogfood lab (never released; run via go run)

usage: pvelab <command> [flags]

commands:
  iso     prepare the auto-install ISO on the outer host
  up      provision node VMs, form the cluster, write state + env files
  down    stop + delete the lab VMs
  status  show the lab's current state
  env     print the inner suite's environment exports

run "pvelab <command> -h" for that command's flags.
`, buildVersion())
}

func run(args []string) int {
	if len(args) == 0 {
		usage()
		return 2
	}

	var err error
	switch cmd := args[0]; cmd {
	case "iso":
		err = cmdISO(args[1:])
	case "up":
		err = cmdUp(args[1:])
	case "down":
		err = cmdDown(args[1:])
	case "status":
		err = cmdStatus(args[1:])
	case "env":
		err = cmdEnv(args[1:])
	case "version", "-version", "--version":
		fmt.Println("pvelab", buildVersion())
	case "help", "-h", "-help", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "pvelab: unknown command %q\n\n", cmd)
		usage()
		return 2
	}

	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 2
		}
		slog.Error("pvelab failed", "err", err)
		return 1
	}
	return 0
}

// configFlag registers the shared -config flag on a subcommand FlagSet.
func configFlag(fs *flag.FlagSet) *string {
	return fs.String("config", "pvelab.yaml", "path to the lab YAML config")
}

// errNotImplemented marks subcommands whose lab-package implementation lands
// later in IMPL-0002 Phase 1; the dispatch skeleton ships first so each task
// wires into a compiling binary.
var errNotImplemented = errors.New("not implemented yet (IMPL-0002 Phase 1 in progress)")

func cmdISO(args []string) error {
	fs := flag.NewFlagSet("iso", flag.ContinueOnError)
	cfgPath := configFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	return fmt.Errorf("pvelab iso (config %s): %w", *cfgPath, errNotImplemented)
}

func cmdUp(args []string) error {
	fs := flag.NewFlagSet("up", flag.ContinueOnError)
	cfgPath := configFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	return fmt.Errorf("pvelab up (config %s): %w", *cfgPath, errNotImplemented)
}

func cmdDown(args []string) error {
	fs := flag.NewFlagSet("down", flag.ContinueOnError)
	cfgPath := configFlag(fs)
	fs.Bool("force", false, "tolerate missing/half-created objects")
	fs.Bool("no-state", false, "tear down from config alone (ignore the state file)")
	fs.Bool("purge-isos", false, "also delete the prepared installer ISOs")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return fmt.Errorf("pvelab down (config %s): %w", *cfgPath, errNotImplemented)
}

func cmdStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	cfgPath := configFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	return fmt.Errorf("pvelab status (config %s): %w", *cfgPath, errNotImplemented)
}

func cmdEnv(args []string) error {
	fs := flag.NewFlagSet("env", flag.ContinueOnError)
	cfgPath := configFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	return fmt.Errorf("pvelab env (config %s): %w", *cfgPath, errNotImplemented)
}
