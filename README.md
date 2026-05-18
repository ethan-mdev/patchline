# Patchline

Patchline is a Go CLI and library for publishing and applying static file updates for games and desktop apps.

It scans a build directory, stores files by content hash, writes release and channel manifests, and gives clients enough information to repair or update an install directory from ordinary static hosting.

Patchline is early, but the local core is usable: you can publish a build to a local release directory and apply that release from an HTTP file server.

## Why

Small teams often need a reliable update path before they need a full launcher, hosted patch service, or package-manager-grade update framework. Patchline focuses on the patching substrate:

- deterministic build scanning
- SHA-256 file hashing
- content-addressed object storage
- versioned release manifests
- mutable channel manifests
- client-side update planning
- hash verification before files are installed
- atomic replacement of changed files

Applications keep control of their own UI, auth, launch behavior, installation flow, and product-specific decisions.

## Current Status

Implemented:

- Manifest v1 structs and validation
- Nested directory scanning with deterministic ordering
- SHA-256 hashing and content-addressed object keys
- Local filesystem storage backend
- Local publish orchestration
- `patchline publish`
- `patchline apply`
- Go client primitives for fetching, planning, and applying updates
- Optional client manifest verifier hook for signing integration
- Runnable local apply example under `examples/local_apply`

Not implemented yet:

- Manifest signing and built-in signature verification
- S3-compatible storage backend
- Channel promotion, rollback, garbage collection, and doctor commands
- Configuration file support

## Quick Start

Publish a build directory:

```powershell
go run ./cmd/patchline publish `
  --app-id com.example.game `
  --version 1.0.0 `
  --channel beta `
  --output ./release-output `
  ./dist
```

Serve the published release directory:

```powershell
python -m http.server 8080 --directory ./release-output
```

Apply the beta channel into an install directory:

```powershell
go run ./cmd/patchline apply `
  --app-id com.example.game `
  --channel beta `
  --base-url http://localhost:8080 `
  --install-dir ./install
```

Use `--json` on publish or apply for machine-readable output.

There is also a runnable end-to-end example:

```powershell
go run ./examples/local_apply
```

## Storage Layout

Patchline publishes releases as static files:

```text
objects/sha256/ab/cd/<hash>
releases/1.0.0/manifest.json
channels/beta/manifest.json
```

Objects are immutable and addressed by SHA-256. Release manifests are versioned snapshots. Channel manifests are movable pointers that tell clients which release is current for a channel such as `beta` or `stable`.

## Library Usage

The CLI is a thin wrapper around Go packages under `pkg/`:

- `pkg/manifest` defines release metadata and object keys.
- `pkg/patch` scans directories and hashes files.
- `pkg/publisher` publishes a build through a storage backend.
- `pkg/storage` defines backend interfaces.
- `pkg/client` fetches manifests, builds update plans, and applies changed files.

## Development

Run the test suite:

```powershell
go test ./...
```

Patchline currently targets local development first. The next major pieces are signed manifests, S3-compatible publishing, and the operational commands needed for real release workflows.
