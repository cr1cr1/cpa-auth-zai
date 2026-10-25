---
type: Issue
title: Simplify the README
description: Replace dense prose with concise bullets while preserving technical details, commands, configuration, and links.
status: draft
issue_status: done
---
# Issue 0022: Simplify the README

## Acceptance

- Shorten long paragraphs and dense table cells.
- Use bullets for features, build steps, release details, login provisioning, and logging.
- Preserve technical claims, verification status, commands, configuration, and link targets.

## Verification

- A preservation check confirmed identical fenced code blocks and link targets, with all original inline code retained.
- The rewrite contains no em or en dashes and reduces the README's word count.
- `git diff --check` passed.
- `go test -race ./...` passed and `golangci-lint run` reported `0 issues`.
- The current-platform GoReleaser snapshot build passed.
