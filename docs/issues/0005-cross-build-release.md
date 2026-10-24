---
type: Issue
title: Build plugin libraries for every release target
description: Configure GoReleaser and CI to cross-build the cgo shared plugin for Linux, macOS, and Windows on amd64 and arm64.
status: in_progress
issue_status: in_progress
---
# Issue 0005: Cross-build the plugin with GoReleaser

## Scope

Use the existing OS/architecture matrix in `.goreleaser.yml` for the C-ABI plugin. Build with `CGO_ENABLED=1` and `-buildmode=c-shared`. Ubuntu builds Linux and Windows with Zig; macOS builds Darwin with native Clang. GoReleaser publishes each runner's artifacts to the tag release, serialized with `max-parallel: 1`. Include unique versioned library names.

## Acceptance

- GoReleaser configuration validates.
- Runner-specific snapshots succeed without a local CPA checkout or module downloads: Ubuntu covers four Linux/Windows targets; macOS covers two Darwin targets. Linux-local verification covers the four non-Apple targets; Darwin requires the macOS runner.
- Linux artifacts contain `.so`, macOS artifacts contain `.dylib`, and Windows artifacts contain `.dll`; each archive includes the generated header and README.
- Artifact names include the plugin ID, release version, OS, and architecture; per-runner checksum files cover all six archives without overwriting each other.
- Existing race tests and lint remain green.

## Verification

- `goreleaser check` validated the complete six-target configuration.
- Linux amd64/arm64 and Windows amd64/arm64 snapshot releases passed with `GOPROXY=off` and vendored modules. All four archives contain the target library, generated header, and README; `sha256sums.txt` has four entries.
- The Linux artifacts use GNU libc because the initial musl build did not load on the glibc host. The replacement amd64 library passes native `dlopen` and exports all four CPA C ABI symbols. ARM64 ELF and Windows PE32+ formats were inspected.
- `mise run test` passed and `mise run lint` reported `0 issues`.
- **Pending:** the two Darwin builds have not run locally. Ubuntu builds the other four targets, while the `macos-latest` matrix job builds both Darwin targets with native Clang. Close this issue after a macOS snapshot or tag run confirms both `.dylib` archives.
