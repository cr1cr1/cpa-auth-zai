---
type: Issue
title: Keep the release checkout clean
description: Use the checked-in GoReleaser configuration directly without generating or editing files in the release workflow.
status: draft
issue_status: done
---
# Issue 0021: Keep the release checkout clean

## Acceptance

- Run all six release targets on one macOS runner with native Clang and Zig.
- Pass `.goreleaser.yml` directly to GoReleaser.
- Remove the configuration copy, target filtering, checksum renaming, and backup files from the workflow.
- Keep one checksum file for all six archives.

## Verification

- The immutable-release acceptance check failed on the matrix workflow and passed after the change.
- `goreleaser check --config .goreleaser.yml` validated the configuration.
- `go test -race ./...` passed and `golangci-lint run` reported `0 issues`.
- The current-platform GoReleaser snapshot build passed with Zig 0.17.0 and the checked-in configuration.
- `git diff --check` passed; the build added no untracked release-configuration or backup files.
- **Pending:** the single macOS release job must build all six targets in CI; local Linux verification cannot build Darwin.
