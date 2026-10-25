---
type: Issue
title: Inject the plugin version at build time
description: Replace the fixed release version with the cleaned SemVer from the current Git tag while keeping a simple untagged fallback.
status: draft
issue_status: done
---
# Issue 0018: Inject the plugin version at build time

## Problem

The plugin version is a constant, so builds cannot replace it through the Go linker. Release metadata can become inconsistent with the Git tag.

## Acceptance

- Keep `0.1.0` as the fallback for local builds.
- Make `pluginVersion` a linker-overridable variable.
- GoReleaser injects its cleaned `.Version` value, so tag `v1.2.3` registers version `1.2.3`.
- Leave the existing mise build task unchanged.
- Add no dependency, version file, code generator, or release machinery.

## Verification

- Before implementation, `TestPluginVersionCanBeOverridden` failed to compile because a constant cannot be assigned or replaced by the linker.
- After changing the declaration to a variable, the test passed and confirmed that registration uses the overridden value.
- `go test -race ./...` passed and `golangci-lint run` reported `0 issues` with absolute writable caches.
- `goreleaser check --config .goreleaser.yml` validated the release config.
- `goreleaser build --snapshot --clean --single-target` reached compilation but Zig was unavailable. The same GoReleaser config with only the Linux compiler changed to Clang built `dist/plugin_linux_amd64_v1/cpa-auth-zai-v0.0.0-next-linux-amd64.so`.
- The built binary contains `0.0.0-next`, confirming linker injection through GoReleaser. A tagged release was not created; the normal tag workflow with Zig was not run.
- The user's independent mise edit was not changed or included in this issue.
