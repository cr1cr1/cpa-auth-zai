---
type: Implementation Plan
title: Z.ai authentication, model access, and quota plugin
description: A source-backed plan for a CPA plugin using login-derived credentials, with remote login and management-UI timezone support.
status: draft
generated:
  by: planning-agent/gpt-6-astra
  at: 2026-10-23T18:12:29+03:00
sources:
  - id: requirements
    resource: User requirements in this session through 2026-10-23
    title: Agreed requirements and planning-only instruction
  - id: cpa
    resource: file:///mnt/linux2/dev/CLIProxyAPI
    title: CPA checkout at d946c4fba6fc9a5cfbca1f3d85184d62ad2fb683
  - id: polling-report
    resource: https://cdn.jsdelivr.net/npm/pi-zcode-provider@0.1.0/PROTOCOL.md
    title: Third-party ZCode protocol analysis; not independently verified
  - id: quota-report
    resource: https://openusage.sh/docs/providers/zai/
    title: Third-party account usage integration; API-key authentication is not adopted here
  - id: provider-policy
    resource: https://docs.z.ai/devpack/faq
    title: Official Coding Plan restrictions and endpoints
---
# Z.ai Provider Implementation Plan

> **For agentic workers:** After approval to implement, use `executing-plans` or `subagent-driven-development` task by task. Checkboxes below describe future work, not completed implementation.

**Goal:** Add one CPA plugin that authenticates a z.ai account, offers its accessible models, and exposes its quota and usage through CPA.

**Architecture:** One Go C-ABI shared library provides authentication, account-specific models, execution, and quota. CPA owns login status, credential persistence, routing, and the management UI. A protected history API is added only through a supported host extension; it must receive the UI's selected timezone.

**Tech Stack:** The latest supported CPA public plugin API/ABI, cgo, the Go toolchain required by that CPA revision, and the Go standard library. The inspected baseline is CPA v8 with Go 1.26.10. Use CPA transport and protocol translation instead of a separate browser service or HTTP stack.

**Spec:** The Requirements section below is the self-contained specification derived from the conversation.

## Status (2026-10-24)

- Tasks 1–4 implemented and unit-tested; Task 6 packaging uses `mise run build` and the README install steps.
- Live integration verified in a disposable sibling of the operator's CPA v8 container: registration, live login start/pending-poll/cancel, management-only history route, quota 404 semantics.
- Review hardening (poll-credential server-side only, flex-number quota decode, sanitized usage errors, https authorize check, expiry clamp, stream-fallback tests) committed as 63ffe22.
- Task 5 remains open at the documented scope gate: the deployed management UI has no timezone preference to supply (see docs/compatibility.md); extending it is external-repository work needing approval.
- Gated account run completed 2026-10-24: sign-in, quota, and history verified live; token-only model execution hit the plan stop condition — z.ai rejects login tokens on all known model endpoints (see docs/compatibility.md). No API-key fallback exists.
- Review follow-up [issue 0003](</mnt/linux2/dev/cpa-auth-zai/docs/issues/0003-provider-review-fixes.md>) disables model and executor registration until a live token-only completion succeeds. Login, quota, and history remain active; refresh requires a new browser sign-in.

## Requirements

1. Create the plugin in `/mnt/linux2/dev/cpa-auth-zai`; treat the supplied CPA checkout as reference code.
2. Authenticate through z.ai's browser login. Passwords, MFA, and the choice of login channel remain with z.ai.
3. Minimize user actions. Support a remote CPA server or container with the browser on another machine.
4. A pasted final callback URL is an acceptable recovery path when authorization has actually issued a code.
5. **Do not create API keys.** Do not list, copy, rotate, import, or otherwise substitute account API keys as an undocumented fallback. Use login-derived tokens only.
6. Present models accessible to the authenticated account and execute requests with that account's login-derived credentials. Usage history is not an entitlement list.
7. Integrate quota and usage into CPA, using the supplied usage-detail endpoint and existing quota capabilities. Do not replace CPA with a separate dashboard.
8. Use the timezone selected in the management UI, not the server/container timezone. Preserve the meaning of source reporting intervals.
9. Current authorization is **planning only**: no login attempts, credential capture, API-key operations, account changes, dependency installation, product scaffolding, or product-code edits.

### Non-goals and limits

- No China-region provider, billing changes, plan changes, quota reset actions, custom desktop URI registration, browser automation, or browser-storage scraping.
- No silent switch to a paid/general API endpoint if Coding Plan access fails.
- No claim that a session token authorizes model execution until evidence proves it.
- No generic failover framework. Implement one verified automated login mechanism and, if viable, CPA's manual callback recovery.
- Do not expand the release into images, audio, or other model modalities without an account-supported protocol and an agreed requirement.

## Global Constraints

- Provider identifier: `zai`. Plugin identifier: `cpa-auth-zai`. These are proposed names, not existing registrations.
- Target the latest supported CPA API at implementation time, currently v8 in the supplied checkout. Recheck the module path, management routes, public plugin interfaces, ABI, and schema against concrete source before implementation; pin the selected revision for reproducible builds. Do not infer a successor version or retain obsolete APIs merely to match examples.
- Versioned paths and signatures below document the inspected v8 baseline. If the selected current API differs, update the plan and its tests before implementing affected tasks. No backward-compatibility layer for v7 or deprecated v0 management routes is planned.
- Use the selected revision's ABI and schema constants; their versions are independent of the CPA API major. The inspected values are ABI 1 and schema 6.
- Keep secrets in CPA's credential storage and private, short-lived login state. Return only the public authorization URL and opaque state to the browser.
- Use host-mediated HTTP, cancellation, proxy settings, and stream cleanup. Redact authorization headers, codes, tokens, and upstream error bodies from logs and errors; verify host request logging also redacts them.
- Admit outbound credential-bearing requests only to the verified z.ai origins and paths. Reject off-origin redirects; never fetch a pasted callback URL.
- Use PKCE when supported by the verified flow; always enforce flow correlation, expiry, and single consumption. Do not invent provider PKCE support.
- Follow CPA's payload ordering: translate and normalize first, apply configured payload rules once at the final semantic stage, then serialize/send without changing business fields.
- Do not add arbitrary transport deadlines after upstream connection establishment contrary to CPA rules. Credential acquisition may have a deadline; all requests must support cancellation.
- Preserve existing CPA and workspace changes. Any later implementation needs an issue record and a rollback check.
- External CPA/backend or management-UI changes require a separately identified scope and approval. Do not modify deprecated v0 routes to work around missing v8 support.

## Baseline and Evidence

### TaskStartSnapshot

- Workspace was empty and not a Git repository before these planning documents.
- CPA revision inspected: `d946c4fba6fc9a5cfbca1f3d85184d62ad2fb683`.
- CPA already has a staged change to [.gitignore](</mnt/linux2/dev/CLIProxyAPI/.gitignore>) and an untracked [.ignore](</mnt/linux2/dev/CLIProxyAPI/.ignore>); neither was changed by this planning task.
- No Graft index exists for the new workspace. Inspection used AST outlines, bounded searches, and targeted reads.
- Change necessity: the inspected plugin contracts provide extension points; no built-in z.ai integration was established by this bounded inspection.
- Architecture review required before implementation: yes, for secret handling, model authorization, and the management-UI timezone/history connection.

### CPA patterns to use

| Concern | Source and applicable behavior |
|---|---|
| Callback and pasted-URL login | [Antigravity login](</mnt/linux2/dev/CLIProxyAPI/sdk/auth/antigravity.go#L53-L158>) starts a flow, waits for callback or manual input, validates state, then exchanges a code. Reuse the pattern, not Antigravity's provider credentials, port, or endpoints. |
| Remote/headless login | [Codex device flow](</mnt/linux2/dev/CLIProxyAPI/sdk/auth/codex_device.go#L64-L125>) and [polling](</mnt/linux2/dev/CLIProxyAPI/sdk/auth/codex_device.go#L171-L225>) show browser authorization separate from server polling, cancellation, and expiry. Provider-specific pending codes must come from z.ai evidence. |
| Plugin login ownership | [ServePluginAuthURL](</mnt/linux2/dev/CLIProxyAPI/internal/api/handlers/management/auth_files_oauth_callback.go#L164-L219>) registers state and server-side metadata. [GetAuthStatus](</mnt/linux2/dev/CLIProxyAPI/internal/api/handlers/management/auth_files_provider_oauth.go#L955-L1029>) calls plugin polling and saves completed auth records. |
| Manual callback submission | [v8 callback adapter](</mnt/linux2/dev/CLIProxyAPI/internal/api/handlers/management/oauth_siwc.go#L315-L348>) delegates ordinary plugin flows to [callback validation](</mnt/linux2/dev/CLIProxyAPI/internal/api/handlers/management/oauth_callback.go#L46-L138>), which accepts `redirect_url`. |
| Callback handoff | [Callback-file contract](</mnt/linux2/dev/CLIProxyAPI/internal/api/handlers/management/oauth_sessions.go#L434-L496>) writes code/state/error to a protected file. [Plugin polling](</mnt/linux2/dev/CLIProxyAPI/internal/pluginhost/auth_provider.go#L332-L355>) passes metadata and AuthDir, not the callback code itself. The plugin must consume the matching callback file if using this flow. |
| Current endpoints | [v8 route registration](</mnt/linux2/dev/CLIProxyAPI/internal/api/server_management_v8.go#L43-L65>) exposes OAuth start/status/cancel and per-plugin quota reads. |
| Per-account models | [ModelProvider](</mnt/linux2/dev/CLIProxyAPI/sdk/pluginapi/types.go#L400-L443>) receives account storage and returns models. Static models alone do not establish availability. |
| Execution and transport | [ProviderExecutor and HostHTTPClient](</mnt/linux2/dev/CLIProxyAPI/sdk/pluginapi/types.go#L670-L684>) define execution and host transport ownership. |
| Quota | [QuotaProvider](</mnt/linux2/dev/CLIProxyAPI/sdk/pluginapi/types.go#L1616-L1662>) and [quota result](</mnt/linux2/dev/CLIProxyAPI/sdk/pluginapi/types.go#L1693-L1767>) support subscription, summary metrics, remaining fractions, and reset timestamps. |
| ABI | [Plugin ABI methods](</mnt/linux2/dev/CLIProxyAPI/sdk/pluginabi/types.go#L5-L115>) include auth, model, executor, quota, and host HTTP callbacks. |

### Important integration gaps

1. **Token-only inference is unverified.** Public integration examples often obtain a separate API key. That approach is explicitly excluded here. A successful quota request is not proof of model access.
2. **Automated polling is a candidate, not a fact established locally.** A third-party report describes ZCode-hosted `POST /api/v1/oauth/cli/init` and `GET /api/v1/oauth/cli/poll/{flow_id}`. Do not assume it is a stable public contract, uses standard device-grant semantics, or grants model access.[^polling-report]
3. **Rejected and unreachable redirects differ.** Manual paste can recover an issued code from an unreachable destination. It cannot recover a code that z.ai never issued because the redirect URI was rejected.
4. **The plugin does not receive UI timezone today.** [HostConfigSummary](</mnt/linux2/dev/CLIProxyAPI/sdk/pluginapi/types.go#L207-L218>) and the quota request have no such field. No TypeScript/Vue management-UI source was found in this CPA checkout. Trace the actual deployed UI before specifying its files or preference API.
5. **History has no native quota result field.** Native summary/buckets can show totals and limits, but not the supplied daily arrays or trends. [Custom management routes](</mnt/linux2/dev/CLIProxyAPI/sdk/pluginapi/types.go#L1445-L1490>) are documented as v0-only, and browser resources are not management-authenticated. Do not expose account usage through those public resources.
6. **Remaining quota is not present in the supplied fixture.** A third-party integration identifies `/api/monitor/usage/quota/limit`, but its documented authentication uses an API key. Verify token access and response semantics separately.[^quota-report]
7. **Provider usage conditions apply.** z.ai's FAQ restricts Coding Plan use to supported tools. Technical success does not establish support for CPA; record this as a deployment condition rather than imitate another application's identity.[^provider-policy]

## Proposed Design

### Authentication

Use CPA's existing start/status/cancel UI and host session storage. The preferred candidate is provider-hosted polling, modeled on CPA's device flows: the browser completes login on z.ai while CPA polls over outbound HTTPS. Keep a separate random CPA state and upstream polling secret. Respect the upstream poll interval and expiry; repeated UI status requests must not cause overlapping polls or token exchanges.

If a supported authorization-code flow is established instead, use CPA's callback endpoint and manual callback submission. Record the exact accepted redirect URI at start and use that same value in token exchange. Bind the callback to the pending provider/state, consume it once, and remove only its own verified callback file. Do not register or take over `zcode://`.

CPA persists the completed `AuthData`. Store tokens with their purposes and provider-supplied expiry/refresh information, not as assumed interchangeable strings. Renew only through a verified refresh mechanism; otherwise return an actionable sign-in-required state. Restarting CPA may require a new pending login, but must preserve already saved credentials. Cap pending sessions and expire abandoned state without requiring further browser polls.

### Models and requests

`model.static` returns no availability claim without account evidence. `model.for_auth` returns the verified account-accessible catalog, filtered by CPA exclusions and aliases. Do not derive availability from usage history or a general public catalog.

Use the verified upstream model protocol and login token. Prefer an existing CPA-supported format if the endpoint uses it; do not build a speculative chat-web protocol. Support normal and streaming text/tool exchanges only to the extent demonstrated by that protocol. Preserve cancellation, upstream HTTP status, usage reporting, and payload rules. Reject unsupported operations explicitly; never return invented token counts or mock responses.

### Quota and usage

Use `quota.describe` with `SupportedProviders: ["zai"]` and `SupportsReset: false`. `quota.fetch` returns only measured values. Missing quota is unknown, not zero remaining, unlimited, or a guessed percentage. `quota.reset` returns a clear unsupported error and makes no upstream call.

Use the supplied history endpoint:

```text
GET https://api.z.ai/api/monitor/token-usage/usage-detail
    ?startTime=<encoded datetime in the verified upstream query timezone>
    &endTime=<encoded datetime in the verified upstream query timezone>
    &usageType=MODEL
    &type=1
```

The response's `timezone` describes its reporting data; it does not by itself prove how the API interprets the unqualified `startTime`/`endTime` query strings. Establish query timezone and boundary semantics before converting UI ranges.

Validate HTTP status and the JSON `code`/`success` envelope. Preserve raw numeric precision and convert only at the CPA display boundary. Pair each model's arrays with `xTime`, validate lengths, and count repeated summaries only once. Cached input is already part of input; total tokens already includes input and output.

The user sample is the acceptance fixture for 2026-10-17 through 2026-10-23: total 43,826,245; GLM-5.3 43,484,230; GLM-5.3-Flash 342,015; cache hit rate 0.9780; off-peak rate 1.0000. It reports `granularity: DAY`, `timezone: Asia/Shanghai`, `code: 200`, and `success: true`.

Preserve these fixture details for implementation without another account request:

| Model/date | Uncached input | Cached input | Input | Output | Total |
|---|---:|---:|---:|---:|---:|
| GLM-5.3 / 2026-10-20 | 862246 | 42476544 | 43338790 | 145440 | 43484230 |
| GLM-5.3-Flash / 2026-10-18 | 100493 | 237888 | 338381 | 3634 | 342015 |

Each model's arrays have seven entries aligned with the seven consecutive `xTime` dates; all other entries are zero. Model codes are `glm-5.3` and `glm-5.3-flash`, with sort order 1 and 2. The model summary list repeats the totals, not additional usage. Summary string values/trends are: cache `0.9780` / `-0.0058`; off-peak `1.0000` / `0.0604`; total `43826245` / `-0.8901`; daily average `6260892.14` / `-0.8901`. Trends remain provider-defined until their comparison period is known.

### UI timezone and protected history API

The existing management UI remains the timezone source of truth. The proposed history request contains `auth_index`, `start`, `end`, and `timezone`; `timezone` is the UI's selected IANA zone, and `start`/`end` are unambiguous RFC3339 instants. Use an internal half-open range `[start,end)` and adapt to upstream precision only after boundary semantics are established.

The proposed result contains `requestedTimezone`, `sourceTimezone`, `granularity`, `aggregationBasis`, explicit interval boundaries, summary values, and per-model series. `aggregationBasis` distinguishes `ui-local` from `provider-day`. These are proposed plugin DTOs, not existing CPA fields.

Prefer upstream timezone-aware grouping if supported. Otherwise regroup sufficiently detailed timestamped data. If only Shanghai daily totals exist, preserve each full source-day interval and render its boundaries in the UI timezone with a provider-day label. Do not split, prorate, or relabel those totals as local-day totals. Exact local-day reporting remains unavailable until sufficient source data exists.

Do not invent a timezone query parameter for z.ai. Do not fall back to the server timezone or browser-detected zone when an explicit UI selection exists. Validate zones server-side and use a packaged timezone database suitable for minimal containers.

Native quota reset timestamps should remain timezone-independent instants; CPA's UI formats them in its selected zone. Adding history requires a protected v8 plugin route and a way for the UI to supply its selection. If the deployed host lacks these, stop at the scope gate and request approval for the smallest host/UI extension. Do not describe the full UI requirement as delivered by a plugin alone.

## File Structure for Future Implementation

All following paths are proposed and do not yet contain product code. Keep a single Go package initially; split only at the existing responsibility boundaries below.

| Planned files | Responsibility |
|---|---|
| `go.mod`, `Makefile`, `main.go`, `abi.go`, `abi_test.go` | Pinned CPA v8 dependency, shared-library build, C exports, registration, RPC dispatch, shutdown. |
| `transport.go`, `transport_test.go` | Host HTTP callback adapter, allowed destinations, secret redaction, cancellation and stream ownership. |
| `auth.go`, `auth_test.go` | `AuthProvider` implementation, bounded login states, callback or polling, credential parsing and refresh. |
| `models.go`, `models_test.go` | Per-account model discovery and exclusion/alias compatibility. |
| `executor.go`, `executor_test.go` | `ProviderExecutor`, verified upstream protocol, streams, errors, and usage. |
| `quota.go`, `quota_test.go` | `QuotaProvider`, normalized limits and numeric summaries. |
| `usage.go`, `usage_test.go` | History decoding, precise totals, range/zone validation and aggregation metadata. |
| `management.go`, `management_test.go` | Protected history endpoint only after the v8 route/timezone scope gate passes. |
| `testdata/usage-detail.json`, additional redacted fixtures | User-supplied history sample and subsequently verified protocol examples. |
| `README.md`, `docs/compatibility.md` | Build/install, auth behavior, restrictions, acceptance evidence, recovery and rollback. |

## Review Focus

- One token purpose accidentally used for another: authorization failures must not trigger key creation or paid-endpoint fallback. Task 2 and Task 3 tests.
- Two accounts or browser tabs completing concurrently: state, credentials, catalog, and quota must remain isolated. Task 2 and Task 4 tests.
- Abandoned or replayed login: no repeated code exchange, credential resurrection after cancellation, or unbounded state. Task 2 tests.
- UI timezone differs from server timezone, or crosses DST: preserve real interval boundaries and source totals. Task 5 tests.
- Stream cancellation, logging, and public resources: close resources and disclose no tokens or account history. Task 1, Task 3, and Task 5 tests.

## Implementation Tasks — Execute Only After Approval

### Task 1: Establish the verified contracts and minimal plugin boundary

**Files:** `docs/compatibility.md`, `go.mod`, `Makefile`, `main.go`, `abi.go`, `abi_test.go`, `transport.go`, `transport_test.go`.

**Consumes:** This plan and the pinned CPA public API. **Produces:** `handleRPC(ctx context.Context, method string, payload json.RawMessage) (json.RawMessage, error)` and a host-backed implementation of `pluginapi.HostHTTPClient`.

- [ ] Identify the latest supported CPA API from current source and release/migration information. Confirm module, routes, plugin interfaces, ABI/schema, and Go requirements; pin its revision and update this plan's baseline references and tests if they changed.
- [ ] Record exact OAuth URLs, accepted redirect/client arrangement or polling contract, token purposes, model endpoint/protocol, quota response shape, and UI integration owner. Mark source-only and live evidence separately. Live testing requires new authorization; no private credentials belong in the record.
- [ ] Resolve the token-only inference gate before writing an executor. If no permitted login-token path is supported, stop that part and report it; do not replace it with API-key provisioning or claim the full goal works.
- [ ] Resolve the protected v8 history route and UI timezone connection before writing `management.go`. External changes need a separate scoped approval and exact file map.
- [ ] Add ABI/transport tests: registration IDs/capabilities, unknown method errors, schema 6 JSON handling, exact provider routing, rejection of key-management paths/off-origin redirects, redacted errors, and cancellation cleanup.
- [ ] Implement the smallest shared-library adapter using CPA v8 types and current examples. Do not copy example tokens, static success responses, v7 imports, or fake models.
- [ ] Run `go test ./...` and `CGO_ENABLED=1 go build -buildmode=c-shared -o dist/cpa-auth-zai.so .`; verify the library loads in a disposable matching CPA environment without live credentials.

**Done:** A loadable ABI boundary with no production auth/model/quota success stubs. No unresolved protocol assumptions are hidden as defaults.

### Task 2: Implement login, persistence, and renewal

**Files:** `auth.go`, `auth_test.go`, redacted auth fixtures.

**Consumes:** Task 1 transport. **Produces:** A `Provider` type implementing `pluginapi.AuthProvider`: `Identifier`, `ParseAuth`, `StartLogin`, `PollLogin`, and `RefreshAuth` with CPA's exact request/response signatures.

- [ ] Add `TestLoginRemotePolling`, `TestCallbackStateMismatch`, `TestCallbackReplay`, `TestConcurrentLogins`, `TestLoginExpiry`, `TestPollInterval`, `TestLoginCancel`, `TestRestartCredentialParsing`, `TestRefreshRequiresVerifiedMechanism`, and `TestLoginNeverUsesAPIKeys`.
- [ ] Assert that public login responses contain only URL/state; invalid, expired, cancelled, or replayed flows cannot save credentials; concurrent status calls exchange at most once; refresh preserves account identity; no key-management request is made.
- [ ] Implement the selected verified flow through CPA start/poll. Keep callback recovery only if the selected flow can actually issue a callback code. Handle callback-file state/path/size validation and single consumption when applicable.
- [ ] Validate CPA cancellation behavior with the plugin: the ABI has no per-login cancel method. Prefer request-driven polling, expire private state, and verify cancellation cannot publish credentials after a pending poll completes. Escalate any required host fix instead of silently patching CPA.
- [ ] Run the named tests, `go test -race ./...`, and host-level mocked login integration through `/v8/management/oauth/auth-url`, `/oauth/status`, `/oauth/callback`, and `/oauth/session` as applicable.

**Done:** Local and remote mocked flows save only login-derived credentials through CPA, with deterministic expiry and safe recovery.

### Task 3: Implement account models and request execution

**Files:** `models.go`, `models_test.go`, `executor.go`, `executor_test.go`, redacted model/protocol fixtures.

**Consumes:** Task 2 credentials and Task 1 transport. **Produces:** `pluginapi.ModelProvider` and `pluginapi.ProviderExecutor` implementations using the pinned CPA signatures.

- [ ] Add `TestModelsAreAccountScoped`, `TestHistoryIsNotEntitlement`, `TestUnauthenticatedModelsEmpty`, and tests for revoked access, aliases/exclusions, and cross-account isolation.
- [ ] Add request/stream tests for tool calls, chunk boundaries, cancellation, malformed upstream data, 401/403/429 status preservation, final payload rules, usage accounting, and explicit unsupported operations.
- [ ] Implement only the verified catalog and inference protocol. Do not publish a known public model list as account entitlement. Keep token failures separate from model-not-found and quota exhaustion.
- [ ] Run `go test ./...`, `go test -race ./...`, and mocked client requests through CPA. Later live verification must cover both a minimal normal request and a stream, with explicit approval because model requests consume quota.

**Done:** Tests prove routing and protocol behavior; live account access remains separately labeled until measured. A quota-only integration does not complete this task.

### Task 4: Implement quota and usage decoding

**Files:** `quota.go`, `quota_test.go`, `usage.go`, `usage_test.go`, `testdata/usage-detail.json`, redacted quota fixture.

**Consumes:** Account credentials and host transport. **Produces:** `pluginapi.QuotaProvider`, `decodeUsageDetail(raw []byte) (UsageDetail, error)`, and `UsageDetail` containing source timezone, granularity, precise summary values, model series, and dates.

- [ ] Add `TestUsageSampleTotals`: total 43,826,245, model totals 43,484,230 and 342,015, cache rate 0.9780, off-peak rate 1.0000. Assert no double counting of cached input, repeated summaries, or model totals.
- [ ] Add tests for mismatched array lengths, absent days, decimal strings, non-finite/invalid numbers, JSON business errors despite HTTP 200, inconsistent totals, and preserved provider-defined trends.
- [ ] Add `TestMissingQuotaIsUnknown`, `TestZeroRemainingIsExhausted`, `TestQuotaResetUnsupported`, and `TestQuotaAccountIsolation`. Never synthesize a reset timestamp or remaining percentage from usage totals.
- [ ] Implement the supplied usage-detail request with encoded parameters and token-only authorization. Implement remaining-limit normalization only from a verified response; omit unknown buckets rather than misreport them.
- [ ] Run `go test ./...` and mocked `GET /v8/management/plugins/cpa-auth-zai/quota?auth_index=<id>` requests. Prove wrong-provider records cannot expose another account's data.

**Done:** Native quota results and decoded history are accurate for known fields, with unavailable fields explicitly unavailable.

### Task 5: Connect history and the UI-selected timezone

**Files:** `usage.go`, `usage_test.go`, `management.go`, `management_test.go`; any external host/UI files must first be listed and approved under Task 1.

**Consumes:** Task 4 `UsageDetail` and the UI-selected zone. **Produces:** `UsageQuery { AuthIndex string; Start, End time.Time; Timezone string }`, `UsageView` with the fields specified in the design, and `buildUsageView(detail UsageDetail, query UsageQuery) (UsageView, error)`.

- [ ] Add `TestUsesSelectedUIZoneNotServerZone`, `TestDSTRange`, `TestUnknownTimezone`, `TestProviderDayNotRelabeled`, `TestUsageRequiresManagementAuth`, and `TestHistoryRangeValidation`.
- [ ] Assert that 2026-10-17 00:00 Asia/Shanghai renders as 2026-10-16 19:00 Europe/Bucharest, with the same source-day token total; do not turn it into a Bucharest calendar-day total. Include Europe/Bucharest's 2026-10-25 DST transition.
- [ ] Implement range conversion and grouping only to the precision the provider supports. Reject reversed/unbounded ranges and missing/invalid UI zones. Use CPA's existing range defaults and maximums after identifying them, not a new plugin-specific default.
- [ ] Connect the protected history route to CPA's existing quota UI. Verify switching the selected zone updates dates/reset displays without changing account totals or making data public.
- [ ] Run Go tests plus the deployed management UI's native checks and a browser-level integration test using fixtures. Repeat with a container configured to UTC and the UI set to Europe/Bucharest.

**Done:** CPA's selected timezone controls display; exact local-day grouping is either supported by sufficient data or clearly unavailable. No public resource returns account data. If host/UI scope is unapproved, this task remains incomplete.

### Task 6: Package, verify, and document rollback

**Files:** `Makefile`, `README.md`, `docs/compatibility.md`, integration tests.

**Consumes:** Tasks 1–5. **Produces:** A versioned shared-library artifact and installation/recovery instructions for the exact CPA baseline.

- [ ] Document Go/cgo, target architecture, libc compatibility, plugin configuration, persistent CPA auth storage, and restart behavior. Do not introduce a companion service or expose extra callback ports for polling.
- [ ] Run `gofmt` checks, `go vet ./...`, `go test ./...`, `go test -race ./...`, the shared-library build, and a disposable CPA load/reload test. Verify active streams and pending login state are closed during unload/reconfigure.
- [ ] Review credential leakage across plugin errors, CPA request logs, callback URLs, management responses, and fixtures. Verify no API-key creation, copying, or rotation calls exist.
- [ ] After separate user authorization, verify real remote login, token-only model access, quota/history, refresh or re-login, and UI timezone behavior. Keep source inspection, mocked tests, and live acceptance evidence distinct.
- [ ] Document rollback: disable this plugin, stop its activity, and remove only its artifact if requested. Preserve unrelated auth files/configuration; deleting this plugin's stored credentials requires explicit user direction. No account API keys exist to revoke from this implementation.

**Done:** All requested behavior has evidence, or remaining gaps are reported explicitly. Do not call the provider complete on the strength of a build or fixture tests alone.

## Approval and Stop Conditions

- This document does not authorize execution. The user requested a plan only.
- Before implementation, approve the design and choose execution ownership. Do not automatically run a probe or create a Git repository.
- Stop the affected feature when token-only access is unsupported, the login contract requires forbidden key operations, or the host/UI extension is outside approved scope. Report the exact missing contract rather than add workarounds.
- [Issue 0001](</mnt/linux2/dev/cpa-auth-zai/docs/issues/0001-zai-provider.md>) tracks both original goals; partial quota success does not erase the model-provider goal.

## Plan Review

Requirements 1–9 are covered by Global Constraints and Tasks 1–6. Source inspection confirms CPA interfaces, not z.ai token permissions or deployed management-UI behavior. API-key creation is excluded in design, transport checks, tests, and release review. Remote operation uses CPA's existing OAuth patterns; timezone authority remains with the management UI. Remaining protocol and external-repository questions have explicit stop conditions.

[^requirements]: User requirements in this session.
[^cpa]: [Inspected CPA checkout](</mnt/linux2/dev/CLIProxyAPI>).
[^polling-report]: [Third-party polling flow description](https://cdn.jsdelivr.net/npm/pi-zcode-provider@0.1.0/PROTOCOL.md).
[^quota-report]: [OpenUsage z.ai endpoint documentation](https://openusage.sh/docs/providers/zai/).
[^provider-policy]: [Official z.ai Coding Plan FAQ](https://docs.z.ai/devpack/faq).
