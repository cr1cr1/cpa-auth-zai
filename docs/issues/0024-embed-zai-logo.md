---
type: Issue
title: Embed and serve the z.ai logo
description: Package the official SVG in the plugin and expose it through a CPA-relative resource URL.
status: draft
issue_status: done
---
# Issue 0024: Embed and serve the z.ai logo

## Acceptance

- Download the official SVG as `zai.svg` and embed it in the plugin binary.
- Set `metadata.Logo` to `/v0/resource/plugins/cpa-auth-zai/zai.svg`.
- Register a public `/zai.svg` resource without adding a menu entry.
- Serve the exact embedded bytes as `image/svg+xml`.
- Keep usage history authenticated and unchanged.

## Verification

- The metadata test failed with the remote URL, then passed with the CPA-relative path.
- Resource registration and serving tests failed with no resource and HTTP 404, then passed with the embedded asset.
- The serving test compares the response with `zai.svg` and checks `image/svg+xml`.
- The downloaded 11,037-byte SVG parses as XML and contains no script, foreign object, event-handler, or external-link attributes.
- CPA v8.0.40 `internal/pluginhost/management.go` confirms `/zai.svg` resolves under `/v0/resource/plugins/cpa-auth-zai/` even with an empty menu label.
- `go test -race ./...` passed and `golangci-lint run` reported `0 issues`.
- The current-platform GoReleaser snapshot build passed.
- **Pending:** browser display through a live CPA management UI was not checked.
