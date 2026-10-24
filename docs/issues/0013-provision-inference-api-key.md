---
type: Issue
title: Provision the inference API key
description: Reuse or create a named z.ai project API key after OAuth login, store it in CPA, and use it for chat-completions inference.
status: draft
issue_status: done
---
# Issue 0013: Provision the inference API key

## Problem

Live inference rejects every login-derived token. The known ZCode client resolves an organization/project API key after OAuth login and uses that key for inference. The plugin currently stops after storing login tokens.

## Acceptance

- Add the `api-key-name` string config field with default `cpa-auth-zai`.
- After OAuth login, list keys in the default organization and first project and select only an exact name match.
- If no key matches, create one with the configured name.
- Read an unmasked returned secret or fetch it through the copy endpoint. Store the combined `id.secret` API key in CPA credential storage.
- Stop login with a redacted error when key provisioning fails or organization/project context is absent. Do not create a duplicate after a list or copy failure.
- Non-streaming and streaming inference use the stored API key as bearer and do not fall back to login tokens. Keep the ZCode header when available.
- Existing credentials without an API key return a clear re-login error. Model discovery, quota, and usage history keep their current login-token behavior.
- No key rotation, deletion, arbitrary key selection, native OpenAI-compatible provider, callback listener, dependency, or file logger.

## Verification

- Existing exact-name reuse failed with no `api_key`, then passed with `key-id.key-secret` in login storage. Config registration failed before the field was added, then passed with the default and trimmed custom name.
- Configured-name creation and masked-secret copy each failed before their implementation, then passed. Non-streaming and streaming inference failed with login-token bearers before API-key selection, then passed with the stored key.
- Failure tests cover missing project context, missing default organization, `id`-only organization responses, no creation after list/copy failure, old-credential re-login errors, and no login-token inference fallback.
- A deterministic concurrent test produced two create calls before provisioning was serialized and one afterward.
- Review regression tests failed for unredacted host errors, create-response key material, stream read/emit errors, and missing HTTP stream cleanup before those paths were fixed.
- `mise run test` passed with the race detector, `mise run lint` reported `0 issues`, `mise run build` produced `dist/cpa-auth-zai.so`, and `git diff --check` passed.
- Live key provisioning, key classification, billing behavior, and inference remain unverified until the operator signs in again and tests the account.

## Evidence boundary

The list/create/copy schema comes from the third-party [ZCode OAuth client](https://cdn.jsdelivr.net/npm/pi-zcode@0.1.4/src/auth/oauth.ts). The [official key-management guide](https://docs.z.ai/devpack/notice/api-key-management) separates Standard and Coding Plan keys but does not specify this private management API. Live verification must check both inference and key classification; unit tests cannot prove billing behavior.
