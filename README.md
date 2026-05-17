# Dispatch

Dispatch is a planned open-source patch publishing and update toolkit for indie games and desktop applications.

The project is being reset from an older backend scaffold into a CLI and Go library that developers can plug into their own launcher, installer, bootstrapper, or custom update flow. The current source of truth is [files/patching_platform_handoff.md](files/patching_platform_handoff.md).

## Goal

Dispatch should make it practical for small teams to ship signed updates without running a custom patch server.

The intended v1 shape:

- Publisher CLI for local and CI/CD release workflows.
- Signed release and channel manifests.
- Content-addressed file storage.
- Local and S3-compatible publishing backends.
- Go client primitives for planning and applying updates.
- Operational docs for rollback, promotion, cleanup, and deployment.

## Non-goals

Dispatch is not:

- A launcher generator.
- An admin dashboard.
- A hosted SaaS.
- A user/account/entitlement system.
- A game installer.
- A TUF replacement.

Applications remain responsible for their own UX, auth, game launch behavior, installer strategy, and product-specific workflow. Dispatch owns the patching substrate.

## Why not TUF?

TUF is the right tool for ecosystems that need delegated trust, root metadata, threshold signatures, formal key rotation, and package-manager-grade supply-chain guarantees.

Dispatch aims at a smaller threat model: signed manifests, immutable file objects, hash verification, channel promotion/rollback, and static hosting for indie-scale desktop apps. Projects that need TUF's full compromise-recovery model should use TUF or a TUF-based updater.

## Planned Storage Layout

Dispatch v1 should use content-addressed storage from day one:

```text
objects/sha256/ab/cd/<hash>
releases/1.2.3/manifest.json
channels/stable/manifest.json
```

Manifests map application paths such as `bin/game.exe` to immutable object keys. This gives cheap deduplication, straightforward garbage collection, and clean cache behavior.

## Planned CLI

Candidate commands:

```text
dispatch init
dispatch scan ./build
dispatch publish ./build --version 1.2.3 --channel beta
dispatch promote 1.2.3 --from beta --to stable
dispatch rollback --channel stable --to 1.2.2
dispatch verify --channel stable
dispatch gc --keep 5
dispatch doctor
```

## Status

Phase 1 local core is underway.

The old backend scaffold has been removed. The current implementation includes:

- Manifest v1 structs and validation.
- Nested directory scanning with deterministic ordering.
- SHA-256 hashing and content-addressed object keys.
- Local filesystem storage backend.
- Local publish orchestration.
- A minimal `dispatch publish` CLI.