---
type: Issue
title: Use CPA release archive names
description: Package every release target as a ZIP archive without the tag's v prefix.
status: draft
issue_status: done
---
# Issue 0027: Use CPA release archive names

## Scope

CPA expects release packages named `cpa-auth-zai_0.4.1_linux_amd64.zip`. GoReleaser previously used TAR archives by default and included the tag's `v` prefix.

## Acceptance

- Every target uses ZIP, including Linux, macOS, and Windows on amd64 and arm64.
- Archive names use `.Version`, not `.Tag`, and retain the project, OS, and architecture fields.
- GoReleaser validates the configuration and a local snapshot confirms the package name and contents.

## Verification

- Before the change, configuration assertions failed because no global ZIP format was set, a Windows-only override remained, and `.Tag` supplied the version.
- After the change, the same assertions passed for the global format and all name fields.
- `goreleaser check` validated the six-target configuration.
- A Linux amd64 snapshot produced `cpa-auth-zai_0.4.1-next_linux_amd64.zip` with the plugin library, generated header, and README. No TAR archive was produced.
- `go test -race -timeout 60s ./...` passed and `golangci-lint run` reported `0 issues`.
- The two Darwin builds require the existing macOS release runner and were not run locally.
