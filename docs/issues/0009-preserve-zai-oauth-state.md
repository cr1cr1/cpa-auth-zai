---
type: Issue
title: Preserve the z.ai OAuth state
description: Reuse z.ai's authorize state as CPA's session state because replacing it makes z.ai authorization fail.
status: draft
issue_status: done
---
# Issue 0009: Preserve z.ai OAuth state

## Problem

Issue 0006 replaced z.ai's state with a new CPA state so manual callback lookup could work. Live authorization then failed at z.ai's callback. z.ai requires its original state, while CPA requires the callback state to match the registered session.

## Acceptance

- Return the upstream authorize URL unchanged.
- Reuse its nonempty `state` for `AuthLoginStartResponse.State` and the plugin flow key.
- Reject an authorize URL without `state`.
- Manual CPA callback lookup and z.ai authorization use the same state.

## Verification

- State-preservation and missing-state tests failed before the fix and pass after it.
- The operator supplied the live failure evidence for the replaced state; no new live z.ai request was made during this fix.
