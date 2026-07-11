package lab

import (
	"context"
	"fmt"
	"log/slog"
	"path"
	"strings"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/ssh"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/storage"
)

// execer is the one *ssh.Client method the ISO workflow needs; the seam lets
// unit tests script command outcomes without an SSH server (the client's own
// exec plumbing is covered by proxmox/ssh's in-process-server tests).
type execer interface {
	Exec(ctx context.Context, cmd string) ([]byte, error)
}

var _ execer = (*ssh.Client)(nil)

// assistantPackages are apt-installed on the outer host if missing (Phase 0
// found the design's "already on node" premise stale).
const assistantPackages = "proxmox-auto-install-assistant xorriso"

// preparedISOName is the http-mode installer ISO filename for one PVE version.
// One prepared ISO serves every node (the 2026-07-10 design amendment): only
// the answer URL is baked in; per-node answers are rendered at install time by
// the embedded answer server. The pvelab- prefix marks it harness-owned for
// `down -purge-isos`.
func preparedISOName(pveVersion string) string {
	return "pvelab-" + pveVersion + "-auto-http.iso"
}

// PreparedISOVolid is the volid `pvelab up` expects (and `pvelab iso`
// produces) on the outer host's ISO storage, e.g.
// "local:iso/pvelab-9.2-auto-http.iso".
func PreparedISOVolid(isoStorage, pveVersion string) string {
	return isoStorage + ":iso/" + preparedISOName(pveVersion)
}

// PrepareISO ensures the http-mode auto-install ISO for cfg's PVE version
// exists on the outer host's ISO storage, running
// proxmox-auto-install-assistant over the SSH side-channel to build it if
// absent. Idempotent: if the volid is already visible it returns immediately.
// The prepared ISO is written next to nested.base_iso, which must therefore
// live inside outer.iso_storage's iso directory (Phase 0's layout) — the
// post-run volid check catches a mismatch.
//
// The `--fetch-from http --url` invocation is live-verified at the Phase 1
// acceptance run; Phase 0 verified the assistant end-to-end in
// `--fetch-from iso` mode, which remains the documented fallback.
func PrepareISO(ctx context.Context, c *proxmox.Client, sc execer, cfg *Config, logger *slog.Logger) (string, error) {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	volid := PreparedISOVolid(cfg.Outer.ISOStorage, cfg.Nested.PVEVersion)

	present, err := isoPresent(ctx, c, cfg, volid)
	if err != nil {
		return "", err
	}
	if present {
		logger.Info("prepared ISO already present, skipping", "volid", volid)
		return volid, nil
	}

	if err := ensureAssistant(ctx, sc, logger); err != nil {
		return "", err
	}

	out := path.Join(path.Dir(cfg.Nested.BaseISO), preparedISOName(cfg.Nested.PVEVersion))
	cmd := fmt.Sprintf("proxmox-auto-install-assistant prepare-iso %s --fetch-from http --url %s --output %s",
		shellQuote(cfg.Nested.BaseISO), shellQuote(cfg.Nested.AnswerURL), shellQuote(out))
	logger.Info("preparing auto-install ISO on the outer host (~1 min)", "cmd", cmd)
	if outBytes, err := sc.Exec(ctx, cmd); err != nil {
		return "", fmt.Errorf("prepare-iso: %w (output: %s)", err, truncateOutput(outBytes))
	}

	present, err = isoPresent(ctx, c, cfg, volid)
	if err != nil {
		return "", err
	}
	if !present {
		return "", fmt.Errorf(
			"prepare-iso succeeded but %s is not visible on storage %s — is nested.base_iso (%s) inside that storage's iso directory?",
			volid,
			cfg.Outer.ISOStorage,
			cfg.Nested.BaseISO,
		)
	}
	logger.Info("prepared ISO ready", "volid", volid)
	return volid, nil
}

// ensureAssistant verifies the assistant + xorriso exist on the outer host,
// apt-installing them if not. This is the one pvelab mutation outside the
// VMID blast radius (package install has no corresponding `down`), so it is
// check-first and logs before mutating.
func ensureAssistant(ctx context.Context, sc execer, logger *slog.Logger) error {
	if _, err := sc.Exec(ctx, "command -v proxmox-auto-install-assistant >/dev/null && command -v xorriso >/dev/null"); err == nil {
		return nil
	}
	logger.Warn("assistant/xorriso missing on the outer host — installing", "packages", assistantPackages)
	if out, err := sc.Exec(ctx, "apt-get install -y "+assistantPackages); err != nil {
		return fmt.Errorf("apt-get install %s: %w (output: %s) — install manually on the outer host and re-run `pvelab iso`",
			assistantPackages, err, truncateOutput(out))
	}
	return nil
}

// isoPresent reports whether volid is listed on the outer node's ISO storage.
func isoPresent(ctx context.Context, c *proxmox.Client, cfg *Config, volid string) (bool, error) {
	items, err := c.Storage().ListContent(ctx, cfg.Outer.Node, cfg.Outer.ISOStorage, storage.WithContentType("iso"))
	if err != nil {
		return false, fmt.Errorf("list %s content on %s: %w", cfg.Outer.ISOStorage, cfg.Outer.Node, err)
	}
	for _, it := range items {
		if it.Volid == volid {
			return true, nil
		}
	}
	return false, nil
}

// shellQuote single-quotes s for the remote shell (config values may carry
// spaces; they never legitimately carry quotes, but escape them anyway).
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// truncateOutput caps captured command output embedded in error messages.
func truncateOutput(b []byte) string {
	const maxLen = 2048
	if len(b) > maxLen {
		return string(b[:maxLen]) + "… (truncated)"
	}
	return string(b)
}
