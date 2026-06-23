# Canonical POS Ownership Checklist

## Purpose

This checklist is the durable handoff for migrating the product from
Square-synced catalog screens to ManleAI-owned canonical salon data with
provider-specific POS links.

Future agents should read this file before changing services, staff,
customers, POS sync, provider switching, availability, or booking resolution.

## Core Decision

ManleAI should be the system of record for salon operational data. POS
providers are external projections and booking execution layers.

Data ownership rules:

- ManleAI owns canonical services, staff, customers, AI controls, owner
  workflow state, fallback requests, logs, and training data.
- The active POS provider owns real availability and booking execution.
- A confirmed, rescheduled, or cancelled appointment still requires active POS
  provider success.
- Provider IDs are mappings, not primary product identity.
- Square Appointments is the first real POS integration. Future POS names are
  architecture targets until implemented.

Do not describe Square or any POS as the sole source of truth for the whole
system. Say: ManleAI owns canonical operational data; the active POS is the
authority for booking execution and provider-owned projections.

## Non-Negotiable Booking Boundary

Never mark an appointment confirmed unless the active `POSProvider` returns a
successful booking ID and required provider booking metadata.

If POS fails, times out, lacks permissions, or omits required booking metadata:

- create or preserve a fallback pending request
- log a normalized POS error
- notify or surface owner review
- do not create fake confirmed appointment state
- do not use customer-facing confirmed wording

## Current System Shape To Respect

Current code has:

- `services` and `staff` tables shaped as Square-synced records with
  `pos_provider`, `pos_service_id`, and `pos_staff_id`.
- `ai_bookable` as internal dashboard control.
- booking attempts and appointments already routed through provider-neutral
  `modules/pos.POSProvider`.
- Square-specific auth, payloads, endpoint URLs, and mapping isolated in
  `backend/modules/pos_square`.
- Customers dashboard as a local CRM-lite directory backed by canonical
  `customers`, activity aggregation, optional POS customer links, and Square
  phone lookup as a secondary tool.

## Global Implementation Rules

- Follow the user confirmation gate in `AGENTS.md`: inspect first, present a
  plan, wait for approval before code changes.
- For UI changes, provide Mockup as Text before implementation.
- Keep handlers thin, business rules in services, persistence in repositories.
- Keep Square-specific behavior inside `backend/modules/pos_square`.
- Keep API responses provider-neutral.
- Update migrations, Ent schemas, repositories, DTOs, docs, and UI states
  together when a slice crosses boundaries.
- Use archive/soft-delete for owner data; avoid hard delete for services,
  staff, customers, appointments, or history-bearing records.

## Target Data Model

Canonical entity tables:

- `services`
- `staff`
- `customers`

Provider link table:

```text
pos_entity_links
  id
  salon_id
  entity_type: service | staff | customer
  entity_id
  provider
  provider_entity_id
  provider_version
  sync_status
  last_synced_at
  last_error
  created_at
  updated_at
```

Recommended sync statuses:

- `local_only`
- `syncing`
- `synced`
- `sync_failed`
- `unmapped`
- `archived`

Recommended canonical fields to add where needed:

- `sync_status`
- `archived_at`
- `last_synced_at`
- `sync_error`
- `source`: `local | imported`

Booking eligibility rule:

- A record may exist locally without a POS link.
- A local-only, unmapped, archived, or sync-failed service/staff record must
  not be used for POS availability or POS booking.
- `AI bookable` can only be enabled when the canonical record is active and has
  a valid active-provider link.

## Phase Checklist

### Phase 0: Architecture Docs

Goal: align repo language and decision-making before schema/code work.

Tasks:

- [ ] Update `docs/architecture.md` with canonical ManleAI ownership.
- [ ] Update `docs/pos-adapter-layer.md` with POS links and provider projection
      model.
- [ ] Update `docs/api.md` with future API semantics and sync statuses.
- [ ] Update `CONTEXT.md` if new terms are needed.
- [ ] Preserve the POS-first booking confirmation boundary.

Definition of done:

- [ ] Docs clearly say ManleAI owns canonical operational data.
- [ ] Docs clearly say active POS owns booking execution.
- [ ] No docs imply universal POS support exists today.

Suggested thread prompt:

```text
Implement Phase 0 from docs/canonical-pos-ownership-checklist.md.
Update architecture/API/POS docs only. Do not change runtime code.
Inspect first, present plan, wait for approval before editing.
```

### Phase 1: Schema Foundation

Goal: add provider link foundation without changing booking behavior.

Tasks:

- [ ] Add migration for `pos_entity_links`.
- [ ] Add sync status fields to `services` and `staff`.
- [ ] Backfill existing Square service/staff mappings into
      `pos_entity_links`.
- [ ] Mark existing Square-imported services/staff as `synced`.
- [ ] Update Ent schemas to mirror migrations.
- [ ] Add repository helpers only if needed for backfill/read consistency.
- [ ] Do not change booking, availability, or dashboard behavior yet.

Definition of done:

- [ ] Existing Square-synced services and staff still load as before.
- [ ] Existing `pos_service_id` and `pos_staff_id` values are represented in
      `pos_entity_links`.
- [ ] Backend tests pass.

Suggested thread prompt:

```text
Implement Phase 1 from docs/canonical-pos-ownership-checklist.md.
Add schema foundation for pos_entity_links and sync status fields.
Backfill existing Square service/staff links. No UI CRUD and no booking
behavior changes. Inspect first, present plan, wait for approval before editing.
```

### Phase 2A: Canonical Services CRUD

Goal: make Services owner-manageable in ManleAI while keeping booking safe.

Tasks:

- [ ] Add provider-neutral service create/update/archive request and response
      DTOs.
- [ ] Add service-layer validation for name, duration, price, active state,
      archive behavior, and AI bookable gating.
- [ ] Add repository methods for local create/update/archive.
- [ ] Keep existing Square sync import path working.
- [ ] Expose sync/link status in service list API.
- [ ] Add Services UI create/edit/archive flow.
- [ ] Show status badges: local only, synced, sync failed, unmapped, archived.
- [ ] Disable AI booking for records without active-provider mapping.

Definition of done:

- [ ] Owner can create/edit/archive services locally.
- [ ] Local-only service is visible but not bookable.
- [ ] Existing Square-synced services remain usable.
- [ ] UI has loading, empty, error, success, and disabled/gated states.

Suggested thread prompt:

```text
Implement Phase 2A from docs/canonical-pos-ownership-checklist.md.
Build canonical Services CRUD API and UI. Services are ManleAI-owned records
with POS link/sync status. Keep AI booking gated by active POS link. Do not
implement Staff/Customers yet. Provide Mockup as Text before UI edits.
```

### Phase 2B: Canonical Staff CRUD

Goal: make Staff owner-manageable in ManleAI while keeping booking safe.

Tasks:

- [ ] Add provider-neutral staff create/update/archive request and response
      DTOs.
- [ ] Add service-layer validation for name, contact fields, active state,
      archive behavior, and AI bookable gating.
- [ ] Add repository methods for local create/update/archive.
- [ ] Keep existing Square staff sync import path working.
- [ ] Expose sync/link status in staff list API.
- [ ] Add Staff UI create/edit/archive flow.
- [ ] Disable AI booking for staff without active-provider mapping.

Definition of done:

- [ ] Owner can create/edit/archive staff locally.
- [ ] Local-only staff is visible but not bookable.
- [ ] Existing Square-synced staff remain usable.
- [ ] UI has loading, empty, error, success, and disabled/gated states.

Suggested thread prompt:

```text
Implement Phase 2B from docs/canonical-pos-ownership-checklist.md.
Build canonical Staff CRUD API and UI. Staff are ManleAI-owned records with POS
link/sync status. Keep AI booking gated by active POS link. Do not change
booking confirmation behavior. Provide Mockup as Text before UI edits.
```

### Phase 3: POS Sync Engine

Goal: push canonical records to active POS when supported.

Tasks:

- [ ] Add provider capabilities contract, for example catalog/staff/customer
      write support flags.
- [ ] Add provider-neutral methods where supported:
      `UpsertService`, `ArchiveService`, `UpsertStaff`, `ArchiveStaff`,
      `UpsertCustomer`.
- [ ] Add `pos_sync_jobs` or equivalent outbox table.
- [ ] Add queued/running/succeeded/failed job states and retry metadata.
- [ ] Add worker/service to process sync jobs.
- [ ] Keep unsupported provider writes capability-gated.
- [ ] Log provider failures to `pos_errors`.
- [ ] Write `pos_sync_logs` for sync operations.

Definition of done:

- [ ] Local service/staff changes can queue POS sync jobs.
- [ ] UI/API can show sync progress and failure reason.
- [ ] Unsupported operations are disabled/gated, not faked.
- [ ] Square payloads stay inside `backend/modules/pos_square`.

Suggested thread prompt:

```text
Implement Phase 3 from docs/canonical-pos-ownership-checklist.md.
Add provider-neutral POS sync jobs/outbox and adapter capability model for
service/staff writes. Capability-gate unsupported provider writes. Keep Square
payloads inside pos_square. Inspect first, present plan, wait for approval.
```

### Phase 4: Booking Resolution Through POS Links

Goal: make booking consume canonical IDs resolved through active-provider links.

Tasks:

- [x] Add repository methods to resolve canonical service/staff to active POS
      provider links.
- [x] Update availability resolution to reject unmapped/local-only/sync-failed
      records.
- [x] Update booking create/reschedule resolution to use provider links.
- [x] Preserve segment and `staff_selection_mode` behavior.
- [x] Preserve fallback pending behavior on POS failure.
- [x] Add service-level tests for bookable vs unbookable mapping states.

Definition of done:

- [x] Booking API accepts canonical IDs.
- [x] POS adapter receives provider IDs.
- [x] Local-only records cannot book.
- [x] Confirmed appointment invariant remains intact.

Suggested thread prompt:

```text
Implement Phase 4 from docs/canonical-pos-ownership-checklist.md.
Update availability and booking resolution to use canonical service/staff IDs
resolved through active provider links. Local-only/unmapped/sync_failed records
must be visible but not bookable. Preserve POS-first confirmation invariant.
```

### Phase 5: Canonical Customers

Goal: turn Customers into a real local CRM-lite model with provider links.

Tasks:

- [x] Add `customers` canonical table.
- [x] Add customer provider links using `pos_entity_links`.
- [x] Add local create/update/archive customer API.
- [x] Add dedupe by phone/email.
- [x] During booking, search/link/create POS customer if needed and supported.
- [x] Update Customers UI from activity-only to customer records plus activity.

Definition of done:

- [x] Owner can create/edit/archive customers locally.
- [x] Customer activity still shows calls, bookings, pending requests.
- [x] Booking links or creates POS customer without leaking POS payloads.
- [x] No duplicate customer creation for same phone/email.

Suggested thread prompt:

```text
Implement Phase 5 from docs/canonical-pos-ownership-checklist.md.
Add canonical Customers table/API/UI with POS customer links. Support local
create/edit/archive, dedupe by phone/email, and link/create POS customer during
booking when needed. Provide Mockup as Text before UI edits.
```

### Phase 6: Provider Switch Workflow

Goal: make switching POS providers a guided, low-risk workflow.

Phase 6A foundation completed:

- [x] Add `salons.active_pos_provider` as the active provider source of truth.
- [x] Add provider switch readiness API and dashboard gate.
- [x] Make service/staff management and phone booking readiness evaluate the
      active provider instead of hard-coded Square checks.
- [x] Keep switch activation disabled until an alternate real adapter exists.

Phase 6B switch run and match skeleton completed:

- [x] Add provider switch run and match persistence tables.
- [x] Add owner-scoped switch run create/latest/read APIs.
- [x] Persist blocked switch runs when the requested target adapter is not
      installed.
- [x] Add service/staff auto-match skeleton by normalized name, duration,
      phone, and email for future real adapter imports.
- [x] Surface latest switch run state and match counts in the Integrations
      dashboard while keeping start/activate controls gated.

Phase 6C match review skeleton completed:

- [x] Add owner-scoped match review API for confirm, unmatched, and skipped
      decisions.
- [x] Recompute switch run review status as `needs_review` or `ready` while
      keeping `can_activate=false`.
- [x] Add Integrations dashboard match review rows with gated actions.
- [x] Keep blocked runs and deployments without an alternate real adapter
      non-editable.

Phase 6D customer match skeleton completed:

- [x] Add customer switch match candidates from canonical customers and
      customer `pos_entity_links`.
- [x] Auto-match customer candidates by normalized phone, email, and name.
- [x] Mark duplicate provider customer candidates that map to the same
      canonical customer as conflicts.
- [x] Keep provider activation disabled after customer matches are reviewed.

Phase 6E dry-run readiness checklist gate completed:

- [x] Add owner-scoped dry-run readiness API for provider switch runs.
- [x] Return checklist gates for target adapter, run state, imported records,
      match resolution, current-provider booking readiness, and dry-run
      execution availability.
- [x] Surface the dry-run checklist in the Integrations dashboard with a
      disabled dry-run action.
- [x] Keep `dry_run_ready=false`, `can_run_dry_run=false`, and
      `can_activate=false` until a real alternate adapter and executable
      dry-run path exist.

Phase 6F import wizard gated shell completed:

- [x] Add Integrations dashboard import wizard shell for the provider switch
      workflow.
- [x] Gate target provider selection and start action when no alternate native
      POS adapter is installed.
- [x] Drive wizard step states from provider switch readiness, latest run
      summary, and dry-run checklist data.
- [x] Do not call import/start APIs, create fake provider options, or fabricate
      imported provider records.

Full Phase 6 remains open because the pilot currently has only the Square
Appointments adapter. Do not add fake provider implementations to satisfy the
workflow.

Tasks:

- [x] Add provider switch/import wizard gated shell.
- [ ] Enable executable provider switch/import wizard once an alternate real
      adapter exists.
- [ ] Import services/staff/customers from new provider.
- [x] Auto-match to canonical records by stable heuristics:
      name, duration, phone, email, normalized text.
- [x] Add manual conflict resolution UI.
- [x] Add dry-run booking readiness checklist gate.
- [ ] Execute alternate-provider dry-run booking readiness against a real target
      adapter and persist `dry_run_ready=true` only after success.
- [ ] Activate new provider only after required mappings are ready.
- [ ] Preserve old provider links for history.

Definition of done:

- [ ] Owner can connect a new provider without losing canonical data.
- [ ] Active provider cannot switch into a broken booking state.
- [ ] Existing appointment history remains tied to the provider used at the
      time.

Suggested thread prompt:

```text
Implement Phase 6 from docs/canonical-pos-ownership-checklist.md.
Build provider switch workflow: connect new POS, import records, auto-match to
canonical services/staff/customers, resolve unmatched records, dry-run booking
readiness, and activate new provider only after required mappings are ready.
```

### Phase 7: Dashboard Polish And Regression Review

Goal: make owner UX coherent after canonical ownership migration.

Tasks:

- [ ] Review Services, Staff, Customers, and Appointments copy.
- [ ] Ensure all pages show sync status, booking readiness, and blockers.
- [ ] Ensure loading, empty, error, success, and disabled/gated states exist.
- [ ] Remove stale Square-only wording where canonical model now applies.
- [ ] Keep wording scope-honest: Square Appointments is current integration.
- [ ] Add regression tests or smoke tests for critical flows.

Definition of done:

- [ ] Owner can understand what is local, synced, failed, or not bookable.
- [ ] No UI implies broad POS support beyond implemented providers.
- [ ] No UI implies confirmed appointment without POS success.

Suggested thread prompt:

```text
Implement Phase 7 from docs/canonical-pos-ownership-checklist.md.
Polish and review dashboard UX for canonical ownership and POS sync status.
Add missing empty/loading/error/sync_failed states, docs updates, and regression
tests. Keep Square Appointments wording scope-honest.
```

## Validation Commands

Backend:

```bash
cd backend
GOCACHE=/private/tmp/manleai-go-cache go test ./...
```

Frontend:

```bash
cd frontend
npm run typecheck
npm run build
```

Local services:

```bash
docker compose up -d postgres redis flyway
```

## Future Agent Startup Checklist

At the start of every thread in this migration:

- [ ] Read `AGENTS.md`.
- [ ] Read this checklist.
- [ ] Read `docs/architecture.md`.
- [ ] Read `docs/pos-adapter-layer.md`.
- [ ] Read `docs/api.md` for affected endpoints.
- [ ] Inspect current git status.
- [ ] Inspect affected schema, repository, service, handler, DTO, and UI files.
- [ ] Present a scoped plan and wait for approval before code edits.

## Safety Stop Conditions

Stop and ask for explicit direction if:

- a phase requires provider writes that Square or the active provider does not
  support cleanly
- a migration would destroy existing POS IDs or booking history
- a UI change would imply local-only records are bookable
- booking confirmation wording could appear without POS success
- provider switching would activate a provider with incomplete mappings
