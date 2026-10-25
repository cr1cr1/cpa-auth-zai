---
type: Issue
title: Reject malformed upstream SSE events
description: Prevent truncated z.ai SSE JSON from reaching OpenAI clients while preserving valid event bytes and terminal behavior.
status: draft
issue_status: done
---
# Issue 0026: Reject malformed upstream SSE events

## Problem

OpenAI Responses intermittently failed with `Error reading response: malformed server-sent event JSON`. Captured tool-call and plain-text events were truncated inside JSON strings. The plugin forwarded those malformed bytes before the client could read later valid events.

## Acceptance

- Assemble complete SSE events across arbitrary chunks and newline styles.
- Forward valid events and non-data framing with their exact original bytes.
- Drop malformed JSON data events and keep permanent sanitized debug logs.
- Continue after a malformed event when a valid terminal event arrives.
- Fail explicitly if the upstream ends without a valid terminal after a malformed event.
- Do not repair JSON, synthesize completion, or change non-streaming execution.

## Verification

- Tests failed with malformed bytes forwarded and no explicit terminal failure, then passed with only valid events forwarded.
- A later valid `response.completed` closes cleanly after a malformed event.
- A malformed terminal event without a later valid terminal produces an explicit `malformed SSE` stream error.
- Existing native Responses, Anthropic `message_stop`, redaction, cleanup, and streaming tests pass.
- `go test -race ./...` passed and `golangci-lint run` reported `0 issues`.
- The current-platform GoReleaser snapshot build passed.
- **Pending:** live z.ai recovery with the original complex Responses workloads.

## External evidence

- [Kilo Code issue 9432](https://github.com/Kilo-Org/kilocode/issues/9432) reports intermittent z.ai truncation and concatenated chunks.
- [Magpie issue 3197](https://github.com/yetone/magpie/issues/3197) documents malformed events after z.ai mid-stream failures and the later error event hidden behind them.
