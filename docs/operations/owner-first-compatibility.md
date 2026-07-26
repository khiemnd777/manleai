# Owner-first Compatibility And Legacy POS Removal Gate

This inventory is the compatibility contract for scheduling responses and
consumer behavior. It does not create a second scheduling taxonomy. The stable
authority values and success rules remain owned by `docs/scheduling-authority.md`:
`owner_manual`, `manleai_calendar`, and `external_provider`.

## Consumer Rule

Generic scheduling consumers must first validate `scheduling_authority`. They
then use authority-native evidence and backend capabilities:

- `owner_manual`: a durable scheduling request ID, request status, and request
  version. It is never an appointment confirmation.
- `manleai_calendar`: a durable internal appointment and attempt ID, exact
  appointment version/status, active-child count, and exact child plan where
  the operation requires one.
- `external_provider`: canonical `authority_provider`,
  `authority_appointment_id`, `authority_appointment_version`, the persisted
  external attempt status, and the external compatibility evidence required by
  the provider path.
- unknown or missing authority: fail closed. Do not infer the origin from a POS
  ID, provider label, Square connection/readiness, `active_pos_provider`, sync
  state, status copy, or a non-empty legacy field.

An external row remains external after a later authority switch because the
persisted operation or target origin wins. The salon's current authority gates
only genuinely new work.

## Compatibility Inventory And Removal Gate

The table is the removal gate. A field family may be removed only after every
named consumer has migrated, its regression gate passes without the field, and
the public API/version decision is documented. Empty/null internal evidence is
not a migration signal and must never be converted to a provider value.

| Field family | Current owner and consumers | Authority rule | Classification | Regression/removal gate |
| --- | --- | --- | --- | --- |
| `salons.active_pos_provider`; API/TS `active_pos_provider` | Salon external-adapter configuration; POS repository, Integrations, Services/Staff external mapping, Square readiness, config-transfer preview | Selects an adapter only inside `external_provider`; never selects authority or confirms work | Retained active external configuration; portable config-transfer adapter intent | Removal blocked until external adapter selection has a replacement contract and schema/API migration. Config import must continue to report source/target/result without switching authority. |
| `booking_attempts.pos_provider`, `pos_booking_id`, `pos_booking_version`, `target_pos_booking_version`, `pos_idempotency_key`; booking DTO aliases | External booking dispatch, provider idempotency, retry, lease recovery, direct mutation and reconciliation | Valid only as external-provider execution evidence; generic UI dispatch uses `scheduling_authority` plus `authority_*` | Retained external evidence; response aliases deprecated outside external detail views | External booking, retry, recovery, reconciliation, Square test, frontend and POS-calendar contract tests must first consume canonical authority evidence. Removal remains blocked by external API compatibility. |
| `appointments.pos_provider`, `pos_appointment_id`, `pos_appointment_version`, `pos_customer_id`, `pos_sync_status`, `last_pos_synced_at`, `pos_sync_error`; appointment DTO aliases | External calendar import/webhook/repair, external reschedule/cancel and provider detail views | May describe only an external-origin appointment. Internal rows keep persisted columns NULL and nullable JSON aliases omitted | Retained external mirror evidence; response aliases deprecated for authority/lifecycle discrimination; internal-null invariant | Calendar/webhook/direct-mutation convergence must migrate to a versioned external evidence contract before removal. Internal JSON tests must continue to reject provider/POS/sync/error fields. |
| `booking_attempt_segments.pos_service_id`, `pos_service_version`, `pos_staff_id`; `appointment_services` equivalents; DTO compatibility snapshots | Exact external catalog/assignment proof and historical external replay | External-only raw assignment evidence. Internal segments use canonical and `authority_*` IDs with POS columns NULL | Retained external evidence; internal-null invariant | Removal blocked until external fingerprint, retry, mirror matching and reconciliation no longer consume raw mappings and old rows have a proven migration. |
| `provider_outcome`, `retry_policy`, `reconciliation_status`, `reconciliation_resolution`, `reconciliation_resolved_at`, `error_code`, `error_message`, processing lease fields | External booking attempts, owner external-reconciliation queue, Square test/integration diagnostics | These describe external execution uncertainty; they do not classify authority. Owner/manual and internal neutral action responses do not emit them | Retained active external evidence; internal response exclusion | Removal blocked while fallback/retry/reconciliation and lease recovery use these fields. Generic badges/actions must first require external origin. |
| `provider`, provider location/generation/fence fields on external quotes and attempts | External availability, target/retry quote lineage, provider fence validation | External-only. Internal quotes use authority/config/version fences and contain no provider fence | Retained external safety evidence | Removal blocked by provider location/generation safety, quote replay and retry-lineage compatibility. |
| `authority_provider`, `authority_appointment_id`, `authority_appointment_version`, scheduling authority/version fields | Authority-neutral booking/scheduling DTOs, frontend, POS Calendar, conversation and historical replay | Canonical authority-native discriminator/evidence. `authority_provider` is absent for internal/manual; appointment ID/version remain authority-owned | Primary contract; not a legacy-removal target | All three-authority JSON and mapper counterexamples must pass. Unknown authority fails closed. |
| Service/staff/customer `pos_provider`, `pos_service_id`, `pos_service_version`, `pos_staff_id`, `pos_customer_id`, `pos_linked`, provider entity links and sync fields | Canonical catalog management plus external mappings, Square sync, external availability/booking | Provider IDs are mappings, never canonical product identity or authority selection. Internal/manual eligibility uses canonical records without fake links | Retained active external mapping compatibility | Removal blocked until all external mapping reads use the durable provider-link model and legacy columns have an explicit data migration/backfill proof. |
| Business-hour `provider`, `provider_location_id`, `provider_period_index`, imported source values | External provider calendar safety and audit | Provider-imported periods remain external evidence. `manleai_calendar` readiness uses only `source=local_override` | Retained external evidence; internal exclusion | Removal blocked while external availability uses imported hours and historical provenance. Never copy imported hours into local overrides. |
| Reconciliation candidate `provider`, `provider_appointment_id`, `provider_appointment_version`, `provider_status` | External reconciliation API/UI only | Requires an external attempt/target origin before display or mutation | Retained active external evidence | Removal blocked while owner reconciliation attaches exact provider mirrors. |
| Square test-booking `pos_booking_id`, POS/provider version and outcome fields | Integrations test and cleanup routes | Endpoint is explicitly external/Square and is gated to `external_provider`; IDs remain required confirmation/cleanup evidence | Retained external compatibility surface | Removal blocked until a versioned replacement exists for Square test create/cancel and exact replay/cleanup. |
| Public catalog and landing payload | `backend/modules/public_catalog`, `landing/lib/types.ts`, public salon page | Public reads expose scheduling authority/version and public-safe catalog only; no POS/provider IDs, readiness payloads, tokens or `active_pos_provider` | Required public/config export exclusion | Public JSON tests and landing types must remain provider-ID-free. Any new field requires a public privacy/compatibility review. |
| Configuration-transfer bundle and preview | `backend/modules/config_transfer`, frontend import preview | Scheduling authority/version/history, provider mappings/connections, operational evidence and secrets are excluded. `active_pos_provider` is reported as adapter intent only | Required transfer exclusion plus retained adapter-intent compatibility | Schema-version tests must prove exclusions, exact destination authority fence and no implicit switch. Removal of adapter intent requires a new schema version. |

## Response Compatibility

The neutral scheduling endpoints are additive discriminated unions. Consumers
must branch on `kind` and `scheduling_authority`; they must not use a nested
legacy field to reinterpret the result. Existing external booking and Square
routes remain compatibility surfaces.

Nullable legacy provider/POS aliases are omitted for internal appointment JSON.
External responses continue to carry the provider IDs, versions, attempt
outcome, retry and reconciliation evidence required by their existing routes.
This cleanup therefore changes generic interpretation, not external execution
or historical evidence.

## Audit Commands

Before release, review every hit rather than accepting a zero-count shortcut:

```sh
rg -n "pos_booking_id|pos_appointment_id|pos_provider|active_pos_provider|provider_outcome|reconciliation_status" backend frontend pos-calendar landing
rg -n "pos_booking_id|pos_appointment_id" frontend pos-calendar
```

Allowed UI hits are external detail/display, Square-specific setup/test, or
external reconciliation code that has already checked the persisted authority.
Any generic action, badge, mapper, confirmation message, or lifecycle gate that
uses those fields before authority/native evidence fails this removal gate.
