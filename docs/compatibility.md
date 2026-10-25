---
type: Compatibility Record
title: cpa-auth-zai verified and unverified contracts
description: Evidence tiers for every external contract the plugin depends on, with the source for each claim.
status: draft
---

# Compatibility record

## Evidence tiers

- Verified (source): confirmed in the public CPA v8.0.40 module and the inspected `v8.0.40` checkout; required SDK packages are vendored.
- Verified (user): confirmed by the operator with a live account request (response fixture).
- Verified (live): exercised against the real CPA v8 docker image (`eceasy/cli-proxy-api:latest`, 2026-10-23) without an account sign-in.
- Reported: third-party reverse-engineering publication; not confirmed by us or z.ai documentation.
- Unverified: design assumption; requires a live gated check before use.

## Live gated run (2026-10-24, real account)

A real hosted sign-in completed against the disposable CPA container. Live account data confirmed:

- Hosted sign-in: pass; credential `zai-<account>.json` persisted and was ready in ~20 s / 4 polls.
- Storage: `biz_access_token` (long-lived HS512), short-lived `oauth_access_token`, `zcode_jwt`, email, and user id; no API key touched.
- Monitor endpoints (`quota/limit`, `usage-detail`): the business token authorizes. OAuth session token and zcode JWT fail with envelope `code: 401 "token expired or incorrect"` inside HTTP 200.
- Live quota through CPA: pass; 5-hour window 96% remaining (reset same-day 12:31 UTC), 7-day 98% (reset Oct 30). The envelope-401 candidate fallback was necessary and worked.
- Live history through CPA: pass; totals matched the user's known usage (GLM-5.3 43,484,230). `history-timezone: Europe/Bucharest` applied (`2026-10-19T19:00+03:00 → 2026-10-20T19:00+03:00` boundaries).
- Token-only model inference: provider-side failure. Coding, Anthropic, and zcode ultra-proxy endpoints rejected every login token (HTTP 200 envelope `code 1234 "Network error"`, or 401). Model routing and credential selection worked; upstream refused the credential class.

Result: no API-key fallback was added. Quota and usage management (goal 2) were complete and live-verified. After the operator instructed the project to assume OAuth-token inference works, [issue 0010](issues/0010-enable-oauth-models.md) re-enabled model and executor registration. Existing handlers were unit-tested; no new live inference success was claimed. A new live completion remained necessary to resolve the provider-side rejection.

## Live executor routing (2026-10-25)

- The operator confirmed that the provider model catalog appeared in CPA.
- Initial execution selected CPA's native OpenAI-compatible executor because an `openai-compatibility` entry was also named `zai`. Its local `missing provider baseURL` error caused a generic invalid-API-key response. The operator removed the conflict and restarted CPA.
- The next `glm-5.3` request reached upstream and failed after 837 ms with HTTP 500 `Internal Server Error`. [Issue 0012](issues/0012-send-zcode-token-for-inference.md) added `X-Zcode-Token`. A minimal non-streaming request still failed with z.ai error ID `20261025062312a301da0c74d44317`, ruling out DSH tools and payload translation. Combined login-token headers did not enable inference.
- [Issue 0013](issues/0013-provision-inference-api-key.md) replaced login-token inference with the organization/project API-key flow from the third-party ZCode client. Live login created the configured key, and Responses requests reached model output. An initial terminal failure came from a stale plugin binary. With the native-routing build installed, a minimal forced tool call completed with `response.completed` and usage totals. Standard/Coding Plan classification remains unverified. [Issue 0015](issues/0015-native-anthropic-messages.md) adds native Anthropic Messages.

## CPA contracts

- C ABI (exports, host struct, envelope): Verified (source). Source: `sdk/pluginabi`, `examples/plugin/*/go/main.go`.
- Capability keys, schema_version 6: Verified (source). Source: `internal/pluginhost/rpc_schema.go`.
- Auth start/poll flow through management endpoints: Verified (source). Source: `internal/api/handlers/management/auth_files_provider_oauth.go`.
- Manual callback lookup: Verified (source, operator, and regression test) 2026-10-24. Authorize URL `state` must match `AuthLoginStartResponse.State`; the plugin reuses z.ai's original state. Source: `internal/api/handlers/management/oauth_callback.go`; `auth_test.go`; replacing the state caused live z.ai authorization failure.
- Plugin RPC wire shapes (executor, quota, management): Verified (source). Source: `internal/pluginhost/rpc_schema.go`, `rpc_client_stream.go`, `stream_bridge.go`, `host_callbacks.go`.
- Server logging through `host.log` with request context: Verified (source). Source: `internal/pluginhost/host_callbacks.go`; no plugin log file is used.
- Plugin discovery path `plugins/<os>/<arch>/<id>-v<version>.so`: Verified (source). Source: `internal/pluginhost/platform.go`.
- Management-authenticated plugin routes; public resources: Verified (source). Source: `sdk/pluginapi/types.go` (`ManagementRoute`, `ResourceRoute`).
- `metadata.Logo` supplies plugin-card and resource-sidebar images: Verified (source). Source: `sdk/pluginapi/types.go`; management-center `pluginLogo.ts` and `pluginResources.ts`; general provider branding has no plugin hook.

## z.ai contracts

- Usage-detail endpoint `GET /api/monitor/token-usage/usage-detail?usageType=MODEL&type=1` returns the documented JSON envelope: Verified (user), fixture `testdata/usage-detail.json`. If wrong, history breaks and decode errors surface as management 502.
- Monitor endpoints authorize with the business token; session token fails with envelope 401 in HTTP 200: Verified (live) 2026-10-24. Envelope-aware bearer fallback handles this.
- Login tokens authorize model inference (coding, anthropic, ultra-proxy): fails, Verified (live) 2026-10-24. Executor routing works; upstream refuses the credential class. No fallback by design.
- Inference sends `X-Zcode-Token` with the provisioned API-key bearer: Reported by the third-party ZCode client; API key accepted and model output observed (user) 2026-10-25. Native Responses terminal completion is verified separately; no login-token fallback exists.
- Inference sends the ZCode 3.10.2 client fingerprint, protocol-specific user agent, process session ID, and per-request UUIDs: Reported by the third-party [pi-zcode-headers capture](https://github.com/cyzlmh/pi-extensions/tree/main/pi-zcode-headers); unit-tested, live unverified. The gateway can reject the client surface; account APIs do not receive these headers.
- Native OpenAI Responses `POST /api/v1/responses` selected from CPA payload-format metadata: Official [Coding Plan quick start](https://docs.z.ai/devpack/quick-start), CPA source, and Verified (user) 2026-10-25 with completed tool call, usage, and `response.completed`. Same-format Responses requests avoid Chat Completions translation; mixed input/output formats are rejected to prevent wrong-protocol output; no completion is synthesized.
- Native Anthropic Messages `POST /api/anthropic/v1/messages` with `anthropic-version: 2023-06-01`: Official [Claude Code guide](https://docs.z.ai/devpack/tool/claude) and CPA source; live unverified. `claude`, `anthropic`, and `anthropic-messages` metadata select the native endpoint; events pass through unchanged.
- Project keys are listed by exact configured name, created when absent, and copied when masked at `/api/biz/v1/organization/{org}/projects/{project}/api_keys`: Reported by the third-party [ZCode OAuth client](https://cdn.jsdelivr.net/npm/pi-zcode@0.1.4/src/auth/oauth.ts); key creation verified (user) 2026-10-25. Login stops with a sanitized error; no arbitrary-key selection, rotation, or deletion.
- New keys use Coding Plan rather than Standard billing: Unverified. The [official guide](https://docs.z.ai/devpack/notice/api-key-management) separates key types but does not document this management API. The operator must inspect classification and billing before relying on plan quota.
- Hosted polling flow: `POST zcode.z.ai/api/v1/oauth/cli/init` returns flow + live authorize URL; `GET /api/v1/oauth/cli/poll/{flow_id}` accepts polling, including ready completion. Verified (live) 2026-10-24 gated run. Sign-in fails with a sanitized error if wrong; no fallback exists by design.
- Business login `POST /api/auth/z/login {"token"}` mints long-lived biz JWT: Verified (live) 2026-10-24 gated run. Monitor endpoints require the business token; envelope-aware bearer fallback handles rejected candidates.
- `quota/limit` response shape (unit/number window rows, percentage, nextResetTime): Reported by [OpenUsage](https://openusage.sh/docs/providers/zai/). If wrong, quota buckets stay absent (unknown) and summary stays empty.
- Coding Plan catalog `GET /api/coding/paas/v4/models` returns `data[].id`; OAuth-derived business token is accepted: Reported by third-party implementations; authentication unverified. If wrong, model discovery returns an error; no fixed-model or general-catalog fallback exists.
- Official z.ai SVG from `https://z-cdn.chatglm.cn/z-ai/static/logo.svg`: Verified (public asset) 2026-10-25. Downloaded as `zai.svg`, embedded in the plugin, and served at `/v0/resource/plugins/cpa-auth-zai/zai.svg`; registration and exact response bytes are unit-tested. No remote asset request is needed at runtime.
- Coding Plan usage is restricted to z.ai-approved tools: Verified (z.ai FAQ, 2026-10). The operator accepts this provider-policy risk when deploying CPA with the plugin.

## Deliberate exclusions

- No API-key rotation, deletion, arbitrary selection, or login-token inference fallback.
- No browser automation, `zcode://` handler takeover, or browser-storage scraping.
- No China-region endpoints (`open.bigmodel.cn`).
- No quota reset actions.

## Upstream query-timezone caveat

- Unverified: which zone `usage-detail` uses to interpret naive `startTime`/`endTime` datetime strings.
- Current behavior: send start/end converted to the response's reported source timezone (Asia/Shanghai for the fixture).
- If a live check disagrees, fix `fetchUsageDetail` only.

## Management UI timezone

- Inspected deployed management UI build (`static/management.html`, 2026-10-23): no display-timezone preference.
- The only IANA timezone setting is `claudeHeaderTimezone` (config path `upstream.claude.header-defaults.timezone`), which cloaks `currentDate` in Claude requests.
- All timestamps render through browser-locale `Intl.*` calls.
- A UI-selected history timezone requires a management-UI extension (external repository) and a host channel for the selection.
- Until that approved scope lands, the history endpoint takes `timezone` explicitly. The fallback is `history-timezone`, then the source timezone with `provider-day` labeling. Never use the server's clock.
