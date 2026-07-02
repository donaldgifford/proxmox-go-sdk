package console

import (
	"context"
	"fmt"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/pverr"
)

// VerifyVNCTicket would independently validate a VNC ticket.
//
// Proxmox VE 9.x exposes no standalone REST endpoint to verify a console ticket:
// a vncticket is checked server-side when it is presented to the vncwebsocket
// upgrade, and there is no confirmed /nodes/{node}/... call that validates one in
// isolation. Rather than fabricate a path that would 404 against a real node,
// VerifyVNCTicket returns a pverr.ErrUnsupported-wrapped error. To confirm a
// ticket, dial it with Connect — an invalid or expired ticket fails the upgrade.
// The signature is stable, so this becomes a real call if PVE ever exposes a
// verification endpoint.
func (*Service) VerifyVNCTicket(_ context.Context, _, _ string) error {
	return fmt.Errorf(
		"console.VerifyVNCTicket: PVE 9.x has no standalone ticket-verify REST "+
			"endpoint; verify by dialling Connect: %w", pverr.ErrUnsupported,
	)
}
