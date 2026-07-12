//go:build integration

package integration

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/console"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/qemu"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/types"
)

// TestAccessReads covers the Phase 6 access criterion: listing users under the
// 9.x privilege model, and the tokens owned by root@pam.
func TestAccessReads(t *testing.T) {
	c := newClient(t)
	ctx := testCtx(t)

	users, err := c.Access().ListUsers(ctx)
	if err != nil {
		t.Fatalf("Access().ListUsers: %v", err)
	}
	if len(users) == 0 {
		t.Error("ListUsers returned none; root@pam always exists on a live node")
	}
	if _, err := c.Access().ListTokens(ctx, "root@pam"); err != nil {
		t.Fatalf("Access().ListTokens(root@pam): %v", err)
	}
}

// TestConsoleMint covers the other half of the Phase 6 criterion: minting a VNC
// console ticket for a VM. It is self-contained — it spins up its own scratch VM
// (create + start), mints against it, then tears it down (stop + delete) in
// cleanup — so it does not depend on a pre-existing guest and never touches one.
// It is gated on PVE_TEST_STORAGE and PVE_TEST_CONSOLE_VMID and skips otherwise.
// The VMID is deliberately its own gate (not the shared PVE_TEST_VMID) so this
// test can run in the same invocation as TestQEMULifecycle without both trying to
// create the same VMID. Minting itself is non-destructive — it does not dial or
// alter the running guest.
func TestConsoleMint(t *testing.T) {
	c := newClient(t)
	node := testNode()

	storage := os.Getenv(envTestStorage)
	raw := os.Getenv(envTestConsoleVMID)
	if storage == "" || raw == "" {
		t.Skipf("console mint disabled (set %s and %s)", envTestStorage, envTestConsoleVMID)
	}
	vmid, err := strconv.Atoi(raw)
	if err != nil {
		t.Fatalf("%s=%q is not an integer: %v", envTestConsoleVMID, raw, err)
	}

	q := c.QEMU(node)
	ts := c.Tasks()

	// Spin up a scratch VM so the VMID exists for the mint.
	ref, err := q.Create(testCtx(t), &qemu.CreateSpec{
		VMID:    types.VMID(vmid),
		Name:    "sdk-itest-console",
		Memory:  512,
		Cores:   1,
		Net0:    "virtio,bridge=vmbr0",
		SCSI0:   storage + ":8",
		Storage: storage,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Tear down even if a later step fails: a running VM cannot be destroyed, so
	// stop first (best-effort — it may already be stopped), then delete.
	t.Cleanup(func() {
		ctx, cancel := cleanupCtx()
		defer cancel()
		if sref, serr := q.Stop(ctx, vmid); serr != nil {
			t.Logf("cleanup Stop(%d): %v", vmid, serr)
		} else if _, werr := ts.Wait(ctx, sref); werr != nil {
			t.Logf("cleanup Wait(stop): %v", werr)
		}
		dref, derr := q.Delete(ctx, vmid)
		if derr != nil {
			t.Logf("cleanup Delete(%d): %v", vmid, derr)
			return
		}
		if _, werr := ts.Wait(ctx, dref); werr != nil {
			t.Logf("cleanup Wait(delete): %v", werr)
		}
	})
	mustSucceed(t, ts, ref, "create")

	// Start it so the mint is against a running guest.
	ref, err = q.Start(testCtx(t), vmid)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	mustSucceed(t, ts, ref, "start")

	ticket, err := c.Console().MintVNCTicket(testCtx(t), node, console.KindQEMU, types.VMID(vmid))
	if err != nil {
		t.Fatalf("Console().MintVNCTicket(vmid=%d): %v", vmid, err)
	}
	if ticket.Ticket == "" || ticket.Port == "" {
		t.Errorf("minted ticket = %+v, want ticket and port set", ticket)
	}
}

// rfbGreetingLen is the exact length of the RFB ProtocolVersion greeting a VNC
// server sends first ("RFB 003.008\n" — RFC 6143 §7.1.1).
const rfbGreetingLen = 12

// TestConsoleRFB is the IMPL-0001 Phase 6 live-only criterion: the VNC (RFB)
// wire payload carried by console.Connect. It spins up its own scratch VM (the
// TestConsoleMint pattern, same gates), mints a VNC ticket, dials the
// vncwebsocket, and asserts the first 12 bytes are QEMU's RFB ProtocolVersion
// greeting. It has NO cassette by design (design OQ-6): the greeting rides a
// hijacked duplex stream go-vcr cannot carry, so the test skips under
// PVE_REPLAY=1 and bypasses record mode via newStreamClient.
func TestConsoleRFB(t *testing.T) {
	if os.Getenv(envReplay) == "1" {
		t.Skip("RFB stream cannot replay (raw websocket bytes, no cassette by design)")
	}
	c := newStreamClient(t)
	node := testNode()

	storage := os.Getenv(envTestStorage)
	raw := os.Getenv(envTestConsoleVMID)
	if storage == "" || raw == "" {
		t.Skipf("console RFB disabled (set %s and %s)", envTestStorage, envTestConsoleVMID)
	}
	vmid, err := strconv.Atoi(raw)
	if err != nil {
		t.Fatalf("%s=%q is not an integer: %v", envTestConsoleVMID, raw, err)
	}

	q := c.QEMU(node)
	ts := c.Tasks()

	ref, err := q.Create(testCtx(t), &qemu.CreateSpec{
		VMID:    types.VMID(vmid),
		Name:    "sdk-itest-rfb",
		Memory:  512,
		Cores:   1,
		Net0:    "virtio,bridge=vmbr0",
		SCSI0:   storage + ":8",
		Storage: storage,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := cleanupCtx()
		defer cancel()
		if sref, serr := q.Stop(ctx, vmid); serr != nil {
			t.Logf("cleanup Stop(%d): %v", vmid, serr)
		} else if _, werr := ts.Wait(ctx, sref); werr != nil {
			t.Logf("cleanup Wait(stop): %v", werr)
		}
		dref, derr := q.Delete(ctx, vmid)
		if derr != nil {
			t.Logf("cleanup Delete(%d): %v", vmid, derr)
			return
		}
		if _, werr := ts.Wait(ctx, dref); werr != nil {
			t.Logf("cleanup Wait(delete): %v", werr)
		}
	})
	mustSucceed(t, ts, ref, "create")

	ref, err = q.Start(testCtx(t), vmid)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	mustSucceed(t, ts, ref, "start")

	ticket, err := c.Console().MintVNCTicket(testCtx(t), node, console.KindQEMU, types.VMID(vmid))
	if err != nil {
		t.Fatalf("MintVNCTicket: %v", err)
	}

	stream, err := c.Console().Connect(testCtx(t), node, ticket)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() {
		if cerr := stream.Close(); cerr != nil {
			t.Logf("close stream: %v", cerr)
		}
	}()

	greeting := readGreeting(t, stream)
	// RFC 6143 §7.1.1: exactly "RFB xxx.yyy\n" — QEMU sends RFB 003.008 (or a
	// close 003.00x); assert the shape rather than pin one minor.
	if !rfbGreetingRe.Match(greeting) {
		t.Fatalf("greeting = %q, want an RFB ProtocolVersion greeting", greeting)
	}
	t.Logf("RFB greeting from live QEMU VNC server: %q", greeting)
}

var rfbGreetingRe = regexp.MustCompile(`^RFB \d{3}\.\d{3}\n$`)

// readGreeting reads the RFB ProtocolVersion greeting off the stream, bounded
// so a silent server cannot hang the suite (the stream has no deadline API).
func readGreeting(t *testing.T, stream io.Reader) []byte {
	t.Helper()
	type result struct {
		greeting []byte
		err      error
	}
	done := make(chan result, 1)
	go func() {
		g, err := deframeGreeting(stream)
		done <- result{g, err}
	}()
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("read RFB greeting: %v", r.err)
		}
		return r.greeting
	case <-time.After(30 * time.Second):
		t.Fatal("no RFB greeting within 30s")
		return nil
	}
}

// deframeGreeting extracts the greeting from the raw console stream. Live PVE
// delivers the stream WebSocket-FRAMED — Connect's documented contract; found
// live 2026-07-12: the first bytes are 0x82 0x0c (a FIN|binary RFC 6455 frame
// of length 12) wrapping "RFB 003.008\n" — so a single leading data-frame
// header is parsed and stripped. An unframed greeting is accepted too, in
// case a PVE version proxies the raw TCP stream.
func deframeGreeting(stream io.Reader) ([]byte, error) {
	head := make([]byte, 2)
	if _, err := io.ReadFull(stream, head); err != nil {
		return nil, fmt.Errorf("read stream head: %w", err)
	}
	if head[0] == 'R' { // unframed: head already holds the greeting's start.
		rest := make([]byte, rfbGreetingLen-len(head))
		if _, err := io.ReadFull(stream, rest); err != nil {
			return nil, fmt.Errorf("read raw greeting: %w", err)
		}
		return append(head, rest...), nil
	}
	if op := head[0] & 0x0f; head[0]&0x80 == 0 || (op != 1 && op != 2) {
		return nil, fmt.Errorf("stream starts with % x, want an RFB greeting or a FIN text/binary WebSocket frame", head)
	}
	if head[1]&0x80 != 0 {
		return nil, fmt.Errorf("server WebSocket frame is masked (head % x)", head)
	}
	payloadLen := int(head[1] & 0x7f)
	if payloadLen < rfbGreetingLen || payloadLen >= 126 {
		return nil, fmt.Errorf("WebSocket frame payload is %d bytes, want >= the %d-byte greeting", payloadLen, rfbGreetingLen)
	}
	payload := make([]byte, payloadLen)
	if _, err := io.ReadFull(stream, payload); err != nil {
		return nil, fmt.Errorf("read framed greeting: %w", err)
	}
	return payload[:rfbGreetingLen], nil
}
