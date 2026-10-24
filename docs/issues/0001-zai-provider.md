---
type: Issue
title: Add a z.ai provider and quota plugin to CPA
description: Track the planned login-token-only provider and quota integration, including remote login and UI-selected timezone behavior.
status: draft
issue_status: planned
---
# Issue 0001: Z.ai provider and quota plugin

## Scope

Create a CPA plugin in this workspace for browser authentication, account-specific models and execution, and quota/usage integration. Target the latest supported CPA API, currently v8 in the supplied checkout, and pin the selected revision when implementation starts. Support remote servers and containers. Use the management UI's selected timezone. Do not create or substitute API keys.

## Current state

Implemented and integration-tested (2026-10-23). The plugin builds as `cpa-auth-zai-v0.1.0.so`, loads in the real CPA v8 docker image, registers auth/model/executor/quota/management capabilities, and starts live z.ai hosted sign-in flows. Unit tests (47) and race runs pass; the [plan](</mnt/linux2/dev/cpa-auth-zai/docs/superpowers/plans/2026-10-23-zai-provider.md>) and [compatibility record](</mnt/linux2/dev/cpa-auth-zai/docs/compatibility.md>) track what is verified.

## Completion

Gated run done (2026-10-24): sign-in, quota windows, and history verified live against the real account. Remaining before closing: z.ai does not authorize model inference with login tokens on any known endpoint (coding, anthropic, ultra-proxy all refuse the credential class) — the plan stop condition fired. Goal 2 (quota/usage management) is complete; goal 1 stays open pending either z.ai enabling login-token inference or an explicit operator decision to relax the no-API-key constraint.
