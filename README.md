# cpa-auth-zai

A [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) plugin for z.ai (GLM Coding Plan): browser sign-in, models, inference, quota, and usage history. Login provisions a named project API key; account APIs use separate login-derived tokens.

## Screenshots

![OAuth login flow in the CPA management UI](screenshots/oauth-login.png)

![Quota and usage history in the CPA management UI](screenshots/quota.png)

## Build

- [mise](https://mise.jdx.dev) provides Go, golangci-lint, and tasks (`mise run` lists them).
- Vendored dependencies include the public CPA v8.0.40 SDK packages.
- Tasks use `-mod=vendor`; no CPA checkout or Go module download is needed.

```bash
mise run test   # go test -race ./...
mise run build  # dist/cpa-auth-zai.so
```

## Release builds

- Tag workflow: c-shared plugin for Linux, macOS, and Windows on amd64 and arm64.
- One macOS runner: native Clang, Zig, and the checked-in GoReleaser config without rewrites.
- GoReleaser injects cleaned tag SemVer into plugin registration and publishes the release; local builds use `0.1.0`.
- Archives: versioned plugin library, generated C header, and this README.
- Manual install: rename the library to CPA's `cpa-auth-zai-v<version>` form.
- Local macOS snapshot: install Zig 0.17.0 and run `goreleaser release --snapshot --clean`.

## Install

Set `CPA_PLUGIN_DIR` to CPA's platform-specific plugin directory. `mise run install` copies the current OS/architecture artifact as `cpa-auth-zai.<ext>`:

```
CPA_PLUGIN_DIR=plugins/linux/amd64 mise run install
# Installs plugins/linux/amd64/cpa-auth-zai.so
```

Enable the plugin in `config.yaml`:

```yaml
plugins:
  configs:
    cpa-auth-zai:
      enabled: true
      # Optional IANA zone for usage history when the request omits timezone.
      history-timezone: Europe/Bucharest
      # Exact project API-key name to reuse or create during login.
      api-key-name: cpa-auth-zai
```

## Features

- `auth_provider`: hosted z.ai sign-in, CPA credential storage, and renewal through a new sign-in.
- `model_provider`: live Coding Plan catalog from `api.z.ai/api/coding/paas/v4/models` with the OAuth-derived business token. No fixed-model fallback.
- `executor`: project API key plus optional `X-Zcode-Token`; CPA payload-format metadata selects:
  - Chat Completions: `/api/coding/paas/v4/chat/completions`.
  - OpenAI Responses: `/api/v1/responses`.
  - Anthropic Messages: `/api/anthropic/v1/messages` with `anthropic-version: 2023-06-01`.
  - No client-specific routing or login-token fallback.
  - Native Responses: live-verified completed tool call and `response.completed`. Anthropic Messages: live-unverified.
- `quota_provider`: rolling 5-hour/7-day windows from `api.z.ai/api/monitor/usage/quota/limit`. Missing values stay unknown; provider-managed resets are unsupported.
- `management_api`: `GET /v0/management/plugins/cpa-auth-zai/usage/history` returns token usage history with explicit timezone handling.
- Streaming: executor requests always send `stream: true`, even when input omits `stream` or sets `false`. Non-streaming payloads stay unchanged. Valid SSE events keep their exact bytes; malformed events are dropped, with an explicit error if no valid terminal remains.
- ZCode fingerprint: inference-only headers for identity, platform, OS, timezone, session, and request tracing; pinned to ZCode 3.10.2. No installed-client inspection or device-identity file.
- Branding: embedded `zai.svg` served at `/v0/resource/plugins/cpa-auth-zai/zai.svg` for plugin cards and the plugin-resource sidebar. General provider branding belongs to the management UI, not plugin API configuration.

## Login

1. Start login in the CPA management UI with provider `zai`.
2. Complete sign-in in a browser on any machine.
3. If z.ai shows its hosted callback page, paste the complete callback URL into the CPA login modal.

The plugin polls z.ai's hosted login endpoint. No callback port, desktop URI handler, or localhost redirect is needed.

After sign-in, the plugin:

- Lists keys in the default organization and first project.
- Reuses the exact `api-key-name` match or creates that key.
- Retrieves the secret and stores the combined key in CPA credential storage.
- Never selects another key, rotates keys, or deletes keys.

Existing credentials from older plugin versions need a new sign-in before inference. Changing `api-key-name` also requires a new sign-in.

The key-management API has third-party evidence, not a published z.ai integration contract. z.ai separates Standard and Coding Plan keys; check the created key's classification and billing during live verification.

## Logging

- OAuth and quota/usage success and failure events use CPA's `host.log` RPC.
- Pending OAuth polls stay quiet. Upstream errors redact credentials.
- Malformed upstream SSE events use CPA debug logs with sanitized previews, tails, lengths, and parse errors. Valid event bytes stay unchanged.
- CPA controls log output. The plugin creates no log file.

## Compatibility

See [docs/compatibility.md](docs/compatibility.md) for verified and unverified behavior.

## License

[MIT License](LICENSE)
