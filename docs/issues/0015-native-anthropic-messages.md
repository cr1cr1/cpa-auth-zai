---
type: Issue
title: Route native Anthropic Messages requests
description: Add Anthropic Messages to the existing protocol-based executor routing so harnesses can call CPA models without using an OpenAI protocol.
status: draft
issue_status: done
---
# Issue 0015: Route native Anthropic Messages requests

## Problem

A harness can select Anthropic Messages when calling CPA models. The plugin currently accepts only Chat Completions and OpenAI Responses, so CPA must translate Anthropic payloads through another protocol. Native Anthropic support is required for the same format-based routing used by the other protocols.

## Acceptance

- Declare CPA's `anthropic` input and output format while retaining Chat Completions and OpenAI Responses.
- Accept `claude`, `anthropic`, and `anthropic-messages` metadata as the same protocol.
- Route native Anthropic payloads to `https://api.z.ai/api/anthropic/v1/messages` without changing the payload.
- Send `anthropic-version: 2023-06-01` with non-streaming and streaming requests.
- Preserve native Anthropic events, including `message_stop`, without synthesizing terminal events.
- Keep existing API-key authentication, optional ZCode header, mixed-format rejection, redaction, and stream cleanup.
- Record the final native OpenAI Responses live-verification result accurately.

## Verification

- Registration failed with only two formats before `anthropic` was added.
- Non-streaming Anthropic execution failed with a mixed-format error before `anthropic-messages` was normalized to `claude`; it then passed with the native URL, required version header, and unchanged payload.
- Streaming Anthropic execution failed with unsupported `claude` before implementation; it then passed with the native URL, required header, and byte-for-byte `message_stop` pass-through.
- Format-only regression tests exposed an empty `SourceFormat` being normalized before the legacy fallback. Both Responses and Anthropic now route correctly with only `Format`; unsupported-format fixtures use `gemini`.
- The full suite verifies existing Chat Completions and OpenAI Responses behavior, API-key authentication, mixed-format rejection, redaction, and stream cleanup.
- `mise run test` passed with the race detector, `mise run lint` reported `0 issues`, `mise run build` produced `dist/cpa-auth-zai.so`, and `git diff --check` passed.
- Live Anthropic Messages completion remains unverified. Native OpenAI Responses success is recorded in [issue 0014](0014-native-openai-responses.md).

## Evidence boundary

The official [Claude Code guide](https://docs.z.ai/devpack/tool/claude) specifies `https://api.z.ai/api/anthropic` and `ANTHROPIC_AUTH_TOKEN`; the native messages path is `/v1/messages`. CPA normalizes registration name `anthropic` to `claude`. Unit tests prove protocol routing and pass-through only.
