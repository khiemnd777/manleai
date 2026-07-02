---
name: booking-safety-tdd
description: Investigate, plan, and only after explicit approval implement booking, reschedule, cancel, availability, appointment, booking_attempt, and fallback behavior with test-first safety. Use when Milestone 3 booking operations or confirmation logic may change. Do not write tests or code until the latest user message explicitly approves the exact change scope.
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

## Write Approval Hard Stop

- This skill does not authorize edits by itself. Follow the root `AGENTS.md` confirmation gate before any code, test, migration, doc, config, skill, agent, runtime, or generated artifact change.
- Treat questions, complaints, screenshots, appshots, status checks, "why" prompts, and partial instructions as read-only investigation requests, not implementation approval.
- Before editing, trace the real booking owner and root cause, state ambiguity or open questions, present the test and implementation plan, then wait for latest explicit approval for the exact scope.
- Earlier approval expires when the scope changes, after the assistant gives a final answer, or when the latest user message is a question or complaint instead of an approval.

## Test-First Flow

1. After explicit approval, write one failing behavior test for the next booking rule.
2. Implement the smallest production code to pass it only after the approved failing test exists or the user explicitly approves a no-test exception.
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
