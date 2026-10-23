---
type: Compatibility Record
title: cpa-auth-zai verified and unverified contracts
description: Evidence tiers for every external contract the plugin depends on, with the source for each claim.
status: draft
---

# Compatibility record

## Evidence tiers

- **Verified (source):** confirmed in the CPA v8 checkout used for the `replace` directive.
- **Verified (user):** confirmed by the operator with a live account request (response fixture).
- **Verified (live):** exercised against the real CPA v8 docker image (`eceasy/cli-proxy-api:latest`, 2026-10-23) without an account sign-in.
- **Reported:** third-party reverse-engineering publication; not confirmed by us or by z.ai documentation.
- **Unverified:** assumed for design; must be confirmed by a live gated check before being trusted.

## Live integration evidence (2026-10-23)

Disposable sibling of the operator's `cli-proxy-api` container (same image), isolated config/auth/plugins:

| Check | Result |
|---|---|
| Plugin load + registration in real CPA v8 binary | pass (`pluginhost: plugin registered plugin_id=cpa-auth-zai`) |
| Management API shows `supports_oauth`/`oauth_provider=zai`, `supports_quota`/`quota_provider=zai` | pass |
| `GET /v8/management/oauth/auth-url?provider=zai` against live z.ai | pass — hosted init accepted; real `chat.z.ai/api/oauth/authorize` URL returned |
| Login poll without sign-in | pass — `{"status":"wait"}`; session cancel + terminal state clean |
| History endpoint without params / public exposure | pass — 400; `/v0/resource/...` → 404 (not public) |
| Quota for unknown auth_index | pass — 404 `auth not found` |
| Unauthenticated management access | pass — 401 |
| Poll → success (needs account sign-in), executor/quota/history with real credentials | **pending operator-gated run** |

## CPA contracts

| Contract | Tier | Source |
|---|---|---|
| C ABI (exports, host struct, envelope) | Verified (source) | `sdk/pluginabi`, `examples/plugin/*/go/main.go` |
| Capability keys, schema_version 6 | Verified (source) | `internal/pluginhost/rpc_schema.go` |
| Auth start/poll flow through management endpoints | Verified (source) | `internal/api/handlers/management/auth_files_provider_oauth.go` |
| Plugin RPC wire shapes (executor, quota, management) | Verified (source) | `internal/pluginhost/rpc_schema.go`, `rpc_client_stream.go`, `stream_bridge.go`, `host_callbacks.go` |
| Plugin discovery path `plugins/<os>/<arch>/<id>-v<version>.so` | Verified (source) | `internal/pluginhost/platform.go` |
| Management-authenticated plugin routes; resources are public | Verified (source) | `sdk/pluginapi/types.go` (`ManagementRoute`, `ResourceRoute`) |

## z.ai contracts

| Contract | Tier | Consequence if wrong |
|---|---|---|
| Usage-detail endpoint `GET /api/monitor/token-usage/usage-detail?usageType=MODEL&type=1` returns the documented JSON envelope | Verified (user) — fixture `testdata/usage-detail.json` | History view breaks; decode errors surface as management 502 |
| Login token authorizes the monitor endpoints | Unverified | Quota/history return empty (`{}`) or 401-based fallbacks |
| Login token authorizes `POST /api/coding/paas/v4/chat/completions` | Unverified | Model requests fail 401; surfaced as client 401, never replaced by API-key fallback |
| Hosted polling flow: `POST zcode.z.ai/api/v1/oauth/cli/init` returns flow + live authorize URL; `GET /api/v1/oauth/cli/poll/{flow_id}` accepts polling | init + pending poll **verified (live)**; ready-completion path remains reported ([PROTOCOL.md](https://cdn.jsdelivr.net/npm/pi-zcode-provider@0.1.0/PROTOCOL.md)) | Sign-in fails with a sanitized error; no fallback exists by design |
| Business login `POST /api/auth/z/login {"token"}` mints long-lived biz JWT | Reported | Only the short-lived OAuth token is available for upstream calls |
| `quota/limit` response shape (unit/number window rows, percentage, nextResetTime) | Reported ([OpenUsage](https://openusage.sh/docs/providers/zai/)) | Quota buckets stay absent (unknown), summary empty |
| Coding Plan model set = `glm-5.3`, `glm-5.3-flash` | Verified (z.ai FAQ, 2026-10) | Model list may need update when z.ai changes plans |
| Coding Plan usage is restricted to z.ai-approved tools | Verified (z.ai FAQ, 2026-10) | Operator accepts the provider-policy risk when deploying CPA with this plugin |

## Deliberate exclusions

- No API-key listing, creation, copying, or rotation — rejected in transport and tests.
- No browser automation, no `zcode://` handler takeover, no browser-storage scraping.
- No China-region endpoints (`open.bigmodel.cn`).
- No quota reset actions.

## Upstream query-timezone caveat

`usage-detail` `startTime`/`endTime` semantics (which zone interprets the naive
datetime strings) are unverified. The plugin sends start/end converted to the
response's reported source timezone (Asia/Shanghai for the fixture). If a live
check shows different semantics, `fetchUsageDetail` is the single place to fix.
