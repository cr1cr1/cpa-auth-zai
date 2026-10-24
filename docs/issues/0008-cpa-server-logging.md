---
type: Issue
title: Send OAuth and quota events to CPA server logs
description: Add concise host.log events for OAuth and quota/usage lifecycle and sanitized failures without creating a plugin log file.
status: draft
issue_status: done
---
# Issue 0008: CPA server logging

## Scope

Send OAuth start/success/failure, quota fetch success/failure, and usage history success/failure events through CPA `host.log`. Keep pending polls quiet. Do not log tokens, credential JSON, full callback URLs, or account usage data. Do not create a plugin log file. Logging failures must not change operation results.

## Verification

- Logging acceptance tests failed before implementation and pass after it.
- Redaction tests cover OAuth, quota, and usage failure logs.
- A quota test confirms logger failure does not change the result.
- `mise run test`: pass, including the race detector.
- `mise run lint`: 0 issues.
