---
type: Issue
title: Send the ZCode token during inference
description: Include the login-derived ZCode JWT in the separate inference header used by the known ZCode client while keeping bearer authentication and prohibiting API keys.
status: draft
issue_status: done
---
# Issue 0012: Send the ZCode token during inference

## Problem

After the conflicting OpenAI-compatible provider was removed, CPA reached the plugin executor but `glm-5.3` returned HTTP 500. The plugin sends each login token as a standalone bearer candidate. The known ZCode client instead sends its main bearer token and the ZCode JWT in `X-Zcode-Token` on the same request.

Earlier live checks rejected standalone login tokens. The combined-header contract remains the smallest untested authentication difference.

## Acceptance

- Non-streaming and streaming inference send `X-Zcode-Token` when storage contains `zcode_jwt`.
- Bearer candidate order and fallback behavior stay unchanged.
- Requests without a ZCode JWT omit the header.
- Payloads pass through unchanged.
- No API key is created, imported, copied, or used.
- No device-identity file, plugin log file, or new configuration is added.

## Verification

- Non-streaming and streaming header assertions failed with `X-Zcode-Token = []` before implementation, then passed.
- Review added coverage for stream fallback and absent/blank JWTs; removing the fallback header made the fallback test fail on attempt two, then the implementation was restored.
- `mise run test` passed with the race detector.
- `mise run lint` reported `0 issues` with a writable linter cache.
- `mise run build` produced `dist/cpa-auth-zai.so`.
- Live inference remains unverified until the operator installs the build and retries `glm-5.3`.

## Evidence boundary

The header contract comes from the third-party [ZCode client](https://cdn.jsdelivr.net/npm/pi-zcode@0.1.4/src/client/client.ts) and [inference stream](https://cdn.jsdelivr.net/npm/pi-zcode@0.1.4/src/stream/stream.ts). It is not an official z.ai OAuth-inference guarantee. The official Coding Plan documentation still describes API-key authentication; this change does not add that prohibited path.
