---
name: booking-safety-tdd
description: Build booking, reschedule, cancel, availability, appointment, booking_attempt, and fallback behavior with test-first safety. Use when implementing Milestone 3 booking operations or changing confirmation logic.
---

# Booking Safety TDD

Use this skill whenever appointment confirmation behavior changes.

## Codebase Truth Rule

- Be strictly honest about the current codebase and runtime configuration. Do not flatter, reassure, or infer readiness from docs, milestones, intended architecture, or previous claims.
- Before saying booking behavior works, is ready, or only needs testing, verify the actual service path, POS/config gates, persistence writes, and tests/status output when available.
- Separate confirmed current behavior from product intent, planned behavior, unverified assumptions, and missing implementation.
- If evidence conflicts, stop and name the conflict directly with file references instead of smoothing over it.

## Product-Grade Rule

- Treat booking, reschedule, cancel, availability, and fallback work as production-grade pilot behavior, not MVP, demo, prototype, or happy-path scaffolding.
- Before proposing or implementing, check repeated-submit behavior, idempotency keys, duplicate appointment/request prevention, retry/rerun semantics, race conditions, conflict handling, tenant/security/privacy boundaries, and safe failure states.
- POS success, POS failure, timeout, retry, fallback pending, owner approval, notification, audit log, and duplicate request paths must be explicit in tests or safely gated.
- Do not mark booking work complete until repeated execution cannot create duplicate confirmed appointments, duplicate fallback rubbish, or misleading caller/owner state.

## Core Invariant

An appointment is confirmed only after the active `POSProvider` returns a successful POS booking ID. If POS fails, create a fallback pending request and log the failure. Do not mark the appointment confirmed.

## Test-First Flow

1. Write one failing behavior test for the next booking rule.
2. Implement the smallest production code to pass it.
3. Repeat one vertical slice at a time.
4. Refactor only while green.

## Required Behaviors

- Success path stores appointment, appointment services, booking attempt, and POS booking ID in one transaction.
- POS failure path stores booking attempt as `fallback_pending` or `failed`.
- POS failure path stores a normalized `pos_errors` row.
- AI/customer-facing confirmation text is only allowed on POS success.
- Reschedule and cancel operations call `POSProvider` and persist internal state only after provider success or with a clear fallback state.

## Test Surfaces

Prefer service-level tests using a fake `POSProvider` implementation. Do not mock Square payloads in booking service tests. Square payload tests belong in `pos_square`.

## Validation

```bash
cd backend
GOCACHE=/private/tmp/manleai-go-cache go test ./...
```
