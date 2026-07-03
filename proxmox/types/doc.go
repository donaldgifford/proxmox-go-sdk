// Package types holds the primitive value types shared across the Proxmox VE
// SDK: identifiers ([VMID], [NodeName], [GuestRef]), the [PowerState] enum, and
// [PVEBool].
//
// # PVEBool
//
// Proxmox encodes booleans as 0/1 (and occasionally "yes"/"no" or true/false).
// [PVEBool] normalises all of these: it unmarshals any of those forms and
// marshals back to 1/0, so config structs can embed it directly:
//
//	type Config struct {
//		OnBoot types.PVEBool `json:"onboot"`
//	}
//
// PVEBool is public (not buried in internal/) precisely because consumers embed
// it in their own request/response structs. Call [PVEBool.Bool] to read it as a
// plain Go bool.
//
// These types are leaves imported directly by services and consumers; the SDK
// does not re-export them through the root package (DESIGN-0001 / OQ-1).
package types
