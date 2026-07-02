package console

import (
	"context"
	"fmt"
	"io"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/internal/svcutil"
)

// Connect opens the VNC console byte stream for a ticket minted by MintVNCTicket
// or MintNodeVNC. It dials /nodes/{node}/vncwebsocket?port=…&vncticket=… over a
// WebSocket upgrade and returns the raw post-handshake duplex stream; PVE
// verifies the one-time ticket during the upgrade, so an invalid or expired
// ticket surfaces here as an error.
//
// The bytes on the returned stream are the live PVE VNC console protocol
// (WebSocket-framed RFB). The SDK carries them verbatim and does not de-frame or
// interpret them — a VNC/RFB client on top is the caller's concern. Close the
// stream to release the connection. Because the wire format cannot be exercised
// without a live 9.x node, the plumbing here is verified against mockpve's echo
// upgrade only; the RFB payload itself is unverified in this environment.
func (s *Service) Connect(ctx context.Context, node string, ticket *VNCTicket) (io.ReadWriteCloser, error) {
	if ticket == nil {
		return nil, fmt.Errorf("console.Connect: %w", svcutil.ErrNilSpec)
	}
	switch {
	case ticket.Ticket == "":
		return nil, fmt.Errorf("console.Connect: ticket: %w", svcutil.ErrMissingField)
	case ticket.Port == "":
		return nil, fmt.Errorf("console.Connect: port: %w", svcutil.ErrMissingField)
	}
	stream, err := s.c.DoWebSocket(ctx, vncWebSocketPath(node, ticket.Port, ticket.Ticket))
	if err != nil {
		return nil, fmt.Errorf("console.Connect: %w", err)
	}
	return stream, nil
}
