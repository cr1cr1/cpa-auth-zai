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

Remaining before closing: one operator-gated account run (complete a sign-in, then verify token-only model execution, quota windows, and history) plus confirmation that the login token authorizes each endpoint. A working quota reader alone does not close this issue.
