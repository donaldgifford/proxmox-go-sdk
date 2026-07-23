# Design Documents

This directory contains detailed design documents for feature implementation.

## What are Design Documents?

Design documents describe **how a feature or system will be built**. Each design
document includes:

- **Overview**: What is being designed and why
- **Goals and Non-Goals**: Scope boundaries
- **Detailed Design**: Architecture, APIs, data models
- **Testing Strategy**: How the design will be validated
- **Migration Plan**: How to roll out the changes

## Creating a New Design Document

```bash
docz create design "Your Design Title"
```

## Design Status

- **Draft**: Initial draft, still being written
- **In Review**: Ready for review and feedback
- **Approved**: Approved and ready for implementation
- **Implemented**: Design has been fully implemented
- **Abandoned**: Design was not pursued

<!-- BEGIN DOCZ AUTO-GENERATED -->

## All Design

| ID          | Title                                                                            | Status      | Date       | Author         | Link                                                                                                                                     |
| ----------- | -------------------------------------------------------------------------------- | ----------- | ---------- | -------------- | ---------------------------------------------------------------------------------------------------------------------------------------- |
| DESIGN-0001 | Proxmox SDK package layout                                                       | Draft       | 2026-06-22 | Donald Gifford | [0001-proxmox-sdk-package-layout.md](0001-proxmox-sdk-package-layout.md)                                                                 |
| DESIGN-0002 | Dogfood harness: pvelab CLI, nested cluster provisioning, and recording pipeline | Implemented | 2026-07-09 | Donald Gifford | [0002-dogfood-harness-pvelab-cli-nested-cluster-provisioning-and.md](0002-dogfood-harness-pvelab-cli-nested-cluster-provisioning-and.md) |
| DESIGN-0003 | SDN fabrics real paths, node membership, and live status                         | Implemented | 2026-07-19 | Donald Gifford | [0003-sdn-fabrics-real-paths-node-membership-and-live-status.md](0003-sdn-fabrics-real-paths-node-membership-and-live-status.md)         |
| DESIGN-0004 | HA arm and disarm, status reads, and DLB reclassification                        | Implemented | 2026-07-19 | Donald Gifford | [0004-ha-arm-and-disarm-status-reads-and-dlb-reclassification.md](0004-ha-arm-and-disarm-status-reads-and-dlb-reclassification.md)       |
| DESIGN-0005 | API coverage tracker with CI drift and fabrication guards                        | Approved    | 2026-07-19 | Donald Gifford | [0005-api-coverage-tracker-with-ci-drift-and-fabrication-guards.md](0005-api-coverage-tracker-with-ci-drift-and-fabrication-guards.md)   |

<!-- END DOCZ AUTO-GENERATED -->
