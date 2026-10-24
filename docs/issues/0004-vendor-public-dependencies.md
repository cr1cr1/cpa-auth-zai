---
type: Issue
title: Vendor public Go dependencies
description: Remove the local CPA source replacement, pin the matching public CPA release, and build from committed vendored modules.
status: done
issue_status: done
---
# Issue 0004: Vendor public Go dependencies

## Scope

Replace the `../CLIProxyAPI` module replacement with public `github.com/router-for-me/CLIProxyAPI/v8 v8.0.40`, the exact tag of the previously inspected checkout. Commit `vendor/` and update the build documentation. No provider behavior changes.

## Acceptance

- `go.mod` has no local `replace` directive.
- `vendor/modules.txt` records CPA v8.0.40 and no replacement.
- Tests, lint, and the c-shared build pass with `-mod=vendor` and no network access.
- A fresh clone needs only the plugin repository and the mise-managed Go tools.

## Verification

- Public proxy metadata confirmed CPA `v8.0.40` is commit `d946c4fba6fc9a5cfbca1f3d85184d62ad2fb683`, matching the former local checkout.
- `vendor/` is 448 KB and contains only the two CPA SDK packages, YAML, license files, and `modules.txt`.
- Race tests and the c-shared build passed with `GOPROXY=off`, `GOSUMDB=off`, `-mod=vendor`, and separate empty cache paths.
- `mise run fmt`, `mise run test`, `mise run build`, and `mise run lint` passed; lint reported `0 issues`.
- `go.sum` contains only the six required module/go.mod checksum entries.
