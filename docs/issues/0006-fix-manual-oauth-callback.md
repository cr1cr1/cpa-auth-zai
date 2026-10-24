---
type: Issue
title: Accept a manually pasted z.ai OAuth callback
description: Share z.ai's original state with CPA and retain HTTP status when z.ai returns an empty poll error.
status: draft
issue_status: done
---
# Issue 0006: Manual OAuth callback

## Problem

CPA registers the plugin's random session state, but the z.ai authorize URL retains a different upstream state. A pasted callback therefore contains a state that CPA does not know and the management UI reports callback submission failure. Empty upstream poll errors also hide the HTTP status.

## Acceptance

- The authorize URL stays unchanged; CPA and the plugin flow reuse z.ai's original `state`. [Issue 0009](0009-preserve-zai-oauth-state.md) corrected the initial replacement approach after live z.ai authorization rejected it.
- A callback pasted from that URL contains the state registered by CPA, so the existing CPA v8 manual-callback endpoint accepts it. Hosted polling completes credential exchange; no local listener is required.
- An empty non-2xx poll response reports its HTTP status.
- No API key is created, imported, or used.

## Verification

- Both regression tests failed before the fix and pass after it.
- `mise run test`: pass, including the race detector.
- `mise run lint`: 0 issues.
- `mise run build`: Linux amd64 c-shared library built.
- Live z.ai sign-in requires an operator retry with the updated plugin.
