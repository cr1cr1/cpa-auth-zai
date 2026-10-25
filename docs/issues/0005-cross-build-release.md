---
type: Issue
title: Build plugin libraries for every release target
description: Configure GoReleaser and CI to cross-build the cgo shared plugin for Linux, macOS, and Windows on amd64 and arm64.
status: in_progress
issue_status: in_progress
---
# Issue 0005: Cross-build the plugin with GoReleaser

## Scope

Use the existing OS/architecture matrix in `.goreleaser.yml` for the C-ABI plugin. Build with `CGO_ENABLED=1` and `-buildmode=c-shared`. One macOS runner builds Darwin with native Clang and Linux/Windows with Zig. GoReleaser uses the checked-in configuration directly and publishes all artifacts to the tag release. Include unique versioned library names.

## Acceptance

- GoReleaser configuration validates.
- A macOS snapshot succeeds without a local CPA checkout or module downloads and covers all six targets. Linux-local verification covers the four non-Apple targets; Darwin requires the macOS runner.
- Linux artifacts contain `.so`, macOS artifacts contain `.dylib`, and Windows artifacts contain `.dll`; each archive includes the generated header and README.
- Artifact names include the plugin ID, release version, OS, and architecture; one checksum file covers all six archives.
- Existing race tests and lint remain green.

## Verification

- `goreleaser check` validated the complete six-target configuration.
- Linux amd64/arm64 and Windows amd64/arm64 snapshot releases passed with `GOPROXY=off` and vendored modules. All four archives contain the target library, generated header, and README; `sha256sums.txt` has four entries.
- The Linux artifacts use GNU libc because the initial musl build did not load on the glibc host. The replacement amd64 library passes native `dlopen` and exports all four CPA C ABI symbols. ARM64 ELF and Windows PE32+ formats were inspected.
- `mise run test` passed and `mise run lint` reported `0 issues`.
- **Pending:** the two Darwin builds have not run locally. The single `macos-latest` job builds all six targets with native Clang and Zig. Close this issue after a macOS snapshot or tag run confirms both `.dylib` archives.
