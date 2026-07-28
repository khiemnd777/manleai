# AI Receptionist Domain Context

This file defines shared language for agents working in this repo. Use these terms consistently in code, docs, tests, PRDs, and review notes.

## Product Language

**Salon**
The tenant business using the AI receptionist. All operational data is scoped by `salon_id`.

**Owner**
The primary salon user who configures settings, connects Square, receives handoffs, and reviews dashboard activity.

**Scheduling Authority**
The salon-scoped contract that owns availability, booking execution, and the
durable evidence required for lifecycle confirmation. The exact protocol values
are `owner_manual`, `manleai_calendar`, and `external_provider`.

**Owner Manual Scheduling (`owner_manual`)**
The AI collects a structured request for owner review. It does not promise
availability or automatically confirm, reschedule, or cancel an appointment.
Phase 2 implements request-only availability plus durable pending requests for
book, reschedule, and cancel through `scheduling_owner_manual`, with no provider
call and no appointment creation.

**ManleAI Calendar Scheduling (`manleai_calendar`)**
ManleAI-owned hours, staff schedules, conflict rules, and appointment ledger
own scheduling. Confirmation requires an atomic internal commit that returns a
durable appointment ID.
Phase 4B implements verified aggregate availability and conflict-safe atomic
create for structured multi-guest, multi-service staff-only and pooled plans.
Phase 4C implements whole-root internal reschedule and cancel against the
persisted target origin and exact appointment version. V52-V55 implement
explicit owner-reviewed authority preview/commit, immutable audit history, and
an inverse-run reference; activation or provider integration never switches
authority implicitly.

**ManleAI Calendar Configuration**
The salon-owned policy and readiness aggregate introduced by V48. It contains
the root booking policy/config fence; only `local_override` salon hours; staff
weekly schedules and eligible services; per-service enablement, capacity mode,
eligible staff, buffers, and optional resource requirements; resource pools;
temporary exceptions; immutable configuration events; backend constraints;
and configuration/execution readiness. It is configuration data, not an
availability result or appointment ledger.
Staff-to-service eligibility belongs to the configuration independently of
whether the related service policy has been configured yet, so Staff-first and
Service-first setup converge without an order dependency.

**Configuration Readiness**
`configuration_ready` means the current persisted ManleAI Calendar aggregate
has a root config, local hours, eligible/configured/enabled services, eligible
staff assignments with schedules, and valid resource requirements. It does not
authorize scheduling execution or confirmed wording.

**Operation Capabilities**
The operation-granular ManleAI Calendar contract:
`staff_only_availability`, `staff_only_create`, `pooled_capacity`,
`party_create`, `reschedule`, and `cancel`. Phase 4C can enable the first two
when configuration, current activation, selected authority, and at least one
staff-only policy are ready. `pooled_capacity` can become true when that engine
fence is ready and an enabled pooled policy exists; `party_create` can become
true when the engine fence is ready, at least one service is enabled, and the
configured maximum party size exceeds one. Consumers must gate an action with
its exact capability instead of treating aggregate readiness as permission.
`reschedule` and `cancel` become true under the same selected-authority,
configuration, and current-activation engine fence when at least one service is
enabled. These aggregate capabilities describe current new-work readiness;
historical lifecycle dispatch still follows the persisted appointment origin.

**Execution Readiness**
`execution_ready` means all six declared operation capabilities are currently
ready. In Phase 4C it becomes true only when staff-only availability/create,
pooled capacity, party create, reschedule, and cancel are all true. A false
aggregate remains compatible with a true operation-specific capability, and it
does not orphan a persisted internal lifecycle target.
The root activation fields record the latest owner activation for one exact
config version, and immutable config events retain each activation action. A
later scheduling-relevant change makes the latest activation stale and
requires re-activation; activation does not switch authority.

**External Provider Scheduling (`external_provider`)**
The selected external adapter owns availability and provider-side mutation
results. Confirmation requires provider success with the required booking ID
and metadata. Square Appointments is the only currently implemented real
external adapter; operational production readiness requires separate live-
provider evidence.
The implemented external executor delegates to the exact existing booking/POS
path. It remains the only provider-backed confirming executor; Phase 4B
ManleAI Calendar confirmation uses separate authority-native evidence.

**Originating Authority**
The immutable authority captured by an appointment, attempt, quote, or segment.
Historical actions and retries use this origin even after the salon explicitly
switches its selected authority.

**Authority Readiness**
The selected mode's scheduling prerequisites. Readiness allows an attempt; it
is not confirmation evidence.

**Scheduling Resolver**
The implemented authority-neutral boundary in `backend/modules/scheduling`.
For genuinely new availability/create work it reads
`salon_settings.scheduling_authority` with an owner check. Existing operations,
retries, target-aware availability, reschedules, and cancellations use the
persisted booking-attempt, scheduling-request, or target-appointment origin,
and every origin present for one mutation must agree. It dispatches legacy
booking API, additive neutral scheduling API, conversation, and Square test
writes. Candidate lookup and response-loss replay are provider-free history
reads. Selection is data-driven; caller wording and integration state do not
choose an authority. Its owner-scoped current-authority read gates new Square
test creation and AI-booking enablement. Its read-only create-lineage resolver
permits a persisted external safe retry after a current-mode switch, without
blocking exact external replay or target-origin cancellation.

**Scheduling Request**
The `owner_manual` aggregate stored in `scheduling_requests`, ordered immutable
`scheduling_request_segments`, and append-only
`scheduling_request_events`. It represents book, reschedule, or cancel work in
`pending`, `contacted`, `resolved`, or `dismissed` status. It is not a booking
attempt, appointment, provider fallback, POS error, or reconciliation task.

**Owner Review Queue**
The Appointments-page workflow where an owner filters, inspects, and marks a
scheduling request contacted, resolved, or dismissed. Review status is audit
state only and never proves an appointment was confirmed, changed, or
cancelled.

**Owner Notification**
A durable owner-operational in-product/outbox record linked to work such as a
pending scheduling request. Queueing or delivering it does not confirm,
reschedule, cancel, or resolve the related scheduling work.

**Owner Notification Delivery**
The provider-neutral V56 delivery state, attempt/event/action evidence, worker
claim/retry boundary, and salon-scoped owner operations. Provider acceptance is
not delivery proof. Only signed monotonic provider evidence may record
`delivered`; an ambiguous post-dispatch outcome is dead-lettered and cannot be
automatically or manually requeued. Current Twilio support is owner-
operational SMS only and does not enable customer SMS or customer-consent
management.

**Canonical Operational Data**
ManleAI-owned salon data such as services, staff, customers, AI controls,
scheduling-authority settings, owner workflow state, pending requests, logs,
and training data. External providers are projections and, only under
`external_provider`, booking execution layers; they are not the primary
identity for this data.

**AI Receptionist**
The voice agent that answers inbound calls, collects booking details, answers FAQs, and routes work to booking and handoff tools.

**AI Receptionist Tone**
A salon-scoped speaking-style preset stored on `salon_settings.ai_tone`. It can make backend-approved replies sound warmer, more natural, younger, or more concise, but it is not a booking rule, provider credential, or free-form prompt override.

**AI Consultation**
A conversation lane that helps a caller clarify needs, compare eligible services, and choose one service from owner-approved facts. It does not own availability or POS tools. A selected recommendation becomes a booking draft only after the caller also expresses booking intent.

**Service Consultation Profile**
The structured, owner-approved child record for one canonical service, stored in `service_consultation_profiles` with a stable `(salon_id, service_id)` key and revision. Only `ready` profiles with at least one recommended outcome and one compatible current system on active-provider, POS-linked, AI-bookable services participate in recommendation ranking; draft, incomplete, and disabled profiles remain management data.

**Guidance Service Catalog**
Canonical services that own service identity, aliases, categories, menu
answers, and consultation independently from confirmation evidence. The current
Square-backed runtime builds this catalog from active-provider, linked,
versioned, synced, AI-bookable services. Authority-specific scheduling
eligibility is derived separately from the selected authority, without a
provider-link requirement for internal modes. Guidance does not authorize
availability or booking.

**Authority-Eligible Service Catalog**
The guidance catalog intersected with the selected authority's readiness.
`owner_manual` uses active, non-archived, AI-bookable canonical services with a
positive duration and active canonical staff without requiring a POS link, and
may collect an owner-review request without promising a slot;
`manleai_calendar` requires owner-managed service/staff and internal schedule
readiness, with Phase 4B availability/create supporting capability-gated
staff-only and pooled multi-guest/multi-service plans;
`external_provider` requires the current provider connection, location,
snapshot, and entity-link fence.

**Booking-Ready Service Catalog**
The current `external_provider` implementation of authority eligibility: the
guidance service catalog intersected with the active provider connection,
selected location, completed snapshot generation, and `last_sync_at` fence.
Provider availability, booking, and reschedule calls require every selected
service to be booking-ready. A guidance recommendation alone never crosses
this gate.

**Public Catalog Readiness**
The owner-publish contract for the separate customer landing page. It follows
the persisted scheduling authority but never performs availability or booking:
`owner_manual` needs a slug and one active canonical AI-bookable service with
no staff or POS requirement; `manleai_calendar` additionally requires enabled
canonical services, current activation, and `local_override` hours; and
`external_provider` requires a current active connection with synced/linked
service and staff projections. Publishing is revalidated under the scheduling
authority fence, and every public read fails closed when readiness becomes
stale. The page is call-to-request only and exposes no provider identifiers.

**Conversation Engine**
The state machine and tool-calling layer behind the AI Receptionist. It must not know Square payloads, OAuth tokens, or provider-specific booking details.

**Turn Understanding**
A typed, catalog-validated semantic interpretation of one caller turn. One turn may contain multiple ordered operations and questions, such as correcting a service, changing a technician, asking about availability, and conditionally accepting a review. The configured semantic interpreter is not gated by a fixed service-edit keyword list. Deterministic catalog/date/phone evidence remains authoritative, and model output cannot mutate state or call booking tools directly.

**Conversation Act**
One validated operation inside a turn understanding, such as add, replace, remove, set, clear, undo, or authorize review. Service replacement keeps source, target, scope, and guest scope separate; staff, date/time, guest, and customer corrections use the same reducer boundary.

**Appointment Draft**
The mutable, non-confirmed service, guest, staff, date/time, and customer-detail selection collected during a call. Draft acknowledgements and final-review acceptance are not confirmed appointments. Service, staff, or time mutations invalidate dependent availability and require a fresh review before booking execution.

**Dialog State**
Versioned call-session state that records the current conversation phase, pending typed clarification, active consultation needs/recommendations/profile revisions, bounded mutation history, no-progress count, and draft/review/authorization revisions. Booking authorization is valid only when all three revisions identify the same unchanged draft.

**Service Understanding**
The backend-owned interpretation layer that maps customer service utterances to active salon services. It uses the salon catalog, service categories, active service aliases, and active service category aliases; asks clarification for category, generic, or fuzzy family matches; and records diagnostic transcript metadata.

**Fuzzy Service Confirmation**
A persisted, state-scoped confirmation of one concrete current catalog
candidate. Exact catalog and alias selections keep their established behavior;
a fuzzy single-candidate result does not mutate the draft, availability proof,
or booking state until `dialog_state.pending.prompt_key` is
`fuzzy_service_confirmation` and the caller explicitly confirms that catalog
identity. Rejected, unclear, stale-catalog, or wrong-state input does not book
or guess a replacement service. The bounded affirmative grammar is valid only
inside that one pending state and is not a general intent classifier.

**Service Alias**
A salon-scoped learned phrase for a real service, stored in `service_aliases` with a normalized alias key. Owner corrections can create or update service aliases; aliases are operational data, not prompt text.

**Service Category**
A salon-scoped menu group such as Manicure, Pedicure, Acrylic, Dip Powder, or Removal, stored in `service_categories`. Categories help the AI answer and clarify grouped service requests; they do not replace real services and are not directly bookable.

**Service Category Alias**
A salon-scoped phrase for a service category, stored in `service_category_aliases` with a normalized alias key. Category aliases route broad caller wording such as "mani" or "take off" to a category-level clarification, not to a single service.

**Party Booking Request**
A structured exception-review record in the existing conversation party
workflow. Phase 2 `owner_manual` scheduling instead persists one
`scheduling_request` with ordered quantity/guest-aware segments for the whole
group. Phase 4B `manleai_calendar` uses one aggregate quote and one atomic root/
child internal commit; `external_provider` retains its established preflight,
all-child success, rollback, and reconciliation behavior. Both are all-or-none
and must not confirm unless every required child has authority-specific success
evidence.

**Booking Service**
The established external-provider domain service that creates, reschedules,
cancels, and records appointments through the active `POSProvider`. Booking API
and conversation callers now enter through the Scheduling Resolver;
`scheduling_external_provider` delegates to this service without changing its
POS safety. Implemented internal modes remain outside `POSProvider` and must
not be represented as fake POS adapters.

**POS Provider**
The external-provider-neutral interface in `backend/modules/pos`. Only the
external-provider executor uses this interface. Internal scheduling authorities
do not use it.

**POS Adapter**
A concrete provider implementation such as `SquareAdapter`. It owns provider-specific auth, API calls, payload mapping, retries, and error normalization.

**SquareAdapter**
The first real POS Adapter and the only real external-provider adapter currently
implemented in this repository. Its operational production readiness is a
separate live-provider evidence gate.

**POS Entity Link**
A mapping between a ManleAI canonical record and a provider-side record, such as a service, staff member, or customer. Provider IDs are mappings, not primary product identity.

**Provider Projection**
Provider-side data imported from or written to an active POS provider so ManleAI records can be used for availability and booking execution.

**Sync Status**
The state of a canonical record's provider mapping, such as `local_only`, `syncing`, `synced`, `sync_failed`, `unmapped`, or `archived`.

**AI Bookable**
An internal dashboard control whose eligibility is scheduling-authority
relative. The current `external_provider` runtime requires an active canonical
record with a valid active-provider link. Current `owner_manual` uses active,
non-archived canonical records without provider links and additionally requires
a positive service duration. `manleai_calendar` derives
service/staff eligibility from those same canonical active, AI-bookable,
non-archived records, positive service duration, and owner-managed schedules
without fake provider links. Phase 4B schedules enabled staff-only policies and
enabled pooled policies with current activation, exact authority/config
evidence, and sufficient resource capacity.

**Fallback Pending Request**
A non-confirmed fallback from an external automated write that failed or
remains ambiguous. It retains provider/POS error, retry, and reconciliation
evidence as applicable. The normal `owner_manual` result is instead a
`pending_owner_review` scheduling request; it is not a provider fallback, and
internal failures must not be mislabeled as POS errors.

**Booking Attempt**
An auditable record of a customer scheduling flow, including originating
authority, status, authority-native evidence when available, and any error.
External records retain POS provider and booking fields for the Square-backed
runtime. Internal Phase 4C records persist those provider columns as null and
use the internal appointment/attempt/event/service/resource ledger instead.

**POS Error**
A normalized, salon-scoped record of provider failures. Use codes such as `POS_TOKEN_EXPIRED`, `POS_BOOKING_FAILED`, and `POS_TIMEOUT`.

## Product Invariants

- Confirmation is scheduling-authority-specific: `owner_manual` never
  auto-confirms; `manleai_calendar` requires an atomic internal commit and
  durable appointment ID; `external_provider` requires provider success and the
  required booking ID and metadata.
- The authority-neutral resolver has three registered executors with distinct
  result classes: `owner_manual` returns request-only availability and pending
  owner review; Phase 4C `manleai_calendar` returns verified aggregate slots
  and confirms create/reschedule/cancel only after the corresponding atomic
  durable internal commit; `external_provider` can
  return provider-confirmed appointment evidence or an external fallback.
  Configuration or activation alone is not execution permission, and
  aggregate `execution_ready=false` does not negate a true operation-specific
  staff-only, pooled-capacity, or party-create capability.
- AI conversation booking policy is separate from scheduling authority:
  `pending_approval` may use the selected authority only to verify a requested
  time before creating one non-reserving owner-review request;
  `confirmed_booking` uses the authority's existing confirming executor; and
  `disabled` performs no origin-free scheduling action. Exact persisted
  operation replay remains bound to its originating authority after either
  setting changes.
- ManleAI owns canonical salon operational data. The captured authority owns
  scheduling execution, and `active_pos_provider` selects an adapter only
  inside `external_provider`.
- `salons.timezone`, canonical `services`/`staff`, V48 calendar policy tables,
  and only `salon_business_hour_periods.source=local_override` own ManleAI
  Calendar configuration. Provider-imported hours and POS mappings do not
  satisfy internal readiness.
- V49 owns authority/config-fenced availability quotes, internal appointment
  and booking-attempt evidence, immutable execution events, and active internal
  staff-overlap protection. Internal availability uses half-open intervals,
  rejects ambiguous or nonexistent DST wall times, and fails closed when
  external conflict evidence cannot identify a safe staff/time range.
- V50 owns the aggregate database guards: quote resource integrity, exact
  committed book graph, guest-reference/party-size invariants, sorted pool
  locking, effective base/override capacity validation, concurrent
  over-capacity prevention, and immutable consumed quote/execution history. It
  adds no second reservation ledger.
- V51 owns internal lifecycle release/version guards. Every root version has
  one unique execution event; the exact prior active plan is released once and
  attributed to the lifecycle attempt; a reschedule installs one exact current
  replacement plan; a cancel stores the exact old snapshot and leaves zero
  active children; cancelled roots are terminal.
- A Phase 4B create consumes one exact ordered guest/service/staff/resource
  quote only after revalidating authority, config, activation, catalog,
  schedules, staff conflicts, resource conflicts, and capacity in one
  transaction. It commits one root appointment and all child service/resource
  evidence or nothing. Exact replay after response loss returns the same root
  IDs and child graph; reuse with changed logical input conflicts.
- A Phase 4C reschedule preserves the whole target's party size, ordered
  service units, and guest mapping. Target-aware availability and execution are
  bound to the exact target version; the current configuration and activation
  fence must still be valid, and the old plan is released only in the atomic
  transaction that commits the new plan and advances the root version.
- A Phase 4C cancel accepts no quote or replacement segments. It resolves the
  internal target origin, requires the exact current target version and an open
  cutoff, releases the exact active plan, writes its immutable attempt snapshot,
  advances the root to `cancelled`, and returns `active_child_count=0` without
  requiring the current salon authority or current config activation.
- Exact lifecycle response-loss replay returns the original event's target
  version, result version, status, attempt ID, and historical child snapshot
  even after later mutations. Current appointment/candidate hydration includes
  only the current version's unreleased plan.
- Every ManleAI Calendar configuration mutation is owner-scoped, action-key
  idempotent, guarded by an expected monotonic config-version fence, and
  recorded by an immutable event. The version may advance more than once when
  one logical mutation replaces multiple children; clients must treat the
  returned version as a fence, not an edit count.
- Service recognition must be catalog-backed and salon-scoped. Do not hardcode per-salon service keywords into prompts or generic matchers.
- Service categories are menu/understanding configuration, not confirmed-booking entities. Category matches should clarify to real services before availability or booking.
- AI consultation may rank only eligible services with complete `ready` owner-approved profiles. Semantic model output may extract needs, field-level consultation mutations, catalog IDs, and a safety assessment but cannot recommend a service, mutate persisted state, or call POS tools directly.
- Service selection during consultation is not booking intent. The runtime must receive both a concrete selected service and an explicit booking request before entering booking collection; consultation-only calls may complete with `consultation_completed`.
- Consultation safety concerns involving pain, injury, infection, allergy, bleeding, swelling, or medical suitability require owner handoff and no medical advice. Deterministic safety evidence is checked before normal routing, and validated structured safety evidence is checked before any state mutation or tool action.
- Group or party operations are all-or-none. `owner_manual` creates pending
  owner review; `manleai_calendar` must commit every child atomically;
  `external_provider` must prove every required provider child succeeded or one
  aggregate provider booking covers every segment. Partial or unknown outcomes
  remain unconfirmed and require rollback/reconciliation as applicable.
- AI tone changes reply style only. It must not change required booking slots, handoff decisions, availability checks, service selection, or confirmed-booking wording.
- Pending clarification is context, not a closed vocabulary. A caller may ask an informational question without losing the draft, or explicitly change to a different catalog-backed target without being trapped in stale candidates.
- Final review is mandatory for persisted production dialog state before a new
  automated booking write. Accepting the review authorizes the attempt but is
  not confirmation; only the captured authority's durable success evidence
  creates confirmed wording.
- A configured semantic turn interpreter runs for every freeform turn that reaches conversation orchestration, including wording not present in a phrase list. Provider-event dedupe and already-scoped confirmations may resolve earlier. Invalid, low-confidence, unavailable, or non-catalog interpretation must preserve the draft and clarify or hand off safely.
- Authority switching is explicit and preserves originating authority. A
  provider connection, sync, webhook, import, or configuration transfer must
  not switch authority implicitly or move historical appointments.
- After SaaS cutover, configuration transfer is a Platform tenant-detail
  workflow. It requires reviewed source/target scope and actual-actor
  capability checks, transfers only portable canonical configuration, and
  never transfers active-provider selection, provider connection state,
  credentials, provider-imported hours, calls, customers, appointments, or
  operational history.
- Platform JSON input uses v9 or compatibility v8. Legacy v7 is accepted only
  as an explicitly scoped content pack containing categories, aliases,
  consultation profiles, or knowledge; it is canonicalized to v8 before
  review. Runtime/provider v7 and v1-v6 remain unsupported.
- Historical reads, reconciliation, provider sync, Square webhooks, and worker
  repair preserve their existing external-provider behavior. External lease
  recovery and provider-calendar mutation are explicitly fenced to external
  origins. Square webhook target/repair selection remains provider/connection-
  scoped so historical external mirrors can converge after a later switch;
  downstream writes cannot overwrite internal-origin rows. The authority
  boundary must not reinterpret those records as internal scheduling.
- Exact external create-mirror canonicalization records missing confirmation
  time/source from the active evidence transition, preserves existing
  provenance, and does not invent an owner actor. Historical V46 backfill
  remains null where the old data cannot prove those facts.
- Square is the first real external-provider integration; future provider names
  are architecture targets, not implemented features.
- Vietnamese language support is product scope, but English remains the primary commercial release language unless a feature explicitly says otherwise.

## Avoid These Terms

- "Universal POS support" for the current commercial release.
- "Square is the source of truth" for all salon operational data.
- "Confirmed" for an `owner_manual` request, a failed/ambiguous internal commit,
  or an external-provider result without required booking evidence.
- "POS-first" as a universal product prerequisite; scope it to the current
  external-provider runtime when describing existing implementation.
- "Mock POS" as a production path.
- "Placeholder" for required production behavior.
