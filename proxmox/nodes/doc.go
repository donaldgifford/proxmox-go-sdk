// Package nodes wraps per-node administration. Every operation is node-scoped:
// the node name is a per-call argument (the [Service] binds none), so one
// service serves the whole cluster. Construct it with [NewService] or via the
// root client's Nodes accessor; one *Service is safe for concurrent use.
//
// A few surfaces here are cluster-scoped rather than node-scoped and take no
// node argument — ACME accounts and challenge plugins are cluster config, and
// they live in this package because they exist to serve node certificates.
//
// # Networking
//
// The configured interfaces at /nodes/{node}/network — bridges, bonds, VLANs,
// and physical NICs.
//
//   - Read: [Service.ListInterfaces], [Service.GetInterface] (lossless
//     [Interface]).
//   - Write: [Service.CreateInterface], [Service.UpdateInterface],
//     [Service.DeleteInterface]. These stage changes into the pending network
//     config; they are synchronous (return an error, no task).
//   - [Service.ApplyNetworkConfig] activates the pending config. PVE may reload
//     synchronously (a zero tasks.Ref) or via a worker (a non-zero Ref to
//     await) — check tasks.Ref.IsZero.
//
// # Node configuration
//
// [Service.GetNodeConfig] and [Service.SetNodeConfig] read and write the node
// config file. Reads are lossless: the ACME keys are parsed into typed fields
// and every other key is preserved in [NodeConfig.Extra]. Writes clear nothing
// implicitly — name a key in [NodeConfigUpdate.Delete] to unset it.
//
// # Packages and disks
//
// [Service.ListAptUpdates], [Service.RefreshAptCache], and the DEB822
// repository pair [Service.ListRepositories] / [Service.UpdateRepository];
// [Service.ListDisks], [Service.GetDiskSMART], [Service.InitializeDisk]. There
// is no method for running arbitrary node scripts — PVE serves no endpoint for
// it, so use the SSH side-channel (proxmox/ssh, via the client's SSH accessor).
//
// # Certificates
//
// A node serves its API over TLS with either an uploaded certificate or one
// obtained from an ACME certificate authority.
//
// Uploaded: [Service.GetNodeCertificates], [Service.UploadCustomCertificate],
// [Service.DeleteCustomCertificate].
//
// ACME is four pieces, and they are usually assembled in this order:
//
//  1. An account with the CA — [Service.ListACMEAccounts],
//     [Service.GetACMEAccount], [Service.RegisterACMEAccount],
//     [Service.UpdateACMEAccount], [Service.DeactivateACMEAccount]. Point it at
//     a directory from [Service.ListACMEDirectories] (Let's Encrypt staging
//     while testing, so a failed order does not burn production rate limits)
//     and accept the terms from [Service.GetACMEMeta].
//  2. A challenge plugin, for DNS-01 — [Service.ListACMEPlugins],
//     [Service.GetACMEPlugin], [Service.CreateACMEPlugin],
//     [Service.UpdateACMEPlugin], [Service.DeleteACMEPlugin]. The plugin carries
//     the DNS provider's credentials; see the provider model below. HTTP-01
//     needs no plugin, and no plugin means PVE's built-in standalone challenge.
//  3. The node's wiring — [Service.SetNodeConfig] sets the ACME account
//     ([NodeACME]) and one [ACMEDomain] slot per name on the certificate, each
//     naming the plugin that answers its challenge.
//  4. The order — [Service.OrderNodeCertificate], [Service.RenewNodeCertificate],
//     [Service.RevokeNodeCertificate]. Each runs as a worker; await the returned
//     tasks.Ref. An order REPLACES the node's serving certificate.
//
// # The ACME provider model
//
// A DNS-01 plugin needs its provider's API credentials, and PVE admits 160
// acme.sh providers. Rather than model all of them, the SDK takes an
// [ACMEPluginData] — two methods, the provider name and its credential
// variables — and owns the wire encoding (base64 of sorted KEY=value lines) so
// a caller never touches base64.
//
// [ACMECloudflare] and [ACMENamecheap] are typed; [ACMERawPluginData] reaches
// every other provider, and [Service.GetACMEChallengeSchema] publishes each
// provider's field names from the node itself, so building one is discovery
// rather than guesswork. Implementing the interface for a provider of your own
// requires no change here.
//
// Credentials are credentials: the shipped types redact themselves under fmt's
// %v and %s, but a type of your own does not inherit that, and an
// [ACMEPlugin] read carries the stored blob in base64 — an encoding, not
// protection.
package nodes
