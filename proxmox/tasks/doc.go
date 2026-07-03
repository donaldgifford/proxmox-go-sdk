// Package tasks decodes PVE worker identifiers (UPIDs) and waits on the
// asynchronous tasks they name. PVE has no event push, so every long-running
// operation (clone, migrate, backup, …) returns a UPID the caller polls.
//
// # Refs and UPIDs
//
// A mutating service operation returns a [Ref] (node + UPID). [ParseUPID]
// decodes the colon-delimited UPID into its fields, and [NewRef] builds a Ref
// straight from a UPID string.
//
// # Waiting
//
// [Service] polls over an [github.com/donaldgifford/proxmox-go-sdk/proxmox/api.Client]:
//
//	ref, _ := qemu.Clone(ctx, 9000, spec)        // returns tasks.Ref
//	st, err := tasksvc.Wait(ctx, ref)            // blocks until the UPID exits
//
// [Service.Wait] backs off between polls (see [WaitPolicy]) until the task
// exits; a non-OK exit returns an *pverr.Error wrapping pverr.ErrTaskFailed,
// carrying the UPID, exit status, and the tail of the task log. Context
// cancellation stops the poll. [Service.WaitFor] generalises Wait to an
// arbitrary status predicate; [Service.Status] and [Service.Log] are the
// one-shot reads underneath.
package tasks
