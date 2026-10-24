---
type: Issue
title: Replace Makefile with mise.toml tasks
description: Build/test/lint tooling moves from make targets to mise tasks with pinned go and golangci-lint.
status: done
issue_status: done
---
# Issue 0002: Replace Makefile with mise.toml

Makefile deleted; `mise.toml` now owns tools (go 1.27, golangci-lint 2.15) and tasks (`test`, `lint`, `fmt`, `build`, `install`, `clean`). README updated to `mise run ...`. The build task also fixes the cgo header cleanup: the emitted file is `dist/cpa-auth-zai.h`, which the old Makefile's `dist/cpa-auth-zai.so.h` pattern never matched. golangci-lint surfaced one staticcheck finding in `executor.go`, fixed.
