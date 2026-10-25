---
type: Issue
title: Route native OpenAI Responses requests
description: Detect the executor protocol from CPA request metadata and use the official z.ai Responses endpoint without translating Responses through Chat Completions.
status: draft
issue_status: done
---
# Issue 0014: Route native OpenAI Responses requests

## Problem

DeepSeek Harness uses OpenAI Responses. The plugin declares only Chat Completions, so CPA translates the request and response. A live tool request ended with `response.output_item.done` but no `response.completed`; CPA reports that a function call remains unfinished. A direct minimal z.ai Chat Completions tool stream is valid and includes `finish_reason: tool_calls` and `[DONE]`, so the failure is specific to the complex translated path.

z.ai officially supports native OpenAI Responses at `https://api.z.ai/api/v1/responses`. Using that protocol when CPA supplies it avoids the Chat Completions translation boundary.

## Acceptance

- Declare OpenAI Responses in executor input and output formats while retaining Chat Completions.
- Select the upstream endpoint from CPA executor format metadata: `openai-response` uses `https://api.z.ai/api/v1/responses`; Chat Completions and empty legacy metadata use the existing coding Chat Completions endpoint.
- Forward the selected request payload unchanged in non-streaming and streaming execution.
- Pass native Responses events, including terminal events, without synthesizing completion or adding DSH-specific behavior.
- Keep API-key authentication, optional ZCode header, redaction, and stream cleanup unchanged.
- Reject unsupported formats and mismatched payload/output formats instead of silently selecting an endpoint or emitting the wrong protocol.

## Verification

- Registration failed with only Chat Completions, native non-streaming execution failed with the chat URL, and an unsupported format incorrectly reached upstream before implementation. All passed after format routing was added.
- A mixed-format test proved that CPA can select different input and output formats when both are declared and then skip translation. The plugin rejects that mismatch with `invalid_request`/400 instead of emitting the wrong protocol. Same-format aliases remain valid.
- Native streaming preserves `response.output_item.done` followed by `response.completed` byte-for-byte and closes the upstream HTTP stream. No completion event is synthesized.
- `mise run test` passed with the race detector, `mise run lint` reported `0 issues`, `mise run build` produced `dist/cpa-auth-zai.so`, and `git diff --check` passed.
- An earlier failed minimal Responses request used a stale plugin binary. After installing the native-routing build, the same forced tool call completed with a finished `get_weather` call, usage totals, and `response.completed`. This path is live-verified; no completion is synthesized. Anthropic Messages support is tracked in [issue 0015](0015-native-anthropic-messages.md).

## Evidence boundary

The endpoint is documented in the official [GLM Coding Plan quick start](https://docs.z.ai/devpack/quick-start). CPA's `prepareExecutorCall` sets `ExecutorRequest.Format` to the selected native output format and `SourceFormat` to the selected native input format. Declaring Responses lets CPA preserve that protocol instead of translating it. When CPA selects different input and output formats, it can skip translation; this plugin rejects that mismatch because translating payloads itself is outside its scope. CPA may add usage-detail defaults on its native Responses path; this does not change event types or remove terminal events. Unit tests prove routing and pass-through only; the original complex live stream must be retested.
