---
type: Issue
title: Log malformed upstream SSE at debug level
description: Diagnose OpenAI SDK SSE JSON failures with permanent sanitized CPA debug logs.
status: draft
issue_status: done
---
# Issue 0025: Log malformed upstream SSE at debug level

## Acceptance

- Parse upstream SSE event boundaries across arbitrary stream chunks for diagnostics only.
- Log invalid `data:` JSON through CPA `host.log` at `debug` level.
- Include a sanitized, length-capped data preview and tail, the byte length, JSON parse error, and event name when present.
- Do not log valid event data or create a plugin log file.
- Forward every original stream chunk unchanged.

## Verification

- The acceptance test failed with zero debug logs, then passed with one sanitized malformed-event log.
- The test covers a split malformed event, a valid event, exact unchanged chunks, API-key redaction, byte length, parse error, and sanitized tail.
- A follow-up truncated Responses tool-call fixture failed without the added diagnostics, then passed.
- `go test -race ./...` passed and `golangci-lint run` reported `0 issues`.
- The current-platform GoReleaser snapshot build passed.
- `git diff --check` passed.
