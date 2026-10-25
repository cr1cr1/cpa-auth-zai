---
type: Issue
title: Load provider models and add the z.ai logo
description: Replace the fixed account model list with Coding Plan API discovery and expose the official z.ai logo through CPA plugin metadata.
status: draft
issue_status: done
---
# Issue 0011: Load provider models and add the z.ai logo

## Problem

The plugin returned two hardcoded models, so provider catalog changes required a plugin release. Registration also left the supported CPA logo field empty.

## Acceptance

- `model.for_auth` sends `GET https://api.z.ai/api/coding/paas/v4/models` with the business token derived from OAuth login.
- Return every nonempty `data[].id` in provider order, including IDs unknown to the plugin release.
- Do not use a fixed-model fallback, usage history, the general API catalog, or an API key.
- Rejected, empty, and invalid catalogs return errors; credential details are redacted.
- Registration uses the official `https://z-cdn.chatglm.cn/z-ai/static/logo.svg` asset.
- Static discovery remains empty.

## Verification

- The dynamic-catalog acceptance test failed with the fixed two-model response, then passed with three provider-returned models.
- Catalog failure tests cover HTTP rejection, envelope rejection, empty data, invalid JSON, missing business tokens, and credential redaction.
- The logo acceptance test failed with empty metadata, then passed with the official SVG URL.
- `mise run test` passed with the race detector.
- `mise run lint` reported `0 issues`.
- `mise run build` produced `dist/cpa-auth-zai.so`.

## Evidence boundary

The endpoint and `data[].id` schema are corroborated by third-party implementations. Business-token acceptance is implementation evidence, not an official z.ai guarantee or a captured live response. See [pi-zcode model discovery](https://cdn.jsdelivr.net/npm/pi-zcode@0.1.4/src/models/models.ts) and its [OAuth credential handling](https://cdn.jsdelivr.net/npm/pi-zcode@0.1.4/src/auth/oauth.ts). No authenticated request was made during this change.

CPA uses `metadata.Logo` on plugin cards and the plugin-resource sidebar. General provider branding uses a separate management-UI map and has no plugin registration hook.

Update 2026-10-25: [issue 0024](0024-embed-zai-logo.md) replaces the remote logo URL with an embedded `zai.svg` served from CPA.
