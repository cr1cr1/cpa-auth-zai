# cpa-auth-zai

A [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) plugin that adds z.ai (GLM Coding Plan) browser sign-in and quota/usage history. Model execution is disabled because z.ai rejects login tokens for inference. Login-derived tokens only — the plugin never creates or uses API keys.

## Build

[mise](https://mise.jdx.dev) provides Go and golangci-lint and runs the tasks (`mise run` lists them). All Go dependencies are vendored, including the public CPA v8.0.40 SDK packages. No CPA checkout or Go module download is needed; tasks use `-mod=vendor`.

```bash
mise run test   # go test -race ./...
mise run build  # dist/cpa-auth-zai.so
```

## Release builds

The tag workflow uses GoReleaser and Zig to build the c-shared plugin for Linux, macOS, and Windows on amd64 and arm64. It runs on macOS because Apple frameworks require the native Clang SDK. Archives contain the plugin library with version and target in its name, its generated C header, and this README. Rename the library to CPA's `cpa-auth-zai-v<version>` form when installing it. For a local snapshot on macOS, install Zig 0.14.1 and run `goreleaser release --snapshot --clean`.

## Install (Linux amd64)

Copy the library into CPA's plugin directory using the ID-version naming rule (`mise run install` does this; override the target with `PLUGIN_DIR=... mise run install`):

```
plugins/linux/amd64/cpa-auth-zai-v0.1.0.so
```

Enable it in `config.yaml`:

```yaml
plugins:
  configs:
    cpa-auth-zai:
      enabled: true
      # Optional IANA zone for usage history when the request omits timezone.
      history-timezone: Europe/Bucharest
```

## Active features

Quota and usage history are the active features. Model and executor capabilities are not registered while z.ai rejects login tokens for inference.

| Capability | Detail |
|---|---|
| `auth_provider` | Remote-friendly sign-in via z.ai's hosted polling flow; credentials stored by CPA; renewal = sign in again. |
| `quota_provider` | Rolling 5-hour / 7-day windows from `api.z.ai/api/monitor/usage/quota/limit`; missing values stay unknown. Resets are provider-managed (unsupported). |
| `management_api` | `GET /v0/management/plugins/cpa-auth-zai/usage/history` — token usage history with explicit timezone handling. |

## Login

Start the flow from the CPA management UI (provider `zai`) and complete sign-in in your browser — the browser may be on any machine. The plugin polls z.ai's hosted login endpoint; no callback port, desktop URI handler, or localhost redirect is required.

## Status and evidence tiers

See [docs/compatibility.md](docs/compatibility.md) for the verified/unverified contract table before relying on any single behavior.
