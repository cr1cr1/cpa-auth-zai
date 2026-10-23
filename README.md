# cpa-auth-zai

A [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) plugin that adds z.ai (GLM Coding Plan) as a provider: browser sign-in, account-scoped models, request execution, and quota/usage reporting. Login-derived tokens only — the plugin never creates or uses API keys.

## Build

Requires Go 1.26+ with cgo and a local CPA v8 checkout (see `go.mod` `replace`).

```bash
export GOCACHE=$(mktemp -d)   # if the default cache is read-only
go test ./...
CGO_ENABLED=1 go build -buildmode=c-shared -o dist/cpa-auth-zai.so .
```

## Install (Linux amd64)

Copy the library into CPA's plugin directory using the ID-version naming rule:

```
plugins/linux/amd64/cpa-auth-zai-v0.1.0.so
```

Enable it in `config.yaml`:

```yaml
plugins:
  configs:
    cpa-auth-zai:
      enabled: true
```

## What it provides

| Capability | Detail |
|---|---|
| `auth_provider` | Remote-friendly sign-in via z.ai's hosted polling flow; credentials stored by CPA; renewal = sign in again. |
| `model_provider` | Coding Plan models (`glm-5.3`, `glm-5.3-flash`) published per authenticated account. |
| `executor` | `chat-completions` passthrough to `https://api.z.ai/api/coding/paas/v4/chat/completions` with the login token. |
| `quota_provider` | Rolling 5-hour / 7-day windows from `api.z.ai/api/monitor/usage/quota/limit`; missing values stay unknown. Resets are provider-managed (unsupported). |
| `management_api` | `GET /v0/management/plugins/cpa-auth-zai/usage/history` — token usage history with explicit timezone handling. |

## Login

Start the flow from the CPA management UI (provider `zai`) and complete sign-in in your browser — the browser may be on any machine. The plugin polls z.ai's hosted login endpoint; no callback port, desktop URI handler, or localhost redirect is required.

## Status and evidence tiers

See [docs/compatibility.md](docs/compatibility.md) for the verified/unverified contract table before relying on any single behavior.
