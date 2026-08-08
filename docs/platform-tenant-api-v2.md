# Platform Tenant API v2

## Purpose

This document is the canonical contract for the normalized Platform > Nail
salons workspace. It replaces implementation-module route grouping with
operator workflows while preserving the existing domain services,
repositories, security boundaries, and scheduling evidence rules.

The v2 migration is additive. A v2 route and its legacy compatibility route
must delegate to the same domain service and persistence owner. Dual writes,
shadow records, and frontend-only authorization are forbidden.

## Security planes

Interactive requests and system runtime are separate security planes:

- Tenant and Platform requests use the server-owned `ActorContext` and the
  canonical `app_actor_feature_access` decision. Platform Admin is direct when
  its active role owns the exact capability. Platform Ops still needs the exact
  salon assignment, delegated capability, current temporary authorization,
  and any declared PII grant. Tenant principals require an active exact-salon
  membership on Tenant routes.
- Provider and worker execution uses a server-bound `app.system_salon_id` and
  `app_rls_system_salon_allowed`. It never receives Platform management
  authority. An unbound, malformed, or cross-salon system context fails closed.
- Frontend visibility and `allowed_actions` are presentation hints only. Every
  backend read and mutation reauthorizes the actor.
- Audit records store the actual actor. Platform requests never substitute the
  Tenant Owner identity.

## Route roots

The canonical roots are:

```text
/api/v2/platform/tenants/:tenant_id
/api/v2/tenant/salons/:salon_id
```

The two surfaces share domain vocabulary and DTOs but have separate route
registration and authorization. Provider and worker entrypoints are not
mounted beneath either root.

## Resource taxonomy

```text
/context

/business/profile
/business/hours
/business/public-page
/customers

/staff
/staff/:staff_id
/staff/:staff_id/scheduling-policy

/services
/services/:service_id
/services/:service_id/aliases
/services/:service_id/consultation-profile
/services/:service_id/scheduling-policy
/service-categories

/knowledge
/corrections
/evaluations

/calls
/calls/:call_id
/calls/:call_id/transcript
/calls/:call_id/scheduling-evidence
/calls/:call_id/diagnostics
/call-simulations

/ai-receptionist/runtime

/integrations
/integrations/:provider
/integrations/:provider/connection
/integrations/:provider/sync-runs
/integrations/:provider/verifications

/scheduling
/scheduling/authority
/scheduling/internal-calendar
/scheduling/resources
/scheduling/exceptions

/operations/overview
/operations/runtime-limits
/operations/provider-events
/operations/notifications

/access/team
/access/operators
/access/operators/:operator_id

/audit-events
/configuration-transfers
```

`technical` is not a domain resource. It remains a legacy route prefix only
until all Platform consumers have moved to Integrations, Scheduling, AI
Receptionist Runtime, and Operations.

## Response contract

Successful single-resource responses use:

```json
{
  "data": {},
  "meta": {
    "request_id": "opaque-request-id",
    "replayed": false,
    "resource_version": 1,
    "permissions": {
      "can_read": true,
      "allowed_actions": []
    }
  }
}
```

List responses place the array in `data` and add `meta.page` with `limit`,
`offset`, and `has_more`.

Mutation requests use a stable `action_key` and the owning resource's
`expected_version` or documented version fence. Exact replay returns the same
domain result with `meta.replayed=true`; changed-payload action-key reuse
conflicts.

Errors use the existing top-level `error` object, extended compatibly with
optional `field_errors`, `retryable`, `next_action`, and `blockers`. Provider
response bodies, secrets, raw SQL errors, and PII never appear in an error.

## Readiness contract

Every readiness projection uses `state`, `summary`, `blockers`, and
`checked_at`. The stable states are `ready`, `needs_setup`, `blocked`,
`degraded`, and `unavailable`. Blockers contain a stable code, safe message,
and optional `next_action`. Frontends render returned evidence and do not infer
readiness from provider names, connection copy, or locally duplicated rules.

## Scheduling authority command

Platform authority changes use the conceptual resource:

```text
PUT /api/v2/platform/tenants/:tenant_id/scheduling/authority
```

The request contains the target scheduling authority, expected current
authority version, and action key. The service may retain preview/run/event
records internally for immutable audit, exact replay, readiness snapshots, and
concurrency fencing. The Platform operator performs one visible change action;
the backend evaluates readiness and rechecks it inside the commit transaction.

Platform Admin does not need Tenant approval. Readiness, authority-version,
live-execution, idempotency, and atomicity checks remain mandatory business
safety rules. Platform Ops needs `technical.write`. Existing operations retain
their originating scheduling authority.

## AI receptionist runtime

`salons.ai_enabled` remains salon-wide runtime intent. Its management contract
is authority-neutral under `/ai-receptionist/runtime`; it is not owned by
Square. Changing this flag does not change scheduling authority, booking mode,
provider connection, conversation state, or historical operation origin.

The v2 control-plane migration must not change the provider/worker system-salon
runtime boundary, conversation state machine, intent interpretation, booking
executors, confirmation evidence, or prompt behavior.

## Platform information architecture

```text
Overview | Business | AI Receptionist | Platform Controls | History

Business
  Profile & hours | Staff | Services | Customers

AI Receptionist
  Calls | Training | Runtime

Platform Controls
  Integrations | Scheduling | Operations | Access | Copy configuration

History
  Audit events | Configuration transfers
```

One screen owns one primary operational object. Child configuration belongs in
the parent detail workflow. Provider setup belongs to Integrations; scheduling
authority and internal-calendar policy belong to Scheduling; AI on/off belongs
to AI Receptionist Runtime; immutable review evidence belongs to History.

## Implemented route ownership

The active Platform tenant frontend uses v2 for the tenant directory/context,
Business profile/hours/public page, Staff, Customers, rich POS-backed Services
and service categories, Calls and call catalog projections, Training,
authority-neutral AI Runtime, provider configuration/connection/verification,
Scheduling authority/internal calendar, Operations health/runtime limits/
provider events/owner notifications, Access, audit events, and configuration
transfers.

Calls detail is independently retriable from the list/readiness aggregate.
`GET /api/v2/platform/tenants/:tenant_id/calls/:session_id` returns optional
safe `detail_warnings` when incompatible legacy optional JSON is omitted and
uses section-specific sanitized error codes for actual transcript, handoff,
party-request, or scheduling-evidence read failures. Dynamic call-session
routes accept GUID session identifiers only. The composed production router
therefore preserves `/calls/readiness` as the static Voice-owned readiness
resource even though Conversation routes are registered first.

`backend/modules/pos` is the sole v2 owner of the rich `/services` and
`/service-categories` workflow because it owns provider field authority,
category aliases, and AI-bookable controls. `backend/modules/business` owns
profile, hours, public page, Staff, Customers, and their versioned mutations.
Legacy Business service/category routes remain compatibility adapters; they do
not compete for the v2 route or create a second UI.

The frontend source intentionally contains no active tenant-detail call to
`/api/platform/tenants/:tenant_id/technical/*`. The page route
`/platform/tenants/:tenant_id/technical` is a compatibility redirect to
Integrations.

## Compatibility and removal gate

Legacy routes remain additive adapters only while a registered frontend or
external consumer still uses them. Removal requires all of the following:

1. v2 and compatibility route contract fixtures prove equal domain behavior.
2. Platform Admin, Platform Ops, Tenant, PII, and exact-salon negative tests
   pass.
3. AI provider/worker tests prove exact system-salon runtime parity.
4. Scheduling authority replay, stale-version, readiness, concurrency, and
   originating-authority tests pass.
5. Frontend source search and runtime evidence show zero legacy consumers.
6. Production Platform Admin RBAC and tenant catalog audits pass.

## Non-goals

- No generic CRUD layer replaces domain services or repositories.
- No provider/worker runtime enters Platform actor authorization.
- No prompt, intent, conversation, booking, or confirmation behavior is
  changed by this control-plane normalization.
- No provider secret, authority history, appointment history, or live
  connection identity is copied by configuration transfer.
