---
type: Issue
title: Inject ZCode fingerprint headers for inference
description: Send the captured ZCode client identity, platform, session, and request headers on every z.ai model inference call.
status: draft
issue_status: done
---
# Issue 0017: Inject ZCode fingerprint headers for inference

## Problem

Model inference sends authentication and content headers but omits the ZCode client fingerprint. The z.ai Coding Plan gateway can use these headers to identify the approved client surface.

## Acceptance

- Inject the captured ZCode 3.10.2 fingerprint into streaming and non-streaming Chat Completions, OpenAI Responses, and Anthropic Messages inference.
- Use AI SDK provider-utils `4.0.39` for OpenAI protocols and `4.0.27` for Anthropic in the ZCode user agent.
- Include client identity, release channel, platform, OS, language, and timezone headers.
- Reuse one valid UUID session ID per plugin process and generate fresh valid UUID query, request, and trace IDs for each inference call.
- Keep the provisioned API-key bearer, optional `X-Zcode-Token`, Anthropic version, payload handling, terminal events, redaction, and cleanup unchanged.
- Do not add configuration, dependencies, a device-identity file, or fingerprint headers to login, model discovery, quota, or usage APIs.

## Verification

- Before implementation, streaming and non-streaming acceptance tests failed because the fingerprint headers were absent and `Accept` did not include `text/event-stream`.
- After implementation, Chat Completions and OpenAI Responses verify the `4.0.39` user agent; Anthropic Messages verifies `4.0.27` and retains `anthropic-version: 2023-06-01`.
- Tests verify every fixed identity header, non-empty platform/OS/timezone values, UUID v4 syntax, one stable process session ID, and fresh query/request/trace IDs across calls.
- Existing API-key, ZCode-token, payload, streaming, terminal-event, redaction, and cleanup tests pass. Call-graph review confirms that only non-streaming and streaming inference invoke fingerprint generation.
- `mise run test` passed with the race detector, `mise run lint` reported `0 issues`, `mise run build` produced `dist/cpa-auth-zai.so`, and Go formatting and `git diff --check` passed.
- Live z.ai acceptance of the injected fingerprint remains unverified. macOS and Windows artifacts were not exercised in this change.

## Evidence boundary

The fixed header set and protocol-specific user agents come from the third-party [pi-zcode-headers capture](https://github.com/cyzlmh/pi-extensions/tree/main/pi-zcode-headers) of ZCode 3.10.2. The [pi-zcode client](https://cdn.jsdelivr.net/npm/pi-zcode@0.1.4/src/client/client.ts) independently confirms the core source headers. This is not an official z.ai integration guarantee. The plugin uses the pinned `3.10.2` fallback identity and Node.js major `24`; it does not inspect installed ZCode applications or create a device identity.
