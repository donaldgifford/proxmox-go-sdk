// Command pve-schemadiff guards this repo's picture of the Proxmox VE REST API
// against reality. It has two modes, both comparing something to the committed
// endpoint baseline:
//
// Schema drift (the default) flags differences between a Proxmox VE apidoc.js
// API-schema dump and the baseline, so CI fails when the 9.x REST surface changes
// across minor releases (OQ-7 / IMPL-0001).
//
// Coverage (-coverage) measures the SDK against that baseline and renders
// docs/COVERAGE.md, failing on a stale report or on a mockpve route referencing
// an endpoint real PVE does not serve (DESIGN-0005 / IMPL-0006).
//
// It is a test helper, not part of the SDK library surface; the logic lives in
// the importable schema and coverage packages beside it.
//
// Usage:
//
//	# fail (exit 1) if apidoc.js drifted from the baseline:
//	pve-schemadiff -apidoc apidoc.js -baseline testdata/baseline.json
//
//	# refresh the baseline from a new apidoc.js (after an intentional bump):
//	pve-schemadiff -apidoc apidoc.js -baseline testdata/baseline.json -update
//
//	# regenerate the coverage report:
//	pve-schemadiff -coverage -baseline testdata/baseline.json \
//	  -annotations coverage-annotations.yaml -out ../../docs/COVERAGE.md
//
//	# fail (exit 1) if the committed report is stale:
//	pve-schemadiff -coverage -baseline testdata/baseline.json \
//	  -annotations coverage-annotations.yaml -out ../../docs/COVERAGE.md -check
package main

import (
	"bytes"
	"compress/gzip"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/donaldgifford/proxmox-go-sdk/cmd/pve-schemadiff/coverage"
	"github.com/donaldgifford/proxmox-go-sdk/cmd/pve-schemadiff/schema"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/mockpve"
)

// Injected at build time via -ldflags (see .goreleaser.yml / Dockerfile).
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// options is the parsed command line. It is a struct so the second mode's flags
// did not turn run() into a positional list nobody can read at the call site.
type options struct {
	apidoc      string
	baseline    string
	update      bool
	coverage    bool
	annotations string
	out         string
	check       bool

	// routes is the coverage numerator. main fills it from mockpve; tests supply
	// their own, since the real mock's routes cannot be measured against a
	// fixture baseline without every one of them looking fabricated.
	routes []string
}

func main() {
	var opts options
	flag.StringVar(&opts.apidoc, "apidoc", "", "path to the Proxmox apidoc.js schema dump (schema-diff mode)")
	flag.StringVar(&opts.baseline, "baseline", "", "path to the committed baseline JSON (required)")
	flag.BoolVar(&opts.update, "update", false, "rewrite the baseline from apidoc.js and exit 0")
	flag.BoolVar(&opts.coverage, "coverage", false, "measure SDK coverage against the baseline")
	flag.StringVar(&opts.annotations, "annotations", "", "path to coverage-annotations.yaml (coverage mode)")
	flag.StringVar(&opts.out, "out", "", "path of the coverage report to write or verify (coverage mode)")
	flag.BoolVar(&opts.check, "check", false, "verify -out matches a fresh render instead of writing it")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("pve-schemadiff %s (%s, %s)\n", version, commit, date)
		return
	}
	if err := opts.validate(); err != nil {
		fmt.Fprintln(os.Stderr, "pve-schemadiff:", err)
		flag.Usage()
		os.Exit(2)
	}
	opts.routes = mockpve.New().Routes()

	report, fail, err := run(&opts)
	if err != nil {
		fmt.Fprintln(os.Stderr, "pve-schemadiff:", err)
		os.Exit(2)
	}
	fmt.Print(report)
	if fail {
		os.Exit(1)
	}
}

// validate rejects flag combinations that would silently do nothing, so a typo
// in a CI invocation fails loudly instead of passing without checking anything.
func (o *options) validate() error {
	if o.baseline == "" {
		return errors.New("-baseline is required")
	}
	if !o.coverage {
		if o.apidoc == "" {
			return errors.New("-apidoc is required (or pass -coverage)")
		}
		if o.annotations != "" || o.out != "" || o.check {
			return errors.New("-annotations, -out and -check apply to -coverage only")
		}
		return nil
	}
	if o.annotations == "" {
		return errors.New("-annotations is required in coverage mode")
	}
	if o.update {
		return errors.New("-update applies to schema-diff mode only")
	}
	if o.check && o.out == "" {
		return errors.New("-check needs -out: the report to verify")
	}
	return nil
}

// run dispatches to the requested mode. It returns the human report to print,
// whether a check failed (so main can set the exit code), and a non-nil error
// only for operational failures (unreadable files, malformed input). Keeping
// os.Exit and stdout out of run makes both modes directly testable.
func run(opts *options) (report string, fail bool, err error) {
	if opts.coverage {
		return runCoverage(opts)
	}
	return runSchemaDiff(opts)
}

// runCoverage builds the report, runs both checks, and either writes or verifies
// the file.
func runCoverage(opts *options) (report string, fail bool, err error) {
	baseline, err := readBaseline(opts.baseline)
	if err != nil {
		return "", false, err
	}
	ann, err := coverage.LoadAnnotations(opts.annotations)
	if err != nil {
		return "", false, err
	}
	rep, err := coverage.Build(baseline, opts.routes, ann)
	if err != nil {
		return "", false, err
	}

	// The fabrication guard runs before any write: a report naming endpoints that
	// do not exist must never reach disk, in either mode.
	if err := rep.Check(); err != nil {
		return checkFailed(err)
	}
	rendered := rep.Markdown()

	switch {
	case opts.check:
		committed, err := os.ReadFile(opts.out) // #nosec G304 -- path is the operator's own -out flag.
		if err != nil {
			return "", false, fmt.Errorf("read report: %w", err)
		}
		if err := coverage.CheckDrift(opts.out, string(committed), rendered); err != nil {
			return checkFailed(err)
		}
		return coverageSummary("is current", opts.out, rep), false, nil
	case opts.out != "":
		if err := os.WriteFile(opts.out, []byte(rendered), 0o644); err != nil {
			return "", false, fmt.Errorf("write report: %w", err)
		}
		return coverageSummary("written", opts.out, rep), false, nil
	default:
		return rendered, false, nil
	}
}

// checkFailed reports a failed check as the tool's exit-1 outcome: the error
// text becomes the report, and err stays nil because a failed check is a
// finding, not an operational failure — CI reads exit 1 for "the check says no"
// and exit 2 for "the tool could not run".
func checkFailed(err error) (report string, fail bool, _ error) {
	return err.Error() + "\n", true, nil
}

// coverageSummary is the one-line result CI logs show.
func coverageSummary(what, path string, rep *coverage.Report) string {
	return fmt.Sprintf("coverage: %s %s: %d of %d endpoint(s) covered (%s)\n",
		path, what, rep.Totals.Covered, rep.Totals.Total, rep.Totals.Percent())
}

// runSchemaDiff parses apidoc.js and either rewrites the baseline (update) or
// diffs against it.
func runSchemaDiff(opts *options) (report string, drift bool, err error) {
	apidocJS, err := readApidoc(opts.apidoc)
	if err != nil {
		return "", false, err
	}
	current, err := schema.Parse(apidocJS)
	if err != nil {
		return "", false, err
	}

	if opts.update {
		data, err := schema.MarshalBaseline(current)
		if err != nil {
			return "", false, err
		}
		if err := os.WriteFile(opts.baseline, data, 0o644); err != nil {
			return "", false, fmt.Errorf("write baseline: %w", err)
		}
		return fmt.Sprintf("baseline updated: %d endpoint(s)\n", len(current)), false, nil
	}

	baseline, err := readBaseline(opts.baseline)
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

// readBaseline reads and parses the committed endpoint baseline.
func readBaseline(path string) ([]schema.Endpoint, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path is the operator's own -baseline flag.
	if err != nil {
		return nil, fmt.Errorf("read baseline: %w", err)
	}
	return schema.UnmarshalBaseline(data)
}

// gzipMagic is the two-byte header every gzip stream starts with.
var gzipMagic = []byte{0x1f, 0x8b}

// readApidoc reads the apidoc.js dump, transparently gunzipping it when the
// content is gzip-compressed (detected by magic bytes, not file extension).
// The real ~4 MB dump is committed gzipped (IMPL-0003 OQ-1) so CI can parse
// genuine PVE output without bloating the module.
func readApidoc(path string) ([]byte, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path is the operator's own -apidoc flag.
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
