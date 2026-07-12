# Changelog

All notable changes to this project are documented here. The format is
based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and
this project adheres to [Semantic Versioning](https://semver.org/).

## [unreleased]

### Features

- *(pvelab)* CLI dispatch skeleton — iso/up/down/status/env (IMPL-0002 P1)
- *(pvelab)* Lab config schema + validation; promote yaml/v4 to direct
- *(pvelab)* ISO preparation over the ssh side-channel; wire cmdISO
- *(pvelab)* Per-node answer rendering + embedded answer server
- *(pvelab)* Node-VM provisioning + readiness poll
- *(pvelab)* Blast-radius-guarded teardown; wire cmdDown
- *(pvelab)* State file + env handoff; wire up/status/env
- *(pvelab)* Dogfood just recipes, gitignore entries, example config
- *(cluster)* Cluster create/join config surface
- *(mockpve)* Cluster-config emulation (create/join-info/join/nodes)
- *(pvelab)* Cluster formation wired into up (lab/cluster.go)
- *(integration)* Password-credential support (PVE_USERNAME/PVE_PASSWORD)
- *(integration)* Multi-pair topology scrub (PVE_SCRUB_EXTRA)
- *(qemu)* ConvertToTemplate with maybe-UPID hedge
- *(pvelab)* Nested.template config block (VMID sub-range 9210-9219)
- *(pvelab)* Lab.BuildTemplate/FindTemplate — template build core
- *(pvelab)* Template build subcommand
- *(pvelab)* CloneNodeVMs + serialized clone re-identify pass
- *(pvelab)* Up provisions via linked clones when the template exists
- *(integration)* Certification.yaml — first mockpve certification entry

### Bug Fixes

- *(mockpve)* Persist create-form keys into guest config
- *(pvelab)* Quote installer-supplied log values; static env-write error
- *(pvelab)* Use the modeled newline-strip sanitizer for installer log values
- *(pvelab)* Gate each cluster join on runtime quorum, not config presence
- *(console)* Dial the vncwebsocket path the ticket is bound to
- *(ha,pverr,integration)* Act on the second live inner-suite run

### Refactor

- *(pvelab)* Address style-review findings
- *(pvelab)* Apply go-style review findings on the Phase 5 surface

### Documentation

- Pvelab layout + workflow notes; mockpve is the only SHIPPED binary
- *(impl)* Check the Phase 1 lint/test/changelog gate
- *(cluster)* Promote package overview to cover the config ops
- *(impl)* Check off IMPL-0002 Phase 2 tasks 1-5 with dated notes
- *(claude)* Dogfood section covers Phase 2 cluster formation
- *(impl)* Phase 2 task 8 + success-criteria status notes
- *(testing)* Dogfood-lab walkthrough + testing-reality refresh
- *(impl)* Check off IMPL-0002 Phase 3 non-live tasks with dated notes
- *(impl)* Check off IMPL-0002 Phase 5 task 1 with dated note
- *(testing)* Template/linked-clone walkthrough + Phase 5 task 2 ledger note
- *(testing)* Certification runbook (drift -> dogfood -> refresh -> re-certify)
- *(inv)* Park INV-0003 — reusable answer-server package idea
- *(pvelab)* Record the first live formation — P2 live boxes closed
- *(impl)* Flip the Phase 3 dogfood-run box its pass note already recorded
- *(impl)* Phase 1 acceptance cycle complete — box + criteria checked

### Testing

- *(cluster)* Create/join-info/join/membership unit tests
- *(integration)* TestResourceAffinityPlacement (scheduler-observed P4)
- *(integration)* Retire TestResourceAffinityRule + PVE_TEST_HA_SIDS
- *(integration)* TestConsoleRFB (live RFB greeting over console.Connect)
- *(integration)* Land the live P4 placement cassette; close P4+P6 in the ledgers

### Miscellaneous Tasks

- *(just)* Dogfood-test + composite dogfood recipes
- *(release)* Guard .goreleaser.yml against a pvelab builds entry

## [0.2.0] - 2026-07-11

### Documentation

- *(readme)* Point badges at the SDK and fix stale scaffold content ([#5](https://github.com/donaldgifford/proxmox-go-sdk/issues/5))
- Dogfood harness docs — INV-0002, DESIGN-0002, IMPL-0002 ([#6](https://github.com/donaldgifford/proxmox-go-sdk/issues/6))

## [0.1.1] - 2026-07-07

### Documentation

- Add SECURITY, CONTRIBUTING, USAGE, and DEVELOPMENT guides

### Testing

- Live-node recording harness (go-vcr) + TESTING.md walkthrough ([#4](https://github.com/donaldgifford/proxmox-go-sdk/issues/4))

## [0.1.0] - 2026-07-03

### Features

- *(types,pverr)* Primitives + typed error taxonomy
- *(api)* Low-level transport, connection failover, credentials
- *(version)* Capability snapshot + per-minor 9.x gates
- *(tasks)* UPID decode + completion waiters
- *(mockpve)* In-memory PVE responder + standalone server
- *(proxmox)* Unified client NewClient + accessors + options
- *(qemu)* VM list/status/config/create/clone/delete service
- *(qemu)* Power ops start/stop/shutdown/reboot/suspend/resume
- *(qemu)* Migrate + disk/NIC add/resize/remove
- *(qemu)* Snapshots list/create/rollback/delete
- *(qemu)* Guest-agent exec, ping, and exec-wait
- *(lxc)* Container CRUD + power on a shared svcutil base
- *(lxc)* Container snapshots (list/create/rollback/delete)
- *(lxc)* Pull OCI images as container templates (9.1+)
- *(storage)* Datastore list/status + content listing
- *(storage,qemu)* Volume allocate/free + guest-scoped disk move
- *(storage)* Volume-chain snapshots, gated on 9.1
- *(storage,api)* Streaming ISO/disk-image upload via DoUpload
- *(ssh)* SFTP/exec side-channel for non-REST ops
- *(storage)* ZFS pool ops + RAIDZ-expansion capability gate
- *(ha)* Cluster-scoped HA resource management
- *(ha)* HA rules — node-affinity + resource-affinity
- *(ha)* CRS scheduler settings read/write
- *(ha)* Dynamic Load Balancer controls (9.2+)
- *(ha)* Arm/Disarm cluster-wide HA switch (9.2, stub)
- *(ha)* Storage/ZFS replication jobs
- *(nodes)* Node networking (Phase 5 task 1)
- *(sdn)* SDN zones, VNets, subnets + ApplySDN (Phase 5 task 2)
- *(sdn)* SDN fabrics with 9.2 protocol gate (Phase 5 task 3)
- *(sdn)* SDN live-status stubs returning ErrUnsupported (Phase 5 task 4)
- *(firewall)* Scoped firewall — cluster/node/guest (Phase 5 task 5)
- *(cluster)* Cluster resources, status, options (Phase 6 task 1)
- *(access)* Users, groups, roles, ACLs, API tokens (Phase 6 tasks 2-3)
- *(nodes)* Apt, disks/SMART, certificates + ACME node admin (Phase 6 task 4)
- *(metrics)* RRD/status reads, metric servers, OTel stub (Phase 6 task 5)
- *(ceph)* Pools, OSDs, status; RBD mirroring stub (Phase 6 task 6)
- *(pbs)* PVE-side backup jobs, vzdump, restore (Phase 6 task 7)
- *(console)* Mint console tickets + VNC Connect over DoWebSocket
- *(schemadiff)* Add pve-schemadiff API schema-drift tool (OQ-7)

### Bug Fixes

- *(security)* Address PR security-scanner findings

### Documentation

- *(proxmox)* Promote Phase 1 doc.go stubs + runnable examples
- *(qemu,lxc)* Runnable package Examples + promote doc.go overviews
- *(storage,ssh)* Runnable Examples; complete Phase 3
- *(ha)* Promote doc.go + runnable Example; complete Phase 4
- *(sdn,firewall,nodes)* Promote doc.go + node-network Example (Phase 5 task 6)
- *(examples)* Promote Phase 6 docs + success-flow examples

### Testing

- *(qemu,lxc)* End-to-end lifecycle covering the Phase 2 criterion
- *(integration)* Add build-tagged live-node harness (OQ-5)
- *(integration)* Add LXC lifecycle to cover Phase 2 "both QEMU and LXC"
- *(integration)* Cover phases 3-5 criteria in the live harness

### Miscellaneous Tasks

- *(lint)* Exclude local/generated files from yamlfmt + markdownlint
- *(tooling)* Make yamlfmt config auto-discoverable + tidy perms
- Fix Test Go / TruffleHog / Build snapshot failures
- Skip signing in the goreleaser snapshot validation

