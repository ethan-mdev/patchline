# Patchline

Patchline is a Go CLI and library for publishing and applying static file updates for games and desktop apps.

It scans a build directory, stores files by content hash, writes release and channel manifests, and gives clients enough information to repair or update an install directory from ordinary static hosting.

Patchline is early, but the local core is usable: you can publish a build to a local release directory and apply that release from an HTTP file server.

## Why

Small teams often need a reliable update path before they need a full launcher, hosted patch service, or package-manager-grade update framework. Patchline focuses on the patching substrate:

- deterministic build scanning
- SHA-256 file hashing
- content-addressed object storage
- Ed25519 manifest signing
- versioned release manifests
- mutable channel manifests
- client-side update planning
- manifest signature verification
- hash verification before files are installed
- atomic replacement of changed files

Applications keep control of their own UI, auth, launch behavior, installation flow, and product-specific decisions.

## Current Status

Implemented:

- Manifest v1 with detached payload signing (`{payload, signature}` envelope)
- Nested directory scanning with deterministic ordering
- SHA-256 hashing and content-addressed object keys
- Local and S3-compatible storage backends
- Ed25519 key generation, manifest signing, and verification
- `patchline keygen`, `publish`, `apply`, `verify`, `promote`, `rollback`, `gc`, `doctor`
- YAML configuration with `${VAR}` env interpolation (`patchline.yaml`)
- Go client primitives for fetching, planning, and applying updates
- Runnable local apply example under `examples/local_apply`

Not implemented yet:

- `init` and `scan` helper commands
- Glob include/exclude patterns for build scanning
- Terraform and GitHub Actions OIDC examples

## Quick Start

Generate an Ed25519 signing keypair:

```powershell
go run ./cmd/patchline keygen `
  --private-out ./patchline.key `
  --public-out ./patchline.pub
```

Publish a build directory:

```powershell
go run ./cmd/patchline publish `
  --app-id com.example.game `
  --version 1.0.0 `
  --channel beta `
  --output ./release-output `
  --signing-key ./patchline.key `
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
  --public-key ./patchline.pub `
  --install-dir ./install
```

Use `--json` on any command for machine-readable output. Unsigned manifests are rejected by default; use `--unsigned-dev` only for local development.

A `patchline.yaml` next to the CLI can supply defaults for `app_id`, `channel`, `base_url`, `install_dir`, key paths, and backend settings. `${VAR}` placeholders are expanded from the environment, so CI can pass a signing-key path through a secret without writing it to the repo.

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
- `pkg/signing` generates Ed25519 keys and signs/verifies manifests.

## Development

Run the test suite:

```powershell
go test ./...
```

Patchline currently targets local development first. The next major pieces are S3-compatible publishing and the operational commands needed for real release workflows.
