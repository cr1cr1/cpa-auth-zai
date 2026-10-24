---
type: Issue
title: Address provider review findings
description: Disable unusable model capabilities, correct refresh and envelope errors, redact credentials, preserve metadata, and clean packaging and evidence docs.
status: done
issue_status: done
---
# Issue 0003: Address provider review findings

## Scope

Implement all findings from the 2026-10-24 ponytail review without adding API-key support.

## Acceptance scenarios

1. Registration advertises only auth, quota, and management capabilities while token-only inference is blocked; no account model catalog is published.
2. Refresh with unchanged login tokens returns an unsupported error, never refresh success.
3. Non-stream code 1234 in HTTP 200 becomes a sanitized plugin error.
4. Stream envelope 401 retries the next token; terminal business errors close the RPC stream with an error, never a clean data-only stream.
5. Upstream errors redact every supplied credential, including credentials at the start or end of the message.
6. Login organization and project IDs survive storage parsing and refresh-related normalization.
7. The tracked executable is removed and ignored; only source and ignored `dist/` artifacts remain.
8. The CPA replacement path is relative to the sibling checkout and the README explains it.
9. Compatibility evidence matches the live login and timezone implementation.
10. Unused endpoints and state are removed.

## Verification

Each behavioral change had a failing test before implementation. Additional stream tests cover fragmented JSON envelopes and prohibit bearer retry after client output starts.

Final gates passed on 2026-10-24:

- `mise run fmt` and `git diff --check`
- `mise run test` (`go test -race ./...`)
- `mise run lint` (`0 issues`)
- `mise run build` produced only `dist/cpa-auth-zai.so`
- `go list -m` resolved CPA to the sibling `../CLIProxyAPI` checkout
- Final ponytail review found no remaining concrete blocker

No new live z.ai inference claim was made in this issue. Its registration stop was later superseded by [issue 0010](0010-enable-oauth-models.md) under an explicit operator assumption; the earlier provider-side rejection evidence remains unchanged.
