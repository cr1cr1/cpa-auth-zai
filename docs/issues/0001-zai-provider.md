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

Implemented and integration-tested (2026-10-23). The plugin builds as `cpa-auth-zai-v0.1.0.so`, loads in the real CPA v8 docker image, and starts live z.ai hosted sign-in flows. [Issue 0010](0010-enable-oauth-models.md) enables the existing account-model and OAuth-executor registration after the operator instructed the project to assume OAuth-token inference works. [Issue 0011](0011-provider-model-catalog-logo.md) replaces the fixed model pair with provider API discovery and adds the official plugin logo. The [plan](</mnt/linux2/dev/cpa-auth-zai/docs/superpowers/plans/2026-10-23-zai-provider.md>) and [compatibility record](</mnt/linux2/dev/cpa-auth-zai/docs/compatibility.md>) track what is verified.

## Completion

Gated run done (2026-10-24): sign-in, quota windows, and history verified live against the real account. That run rejected login-token inference on every tested endpoint. Goal 2 (quota/usage management) is complete. Goal 1 is implemented and enabled under the operator's explicit assumption that OAuth-token inference works; a new live model completion is still required before closing that evidence gap. The no-API-key constraint remains unchanged.
