---
type: Issue
title: Simplify plugin installation
description: Copy the selected binary to a simple plugin filename without install-time renaming logic.
status: draft
issue_status: done
---
# Issue 0020: Simplify plugin installation

## Acceptance

- Replace the destination-name calculation and `install` command with one `cp` command.
- Copy the selected binary to `${CPA_PLUGIN_DIR}/cpa-auth-zai.${ext}`.
- Keep platform selection unchanged.

## Verification

- `mise run install` copied the selected Linux/amd64 binary to `cpa-auth-zai.so`; `cmp` confirmed an exact copy.
- The task contains no destination-name calculation and uses `cp`, not `install`.
- `go test -race ./...` passed and `golangci-lint run` reported `0 issues`.
- GoReleaser built the current-platform artifact with the configured Zig after explicit writable cache directories were supplied.
- `git diff --check` passed.
