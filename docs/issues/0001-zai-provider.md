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

Planning only. The [implementation plan](</mnt/linux2/dev/cpa-auth-zai/docs/superpowers/plans/2026-10-23-zai-provider.md>) records inspected CPA mechanisms, proposed tasks, tests, and unresolved provider/UI contracts. No product code, live login attempts, account changes, or implementation approval exist.

## Completion

Both original goals need acceptance evidence: token-only model access and integrated quota/usage. A working quota reader alone does not close this issue. Follow the plan's approval and stop conditions before implementation.
