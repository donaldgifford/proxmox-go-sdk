// Package access wraps PVE access control: users, groups, roles, ACLs, and API
// tokens, all under the 9.x privilege model. It is cluster-scoped — every
// endpoint lives under /access and binds no node. Construct the [Service] with
// [NewService] or via the root client's Access accessor; one *Service is safe
// for concurrent use.
//
//   - Users, groups, roles: full CRUD ([Service.CreateUser], [Service.ListRoles],
//     …). Roles carry 9.x privileges — VM.Replicate and granular
//     VM.GuestAgent.* — not the removed VM.Monitor. [Role] normalises the two
//     PVE role-read shapes (a CSV list entry vs a privilege→1 object).
//   - ACLs: [Service.ListACLs] and [Service.SetACL] (one PUT grants; set
//     [ACLSpec].Delete to revoke).
//   - API tokens (under a user): [Service.CreateToken] and
//     [Service.RegenerateTokenSecret] return the one-time [TokenSecret] — store
//     it immediately. [Service.ClearTokenComment] needs PVE 9.1 and
//     [Service.RegenerateTokenSecret] needs 9.2; below those they return a
//     pverr.ErrUnsupported-wrapped error.
//
// All config writes are synchronous (they return an error, not a task). Reads
// are lossless — unmodelled keys are preserved in each type's Extra map.
package access
