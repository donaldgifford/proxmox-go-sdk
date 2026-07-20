// Command pve-schemadiff flags drift between a Proxmox VE apidoc.js API-schema
// dump and a committed baseline, so CI fails when the 9.x REST surface changes
// across minor releases (OQ-7 / IMPL-0001). It is a test helper, not part of the
// SDK library surface; the parse/diff logic lives in the importable
// github.com/donaldgifford/proxmox-go-sdk/cmd/pve-schemadiff/schema package.
//
// Usage:
//
//	# fail (exit 1) if apidoc.js drifted from the baseline:
//	pve-schemadiff -apidoc apidoc.js -baseline testdata/baseline.json
//
//	# refresh the baseline from a new apidoc.js (after an intentional bump):
//	pve-schemadiff -apidoc apidoc.js -baseline testdata/baseline.json -update
package main

import (
	"bytes"
	"compress/gzip"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/donaldgifford/proxmox-go-sdk/cmd/pve-schemadiff/schema"
)

// Injected at build time via -ldflags (see .goreleaser.yml / Dockerfile).
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	apidocPath := flag.String("apidoc", "", "path to the Proxmox apidoc.js schema dump (required)")
	baselinePath := flag.String("baseline", "", "path to the committed baseline JSON (required)")
	update := flag.Bool("update", false, "rewrite the baseline from apidoc.js and exit 0")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("pve-schemadiff %s (%s, %s)\n", version, commit, date)
		return
	}
	if *apidocPath == "" || *baselinePath == "" {
		fmt.Fprintln(os.Stderr, "pve-schemadiff: both -apidoc and -baseline are required")
		flag.Usage()
		os.Exit(2)
	}

	report, drift, err := run(*apidocPath, *baselinePath, *update)
	if err != nil {
		fmt.Fprintln(os.Stderr, "pve-schemadiff:", err)
		os.Exit(2)
	}
	fmt.Print(report)
	if drift {
		os.Exit(1)
	}
}

// run parses apidoc.js and either rewrites the baseline (update) or diffs
// against it. It returns the human report to print, whether the schema drifted
// (so main can set the exit code), and a non-nil error only for operational
// failures (unreadable files, malformed input). Keeping os.Exit and stdout out
// of run makes it directly testable.
func run(apidocPath, baselinePath string, update bool) (report string, drift bool, err error) {
	apidocJS, err := readApidoc(apidocPath)
	if err != nil {
		return "", false, err
	}
	current, err := schema.Parse(apidocJS)
	if err != nil {
		return "", false, err
	}

	if update {
		data, err := schema.MarshalBaseline(current)
		if err != nil {
			return "", false, err
		}
		if err := os.WriteFile(baselinePath, data, 0o644); err != nil {
			return "", false, fmt.Errorf("write baseline: %w", err)
		}
		return fmt.Sprintf("baseline updated: %d endpoint(s)\n", len(current)), false, nil
	}

	baselineData, err := os.ReadFile(baselinePath)
	if err != nil {
		return "", false, fmt.Errorf("read baseline: %w", err)
	}
	baseline, err := schema.UnmarshalBaseline(baselineData)
	if err != nil {
		return "", false, err
	}

	diff := schema.Diff(baseline, current)
	if diff.Empty() {
		return fmt.Sprintf("no drift: %d endpoint(s) match the baseline\n", len(current)), false, nil
	}
	lines := make([]string, 0, len(diff.Added)+len(diff.Removed)+1)
	for _, ep := range diff.Added {
		lines = append(lines, fmt.Sprintf("+ %s %s", ep.Method, ep.Path))
	}
	for _, ep := range diff.Removed {
		lines = append(lines, fmt.Sprintf("- %s %s", ep.Method, ep.Path))
	}
	lines = append(lines, fmt.Sprintf("schema drift: %d added, %d removed", len(diff.Added), len(diff.Removed)))
	return strings.Join(lines, "\n") + "\n", true, nil
}

// gzipMagic is the two-byte header every gzip stream starts with.
var gzipMagic = []byte{0x1f, 0x8b}

// readApidoc reads the apidoc.js dump, transparently gunzipping it when the
// content is gzip-compressed (detected by magic bytes, not file extension).
// The real ~4 MB dump is committed gzipped (IMPL-0003 OQ-1) so CI can parse
// genuine PVE output without bloating the module.
func readApidoc(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read apidoc: %w", err)
	}
	if !bytes.HasPrefix(data, gzipMagic) {
		return data, nil
	}
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("gunzip apidoc: %w", err)
	}
	raw, err := io.ReadAll(zr)
	if err != nil {
		return nil, fmt.Errorf("gunzip apidoc: %w", err)
	}
	if err := zr.Close(); err != nil {
		return nil, fmt.Errorf("gunzip apidoc: %w", err)
	}
	return raw, nil
}
