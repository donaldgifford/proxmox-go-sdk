// Package pverr is the Proxmox VE SDK error taxonomy. Every SDK failure
// resolves to a single rich [*Error] wrapping one of the package sentinels, so
// callers branch with errors.Is / errors.As rather than string-matching.
//
// # Sentinels
//
// [ErrNotFound], [ErrConflict], [ErrUnauthorized], [ErrTicketExpired],
// [ErrForbidden], [ErrTaskFailed], [ErrUnsupported], and [ErrTransient] are the
// branchable categories. [Error] carries the operation, request path, HTTP
// status, PVE message, per-parameter validation errors, and (for task failures)
// the UPID:
//
//	if err := c.Tasks().Wait(ctx, ref); err != nil {
//		if errors.Is(err, pverr.ErrTaskFailed) {
//			var pe *pverr.Error
//			errors.As(err, &pe) // pe.UPID, pe.Message
//		}
//	}
//
// # Classification
//
// The transport calls [Classify] (HTTP status + PVE body → sentinel) and
// [ClassifyNetError] (dial/timeout → ErrTransient) so services never map status
// codes themselves. [NewTaskFailed] builds the ErrTaskFailed error for a non-OK
// task exit.
//
// Import the package aliased as pverr to avoid shadowing the stdlib errors
// package (the import path's last element is the package name).
package pverr
