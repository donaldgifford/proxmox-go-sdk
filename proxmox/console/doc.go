// Package console wraps the Proxmox VE 9.x console surface: minting console
// tickets and opening a VNC console session.
//
// Every operation is node-scoped and takes the node per-call, so the Service
// binds no node. The surface splits in two:
//
//   - Minting a ticket is a plain synchronous REST call. MintVNCTicket,
//     MintSPICETicket, and MintTermProxy target a guest
//     (/nodes/{node}/{qemu|lxc}/{vmid}/{vncproxy|spiceproxy|termproxy});
//     MintNodeVNC and MintNodeTerm target the node's own shell
//     (/nodes/{node}/{vncshell|termproxy}). Each returns a ticket carrying the
//     one-time credential and (for VNC/term) the proxy port. These are fully
//     exercised against mockpve.
//   - Connect dials the vncwebsocket endpoint for a VNC ticket over a WebSocket
//     upgrade and returns the raw duplex byte stream. The bytes are the live PVE
//     VNC console protocol (WebSocket-framed RFB); the SDK carries them verbatim
//     and does not interpret them — a VNC client on top is the caller's concern.
//
// SPICE has no SDK-side dial: hand the SPICETicket parameters to a SPICE client
// (remote-viewer). VerifyVNCTicket has no standalone PVE REST endpoint and
// returns pverr.ErrUnsupported — a ticket is verified when Connect presents it
// to the upgrade.
//
// Because no live 9.x node or recorded cassette is available in this
// environment, the Connect plumbing is verified against mockpve's echo upgrade
// only; the RFB payload on the wire is unverified here.
//
// Construct a Service with NewService or the root client's Console accessor; one
// *Service is safe for concurrent use.
//
// See docs/design/0001-proxmox-sdk-package-layout.md and
// docs/impl/0001-proxmox-ve-9x-sdk-coverage.md.
package console
