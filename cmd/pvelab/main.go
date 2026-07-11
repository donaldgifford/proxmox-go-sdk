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
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/donaldgifford/proxmox-go-sdk/cmd/pvelab/lab"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox"
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

// cmdISO prepares the http-mode auto-install ISO on the outer host: SSH in
// (host-key verification mandatory), install the assistant if missing, run
// prepare-iso once per PVE version, and print the resulting volid.
func cmdISO(args []string) error {
	fs := flag.NewFlagSet("iso", flag.ContinueOnError)
	cfgPath := configFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx, stop := signalContext()
	defer stop()

	cfg, err := lab.LoadConfig(*cfgPath)
	if err != nil {
		return err
	}
	client, err := outerClient(ctx, cfg)
	if err != nil {
		return err
	}

	sshOpts, err := cfg.SSHOptions()
	if err != nil {
		return err
	}
	host, err := cfg.OuterHost()
	if err != nil {
		return err
	}
	sc := client.SSH(sshOpts...)
	if err := sc.Connect(ctx, host); err != nil {
		return fmt.Errorf("ssh connect to outer host %s: %w", host, err)
	}
	defer func() {
		if err := sc.Close(); err != nil {
			slog.Debug("close ssh", "err", err)
		}
	}()

	volid, err := lab.PrepareISO(ctx, client, sc, cfg, slog.Default())
	if err != nil {
		return err
	}
	fmt.Println(volid)
	return nil
}

// signalContext is the root context every subcommand runs under: Ctrl-C /
// SIGTERM cancels in-flight SDK calls instead of killing mid-operation.
func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

// outerClient builds the SDK client for the outer PVE host from config +
// resolved env credentials.
func outerClient(ctx context.Context, cfg *lab.Config) (*proxmox.Client, error) {
	creds, err := cfg.OuterCredentials()
	if err != nil {
		return nil, err
	}
	var opts []proxmox.Option
	if cfg.Outer.InsecureTLS {
		opts = append(opts, proxmox.WithInsecureSkipVerify(true))
	}
	return proxmox.NewClient(ctx, cfg.Outer.Endpoint, creds, opts...)
}

// cmdUp provisions the lab: preflight (VMIDs free, ISO prepared) → answer
// server up → create VMs → start (unattended installs begin) → wait for every
// nested API → write the env handoff. State is updated after every stage so a
// mid-up failure leaves evidence on disk (design OQ-7).
func cmdUp(args []string) error {
	fs := flag.NewFlagSet("up", flag.ContinueOnError)
	cfgPath := configFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx, stop := signalContext()
	defer stop()

	cfg, err := lab.LoadConfig(*cfgPath)
	if err != nil {
		return err
	}
	client, err := outerClient(ctx, cfg)
	if err != nil {
		return err
	}
	if err := lab.EnsureVMIDsFree(ctx, client, cfg); err != nil {
		return err
	}
	if err := lab.EnsureISOPrepared(ctx, client, cfg); err != nil {
		return err
	}
	isoVolid := lab.PreparedISOVolid(cfg.Outer.ISOStorage, cfg.Nested.PVEVersion)
	rootPW := os.Getenv(cfg.Nested.RootPasswordEnv) // presence validated at load.

	answers := lab.NewAnswerServer(cfg, rootPW, slog.Default())
	if err := answers.Start(ctx); err != nil {
		return err
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := answers.Shutdown(shutdownCtx); err != nil {
			slog.Debug("answer server shutdown", "err", err)
		}
	}()

	if err := provisionLab(ctx, client, cfg, isoVolid, rootPW); err != nil {
		return err
	}

	env, err := lab.NewEnvFile(cfg, rootPW)
	if err != nil {
		return err
	}
	if err := lab.WriteEnvFile(lab.DefaultEnvPath, env); err != nil {
		return err
	}
	slog.Info("lab is up", "nodes", len(cfg.Nested.Nodes), "env", lab.DefaultEnvPath)
	return nil
}

// provisionLab is `up`'s staged create → start → wait → cluster flow,
// updating the state file after every stage (design OQ-7: a mid-up failure
// leaves evidence on disk).
func provisionLab(ctx context.Context, client *proxmox.Client, cfg *lab.Config, isoVolid, rootPW string) error {
	if _, err := lab.UpdateState(lab.DefaultStatePath, func(st *lab.State) {
		st.ClusterName = cfg.Nested.ClusterName
		st.PVEVersion = cfg.Nested.PVEVersion
		st.ISOVolid = isoVolid
		st.SeedNodes(cfg.Nested.Nodes)
	}); err != nil {
		return err
	}

	if err := lab.CreateNodeVMs(ctx, client, cfg, isoVolid, slog.Default()); err != nil {
		return err
	}
	if _, err := lab.UpdateState(lab.DefaultStatePath, func(st *lab.State) {
		for i := range st.Nodes {
			st.Nodes[i].Created = true
		}
	}); err != nil {
		return err
	}

	if err := lab.StartNodeVMs(ctx, client, cfg, slog.Default()); err != nil {
		return err
	}
	if _, err := lab.UpdateState(lab.DefaultStatePath, func(st *lab.State) {
		for i := range st.Nodes {
			st.Nodes[i].Started = true
		}
	}); err != nil {
		return err
	}

	readiness, waitErr := lab.WaitReady(ctx, cfg, rootPW, slog.Default())
	if _, err := lab.UpdateState(lab.DefaultStatePath, func(st *lab.State) {
		st.ApplyReadiness(readiness)
	}); err != nil {
		return errors.Join(waitErr, err)
	}
	if waitErr != nil {
		return waitErr
	}

	if err := lab.FormCluster(ctx, cfg, rootPW, slog.Default()); err != nil {
		return err
	}
	if _, err := lab.UpdateState(lab.DefaultStatePath, func(st *lab.State) {
		st.Clustered = true
	}); err != nil {
		return err
	}
	return nil
}

// cmdDown tears the lab down. It deletes what the CONFIG says (VMIDs are
// declared, not discovered), guarded by the blast-radius checks in lab; the
// state file only feeds status/env, so -no-state and normal down share the
// same deletion path.
func cmdDown(args []string) error {
	fs := flag.NewFlagSet("down", flag.ContinueOnError)
	cfgPath := configFlag(fs)
	force := fs.Bool("force", false, "tolerate missing/half-created objects")
	noState := fs.Bool("no-state", false, "tear down from config alone (leave the state/env files)")
	purgeISOs := fs.Bool("purge-isos", false, "also delete the prepared installer ISOs")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx, stop := signalContext()
	defer stop()

	cfg, err := lab.LoadConfig(*cfgPath)
	if err != nil {
		return err
	}
	client, err := outerClient(ctx, cfg)
	if err != nil {
		return err
	}
	if err := lab.Teardown(ctx, client, cfg, lab.TeardownOptions{
		Force:     *force,
		PurgeISOs: *purgeISOs,
	}, slog.Default()); err != nil {
		return err
	}
	if !*noState {
		for _, p := range []string{lab.DefaultStatePath, lab.DefaultEnvPath} {
			if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
				slog.Warn("lab is down but a handoff file could not be removed", "path", p, "err", err)
			}
		}
	}
	return nil
}

// cmdStatus prints the outer view (VM presence/power) beside the state
// file's readiness evidence, one line per configured node.
func cmdStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	cfgPath := configFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx, stop := signalContext()
	defer stop()

	cfg, err := lab.LoadConfig(*cfgPath)
	if err != nil {
		return err
	}
	client, err := outerClient(ctx, cfg)
	if err != nil {
		return err
	}
	st, err := lab.LoadState(lab.DefaultStatePath)
	if err != nil && !errors.Is(err, lab.ErrNoState) {
		return err
	}

	svc := client.QEMU(cfg.Outer.Node)
	for _, n := range cfg.Nested.Nodes {
		outer := "absent"
		if vm, err := svc.Get(ctx, n.VMID); err == nil {
			outer = string(vm.Status)
		}
		readiness := "readiness unknown (no state file)"
		if st != nil {
			readiness = "not ready"
			if ns := st.FindNode(n.Name); ns != nil && ns.Ready {
				readiness = fmt.Sprintf("ready (install took %.0fs)", ns.ReadySeconds)
			}
		}
		fmt.Printf("%s\tvmid=%d\touter=%s\t%s\n", n.Name, n.VMID, outer, readiness)
	}
	if st != nil && st.Clustered {
		fmt.Printf("cluster\t%s\tformed (quorate at up)\n", st.ClusterName)
	}
	return nil
}

// cmdEnv re-derives and prints the inner-suite environment from config —
// same content `up` writes to .pvelab.env, without touching any file.
func cmdEnv(args []string) error {
	fs := flag.NewFlagSet("env", flag.ContinueOnError)
	cfgPath := configFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := lab.LoadConfig(*cfgPath)
	if err != nil {
		return err
	}
	env, err := lab.NewEnvFile(cfg, os.Getenv(cfg.Nested.RootPasswordEnv))
	if err != nil {
		return err
	}
	// A static error on write failure: the rendered env carries the password,
	// so nothing derived from it belongs in the logged error chain.
	if _, err := os.Stdout.Write(lab.RenderEnv(env)); err != nil {
		return errors.New("write env to stdout failed")
	}
	return nil
}
