---
type: Issue
title: Enable streaming for streaming executor requests
description: Ensure every executor.execute_stream call asks z.ai for a streaming response, even when the payload omits stream or sets it to false.
status: draft
issue_status: done
---
# Issue 0016: Enable streaming for streaming executor requests

## Problem

The streaming executor relays upstream chunks but forwards the request payload unchanged. If its JSON payload omits `stream` or sets `stream` to `false`, z.ai can return a non-streaming response through the streaming transport.

## Acceptance

- Set `stream` to `true` for Chat Completions, OpenAI Responses, and Anthropic Messages requests handled by `executor.execute_stream`.
- Preserve all other request fields and keep already-streaming payloads unchanged.
- Keep `executor.execute` non-streaming and preserve its payload byte-for-byte.
- Retain native terminal events, authentication, error redaction, and stream cleanup.

## Verification

- Before implementation, `TestExecuteStreamForcesUpstreamStreaming` failed for all three protocols: Chat Completions and OpenAI Responses omitted `stream`, while Anthropic Messages retained `stream: false`.
- After implementation, the acceptance test passes. It verifies `stream: true` and preservation of every other field. Existing native Responses tests verify that an already-streaming payload remains byte-for-byte unchanged.
- `TestStreamingPayloadRequiresObject` verifies sanitized `invalid_request`/400 errors for empty, `null`, and array payloads.
- Existing non-streaming tests still verify byte-for-byte payload forwarding; terminal-event, authentication, redaction, and cleanup tests pass.
- `mise run test` passed with the race detector, `mise run lint` reported `0 issues`, `mise run build` produced `dist/cpa-auth-zai.so`, and Go formatting and `git diff --check` passed.
- Live z.ai traffic was not run for this change. Automated tests verify the upstream request contract, not a live provider response.
