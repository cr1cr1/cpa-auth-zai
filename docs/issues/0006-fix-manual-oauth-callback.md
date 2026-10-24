---
type: Issue
title: Accept a manually pasted z.ai OAuth callback
description: Return CPA's session state in the z.ai authorize URL and retain HTTP status when z.ai returns an empty poll error.
status: draft
issue_status: done
---
# Issue 0006: Manual OAuth callback

## Problem

CPA registers the plugin's random session state, but the z.ai authorize URL retains a different upstream state. A pasted callback therefore contains a state that CPA does not know and the management UI reports callback submission failure. Empty upstream poll errors also hide the HTTP status.

## Acceptance

- The authorize URL keeps its upstream parameters and replaces `state` with the state returned to CPA.
- A callback pasted from that URL contains the state registered by CPA, so the existing CPA v8 manual-callback endpoint accepts it. Hosted polling completes credential exchange; no local listener is required.
- An empty non-2xx poll response reports its HTTP status.
- No API key is created, imported, or used.

## Verification

- Both regression tests failed before the fix and pass after it.
- `mise run test`: pass, including the race detector.
- `mise run lint`: 0 issues.
- `mise run build`: Linux amd64 c-shared library built.
- Live z.ai sign-in requires an operator retry with the updated plugin.
