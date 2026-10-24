---
type: Issue
title: Enable OAuth account models and execution
description: Advertise the existing z.ai model and executor handlers so CPA binds account models and routes chat-completions requests.
status: draft
issue_status: done
---
# Issue 0010: Enable OAuth account models and execution

## Problem

Registration exposed only auth, quota, and management capabilities. CPA therefore never called the existing model and executor handlers, and the credential page showed no models.

Earlier live inference attempts failed. The operator explicitly instructed the project to assume OAuth-token inference works, so this change enables the existing path without claiming a new live inference result.

## Acceptance

- Registration advertises `model_provider` and `executor`.
- Executor scope is `oauth`; input and output formats are `chat-completions`.
- CPA binds `glm-5.3` and `glm-5.3-flash` to each valid z.ai credential through `model.for_auth`.
- Static discovery remains empty; account models come only from authenticated records.
- Execution uses login-derived tokens only. No API-key path exists.
- Auth, quota, usage history, and logging behavior stay unchanged.

## Verification

- The registration acceptance test failed because `model_provider` was absent, then passed after the capability change.
- Existing model and executor tests exercise the handlers that CPA now reaches.
- `mise run test` passed with the race detector.
- `mise run lint` reported `0 issues`.
- `mise run build` produced `dist/cpa-auth-zai.so`.

## Evidence boundary

CPA source confirms that these capabilities bind per-auth models and register the OAuth executor. No new live z.ai inference request was made. The 2026-10-24 provider-side rejections remain recorded in the compatibility record.
