---
type: Compatibility Record
title: cpa-auth-zai verified and unverified contracts
description: Evidence tiers for every external contract the plugin depends on, with the source for each claim.
status: draft
---

# Compatibility record

## Evidence tiers

- **Verified (source):** confirmed in the public CPA v8.0.40 module, matching the previously inspected checkout at tag `v8.0.40`; required SDK packages are vendored.
- **Verified (user):** confirmed by the operator with a live account request (response fixture).
- **Verified (live):** exercised against the real CPA v8 docker image (`eceasy/cli-proxy-api:latest`, 2026-10-23) without an account sign-in.
- **Reported:** third-party reverse-engineering publication; not confirmed by us or by z.ai documentation.
- **Unverified:** assumed for design; must be confirmed by a live gated check before being trusted.

## Live gated run (2026-10-24, real account)

Completed a real hosted sign-in against the disposable CPA container and verified with live account data:

| Check | Result |
|---|---|
| Hosted sign-in → credential persisted (`zai-cristi-chiru-gmail-com.json`) | **pass** — ready in ~20 s / 4 polls |
| Storage contents | `biz_access_token` (long-lived HS512), short-lived `oauth_access_token`, `zcode_jwt`, email, user id — no API key touched |
| Monitor endpoints (`quota/limit`, `usage-detail`) authorize with… | **the business token**; the OAuth session token and zcode JWT are rejected with envelope `code: 401 "token expired or incorrect"` inside HTTP 200 |
| Live quota through CPA | **pass** — 5-hour window 96% remaining (reset same-day 12:31 UTC), 7-day 98% (reset Oct 30); the envelope-401 candidate fallback proved necessary and works |
| Live history through CPA | **pass** — totals match the user's known usage (GLM-5.3 43,484,230); `history-timezone: Europe/Bucharest` config field applied (`2026-10-19T19:00+03:00 → 2026-10-20T19:00+03:00` boundaries) |
| Token-only model inference | **fails — provider-side.** Coding endpoint, Anthropic endpoint, and zcode ultra-proxy all reject every login token (HTTP 200 envelope `code 1234 "Network error"`, or 401). Model routing and credential selection were exercised; the upstream refuses the credential class. |

Consequence: no API-key fallback was added. Quota and usage management (goal 2) are complete and live-verified. After the operator explicitly instructed the project to assume OAuth-token inference works, [issue 0010](issues/0010-enable-oauth-models.md) re-enabled model and executor registration. The existing handlers are unit-tested; no new live inference success is claimed. A new live completion remains necessary to resolve the earlier provider-side rejection evidence.

## Live executor routing (2026-10-25)

The operator confirmed that the provider-returned model catalog appears in CPA. An initial execution attempt selected CPA's native OpenAI-compatible executor because the active configuration also had an `openai-compatibility` entry named `zai`; its local `missing provider baseURL` error produced a generic invalid-API-key response. The operator removed that conflicting entry and restarted CPA.

The next `glm-5.3` attempt reached the upstream execution path and failed after 837 ms with HTTP 500 `Internal Server Error`. [Issue 0012](issues/0012-send-zcode-token-for-inference.md) added the separate `X-Zcode-Token` header used by the known ZCode client. A later minimal non-streaming request with that header still failed with the z.ai error ID `20261025062312a301da0c74d44317`. This ruled out DSH tools and payload translation as the cause and confirmed that the combined login-token header did not enable inference.

[Issue 0013](issues/0013-provision-inference-api-key.md) replaces login-token inference with the organization/project API-key flow used by the third-party ZCode client. Live login created the configured key and subsequent Responses requests reached model output. An initial terminal failure was traced to a stale plugin binary. After the native-routing build was installed, a minimal forced tool call completed with `response.completed` and usage totals. The created key's Standard/Coding Plan classification remains unverified. [Issue 0015](issues/0015-native-anthropic-messages.md) adds native Anthropic Messages as another supported protocol.

## CPA contracts

| Contract | Tier | Source |
|---|---|---|
| C ABI (exports, host struct, envelope) | Verified (source) | `sdk/pluginabi`, `examples/plugin/*/go/main.go` |
| Capability keys, schema_version 6 | Verified (source) | `internal/pluginhost/rpc_schema.go` |
| Auth start/poll flow through management endpoints | Verified (source) | `internal/api/handlers/management/auth_files_provider_oauth.go` |
| Manual callback lookup requires the authorize URL `state` to match `AuthLoginStartResponse.State`; z.ai requires its original state, so the plugin reuses it | Verified (source, operator, and regression test) 2026-10-24 | `internal/api/handlers/management/oauth_callback.go`; `auth_test.go`; replacing the state caused live z.ai authorization failure |
| Plugin RPC wire shapes (executor, quota, management) | Verified (source) | `internal/pluginhost/rpc_schema.go`, `rpc_client_stream.go`, `stream_bridge.go`, `host_callbacks.go` |
| Server logging through `host.log` with request context | Verified (source) | `internal/pluginhost/host_callbacks.go`; no plugin log file is used |
| Plugin discovery path `plugins/<os>/<arch>/<id>-v<version>.so` | Verified (source) | `internal/pluginhost/platform.go` |
| Management-authenticated plugin routes; resources are public | Verified (source) | `sdk/pluginapi/types.go` (`ManagementRoute`, `ResourceRoute`) |
| `metadata.Logo` supplies plugin-card and resource-sidebar images | Verified (source) | `sdk/pluginapi/types.go`; management-center `pluginLogo.ts` and `pluginResources.ts`; general provider branding has no plugin hook |

## z.ai contracts

| Contract | Tier | Consequence if wrong |
|---|---|---|
| Usage-detail endpoint `GET /api/monitor/token-usage/usage-detail?usageType=MODEL&type=1` returns the documented JSON envelope | Verified (user) — fixture `testdata/usage-detail.json` | History view breaks; decode errors surface as management 502 |
| Monitor endpoints authorize with the **business** token; session token rejected (envelope 401 in HTTP 200) | Verified (live) 2026-10-24 | Handled: envelope-aware bearer fallback |
| Login tokens authorize model inference (coding, anthropic, ultra-proxy paths) | **Fails — verified (live) 2026-10-24** | Executor wired and routing correctly; upstream refuses the credential class; no fallback by design |
| Inference sends `X-Zcode-Token` with the provisioned API-key bearer | Reported by the third-party ZCode client; **API key accepted and model output observed (user) 2026-10-25** | Native Responses terminal completion is verified separately; no login-token fallback exists |
| Inference sends the ZCode 3.10.2 client fingerprint, protocol-specific user agent, process session ID, and per-request UUIDs | Reported by the third-party [pi-zcode-headers capture](https://github.com/cyzlmh/pi-extensions/tree/main/pi-zcode-headers); **unit-tested, live unverified** | The gateway can reject the client surface; account APIs do not receive these headers |
| Native OpenAI Responses `POST /api/v1/responses` selected from CPA payload-format metadata | Official [Coding Plan quick start](https://docs.z.ai/devpack/quick-start), CPA source, and **verified (user) 2026-10-25** with completed tool call, usage, and `response.completed` | Same-format Responses requests avoid Chat Completions translation; mixed input/output formats are rejected to prevent wrong-protocol output; no completion is synthesized |
| Native Anthropic Messages `POST /api/anthropic/v1/messages` with `anthropic-version: 2023-06-01` | Official [Claude Code guide](https://docs.z.ai/devpack/tool/claude) and CPA source; **live unverified** | `claude`, `anthropic`, and `anthropic-messages` metadata select the native endpoint; events pass through unchanged |
| Project keys are listed by exact configured name, created when absent, and copied when masked at `/api/biz/v1/organization/{org}/projects/{project}/api_keys` | Reported by the third-party [ZCode OAuth client](https://cdn.jsdelivr.net/npm/pi-zcode@0.1.4/src/auth/oauth.ts); **key creation verified (user) 2026-10-25** | Login stops with a sanitized error; no arbitrary-key selection, rotation, or deletion |
| New keys use Coding Plan rather than Standard billing | **Unverified.** The [official guide](https://docs.z.ai/devpack/notice/api-key-management) separates key types but does not document this management API | The operator must inspect key classification and billing before relying on plan quota |
| Hosted polling flow: `POST zcode.z.ai/api/v1/oauth/cli/init` returns flow + live authorize URL; `GET /api/v1/oauth/cli/poll/{flow_id}` accepts polling, including ready completion | **Verified (live)** 2026-10-24 gated run | Sign-in fails with a sanitized error; no fallback exists by design |
| Business login `POST /api/auth/z/login {"token"}` mints long-lived biz JWT | **Verified (live)** 2026-10-24 gated run | Monitor endpoints require the business token; envelope-aware bearer fallback handles rejected candidates |
| `quota/limit` response shape (unit/number window rows, percentage, nextResetTime) | Reported ([OpenUsage](https://openusage.sh/docs/providers/zai/)) | Quota buckets stay absent (unknown), summary empty |
| Coding Plan catalog `GET /api/coding/paas/v4/models` returns `data[].id`; the business token derived from OAuth is accepted | Reported by third-party implementations; **authentication unverified** | Model discovery returns an error; no fixed-model or general-catalog fallback exists |
| Official z.ai SVG at `https://z-cdn.chatglm.cn/z-ai/static/logo.svg` | Verified (public asset) 2026-10-25 | Plugin cards and the resource sidebar show the generic icon if the browser cannot load the remote asset |
| Coding Plan usage is restricted to z.ai-approved tools | Verified (z.ai FAQ, 2026-10) | Operator accepts the provider-policy risk when deploying CPA with this plugin |

## Deliberate exclusions

- No API-key rotation, deletion, arbitrary selection, or login-token inference fallback.
- No browser automation, no `zcode://` handler takeover, no browser-storage scraping.
- No China-region endpoints (`open.bigmodel.cn`).
- No quota reset actions.

## Upstream query-timezone caveat

`usage-detail` `startTime`/`endTime` semantics (which zone interprets the naive
datetime strings) are unverified. The plugin sends start/end converted to the
response's reported source timezone (Asia/Shanghai for the fixture). If a live
check shows different semantics, `fetchUsageDetail` is the single place to fix.

## Management UI timezone (Task 5 scope evidence)

Inspected the deployed management UI build (`static/management.html`,
2026-10-23): it has **no display-timezone preference**. The only IANA timezone
setting is `claudeHeaderTimezone` (config path
`upstream.claude.header-defaults.timezone`), which cloaks `currentDate` in
Claude requests. All timestamps render through browser-locale `Intl.*` calls.

Consequence: a "UI-selected timezone" for history requires a management-UI
extension (external repository) plus a host channel to pass the selection.
Until that approved scope lands, the history endpoint takes `timezone`
explicitly. When the request omits it, the `history-timezone` config field is
the fallback; if that is empty, the source timezone applies with `provider-day`
labeling — never the server's clock.
