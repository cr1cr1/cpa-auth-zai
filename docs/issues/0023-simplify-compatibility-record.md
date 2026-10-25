---
type: Issue
title: Simplify the compatibility record
description: Replace dense prose and tables with concise bullets while preserving compatibility evidence and caveats.
status: draft
issue_status: done
---
# Issue 0023: Simplify the compatibility record

## Acceptance

- Convert contract tables and long paragraphs to scannable bullets.
- Remove decorative emphasis and repeated wording.
- Preserve evidence tiers, dates, sources, verification status, failures, commands, paths, links, and caveats.
- Keep YAML metadata unchanged.

## Verification

- A preservation check confirmed identical link targets and YAML metadata, with all original inline code retained.
- The rewrite contains no em or en dashes and reduces the document's word count.
- `git diff --check` passed.
- `go test -race ./...` passed and `golangci-lint run` reported `0 issues`.
- The current-platform GoReleaser snapshot build passed.
