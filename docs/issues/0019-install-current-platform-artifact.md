---
type: Issue
title: Install the current-platform GoReleaser artifact
description: Update the mise install task to find the local snapshot for the current OS and architecture and install it under CPA's expected name.
status: draft
issue_status: done
---
# Issue 0019: Install the current-platform GoReleaser artifact

## Problem

The mise build task now uses GoReleaser, which writes versioned binaries under `dist/plugin_<os>_<arch>_*`. The install task still references the removed `dist/cpa-auth-zai.so` path and has invalid shell text.

## Acceptance

- Select the artifact using `go env GOOS` and `go env GOARCH`.
- Support the GoReleaser `.so`, `.dylib`, and `.dll` extensions.
- Require exactly one matching binary and fail with a clear message when it is absent or ambiguous.
- Install it as `cpa-auth-zai-v<version>.<ext>` inside the required `CPA_PLUGIN_DIR`.
- Keep the existing GoReleaser build task unchanged.

## Verification

- Before the fix, `mise run install` failed with an unmatched-quote shell error while still referencing the old binary path.
- After the fix, the task selected `dist/plugin_linux_amd64_v1/cpa-auth-zai-v0.0.0-next-linux-amd64.so` and installed `cpa-auth-zai-v0.0.0-next.so` in a temporary `CPA_PLUGIN_DIR`; `cmp` confirmed an exact copy.
- A simulated Darwin/arm64 install without a matching artifact failed with the expected clear error.
- `go test -race ./...` passed and `golangci-lint run` reported `0 issues` with absolute writable caches.
- GoReleaser rebuilt the Linux/amd64 snapshot using the local Clang-only compiler substitution because Zig is unavailable. The install task then passed again against the rebuilt artifact.
- `git diff --check` passed. Only Linux/amd64 installation was exercised; Darwin and Windows selection uses the same platform mapping but was not run.
