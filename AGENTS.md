# AGENTS.md

## Project Mission

This repository builds an Owner-first AI phone receptionist for US nail salons,
starting with Vietnamese-owned salons and optional Square Appointments
integration.

The product rule is strict and scheduling-authority-specific. `owner_manual`
creates a pending owner-review request and never confirms automatically.
`manleai_calendar` confirms only after an atomic internal commit returns a
durable appointment ID. `external_provider` confirms only after the selected
provider succeeds and returns the required booking ID and metadata. Party
operations are all-or-none, and authority switches are explicit while every
historical operation preserves its originating authority. The currently
confirming executors are Square-backed `external_provider` and Phase 4C
`manleai_calendar` for structured multi-guest, multi-service staff-only and
pooled create plus whole-root internal reschedule/cancel. Phase 2 also
implements `owner_manual` as a request-only, pending-owner-review executor with
no appointment or provider side effect. V52-V55 and the Platform tenant
Technical UI implement explicit reviewed authority preview/commit, immutable audit history, and
an explicit inverse-run reference; integrations never switch authority
implicitly.

## Mandatory Doc Routing

Do not duplicate the source docs here. Before answering, diagnosing, planning,
or implementing work in any area below, read the listed source docs first and
treat them as binding:

- Architecture/current module ownership: `docs/architecture.md`
- Mandatory agent mapping, feature/function/UI routing, and triage keywords:
  `docs/agents/codebase-map.md`
- Scheduling authority, confirmation evidence, party atomicity, and authority
  switching: `docs/scheduling-authority.md`
- POS booking, Square, and confirmation safety: `docs/pos-adapter-layer.md`, `docs/square-integration.md`
- API surface and authenticated status/debug endpoints: `docs/api.md`
- Production, deployment, provider runtime config, and dashboard-managed provider settings: `docs/deployment.md`
- Production readiness scope: `docs/production-readiness-checklist.md`
- Conversation AnswerContext query-count, multi-replica freshness,
  observability, bounded EXPLAIN, and local performance evidence:
  `docs/operations/conversation-answer-context-performance.md`
- Legacy POS/provider compatibility inventory, response interpretation, and
  removal gates: `docs/operations/owner-first-compatibility.md`
- Tagged Owner-first code, PostgreSQL, tenant, and security release gate plus
  operational limitations: `docs/operations/release-gate.md`
- Worker heartbeat ledger, owner-scoped queue health, status classification,
  and deployment/incident use: `docs/operations/operations-health.md`
- Owner-operational SMS delivery, strict Twilio messaging configuration,
  callback verification, retry/dead-letter policy, and incident handling:
  `docs/operations/owner-notification-delivery.md`
- Voice, Twilio, OpenAI, realtime phone behavior, and live phone demo setup: `docs/api.md`, `docs/agents/phone-booking-demo-config-memo.md`
- Design/UI/UX contract: `DESIGN.md`
- Domain language and product scope: `CONTEXT.md`

If the relevant doc cannot be read, say that before giving the answer. If a
change adds, deletes, renames, or materially changes a `.md` file that affects
agent routing, product behavior, operational setup, source-of-truth ownership,
or validation workflow, update this routing rule or the relevant agent rule in
the same approved documentation scope.

## Provider Configuration Source Of Truth

- Active Square, Twilio, and OpenAI configuration is salon-scoped operational
  data stored in `salon_integration_configs` and managed through
  `/platform/tenants/:tenant_id/technical`.
- For active-runtime claims or diagnosis, inspect the Platform-backed API
  state (`GET /api/platform/tenants/:tenant_id/technical/integration-configs`), the relevant readiness or
  debug endpoint such as `GET /api/salons/:id/voice/status`, persisted provider
  state, and the runtime resolver code as applicable.
- Never inspect, quote, or infer active provider configuration from `.env`,
  `project.env`, env templates, Docker Compose defaults, GitHub secrets, or
  process environment values. Those sources are not evidence of the
  salon-scoped configuration currently used by runtime.
- Legacy environment fallback code may be considered only when the task
  explicitly targets bootstrap/legacy fallback behavior and code or database
  evidence proves that no stored provider config exists for the salon. Label
  that path as legacy fallback, never as current UI-managed behavior.
- Repository env files and env templates are infrastructure-only. Do not add
  Square, Twilio, or OpenAI provider settings back to them.

## Scheduling Authority Source Of Truth

- Read `docs/scheduling-authority.md` before planning or changing availability,
  booking, reschedule, cancellation, party booking, calendar, confirmation,
  readiness, provider integration, or scheduling settings behavior.
- The exact protocol tokens are `owner_manual`, `manleai_calendar`, and
  `external_provider`. They are stable contract values, not caller-intent
  keywords or presentation copy.
- The persisted salon scheduling-authority setting owns the selected mode.
  `active_pos_provider` selects an adapter only inside `external_provider`; a
  connected integration never implicitly changes scheduling authority.
- The authority-neutral resolver/boundary is implemented in
  `backend/modules/scheduling`. A genuinely new availability/create operation
  resolves `salon_settings.scheduling_authority` with an owner check. Existing
  operation keys resolve across persisted `booking_attempts` and
  `scheduling_requests`; retry attempts resolve the booking-attempt origin;
  target-aware availability and appointment mutations resolve the persisted
  target-appointment origin. Every origin present for one operation must agree
  or fail before dispatch. Authority-sensitive booking HTTP, neutral scheduling
  API, conversation actions, and Square test writes must enter this boundary.
- `RescheduleCandidates`, `ReplayCreate`, and `ReplayCancel` are provider-free
  history delegation. They do not select an executor or call a provider.
- `scheduling.Service.CurrentSchedulingAuthority` is the owner-scoped read for
  new-work readiness. Square readiness exposes this token and gates new Square
  test creation plus AI-booking enablement to `external_provider`.
  `ResolveCreateSchedulingAuthority` is the read-only operation/retry-lineage
  resolver used before non-replay Square test creation; it falls back to the
  current token only when no persisted origin exists and never dispatches a
  provider. Exact external replay, a persisted external safe retry, and target-
  origin cancellation remain available after a later switch so historical work
  is not orphaned.
- Current provider-backed confirming behavior remains the Square-backed
  `external_provider` executor in
  `backend/modules/scheduling_external_provider`, which delegates to the exact
  existing booking/POS path. The boundary must not weaken provider safety.
- `owner_manual` is implemented by
  `backend/modules/scheduling_owner_manual` through the neutral
  `CheckAvailability`/`ExecuteAction` contract. Availability returns
  `request_only`; book/reschedule/cancel create or exactly replay one durable
  pending owner-review request. It must not create an appointment, booking
  attempt, POS error, reconciliation task, or automatic confirmed,
  rescheduled, or cancelled wording.
- `scheduling_requests`, immutable `scheduling_request_segments`, and immutable
  `scheduling_request_events` own the Phase 2 request aggregate. Creation,
  initial event, call-session link, and deduplicated queued
  `owner_notifications` outbox row commit together. A queued row is not proof
  that any SMS, email, or push was delivered.
- V56 and `backend/modules/notification_delivery` own provider-neutral owner-
  notification claims, attempts, immutable events/actions, monotonic callbacks,
  and bounded retry/dead-letter policy. `backend/modules/notification_twilio`
  owns Twilio Messaging REST/signature translation. Provider acceptance is not
  delivery proof; ambiguous post-dispatch outcomes cannot retry. Owner SMS uses
  only strict salon-scoped database configuration and does not enable customer
  SMS or customer consent management.
- Owner review statuses are `pending`, `contacted`, `resolved`, and `dismissed`.
  Status changes are optimistic-versioned, action-key idempotent, event-audited,
  and never appointment confirmation.
- The Tenant Appointments page contains the owner-review queue, scheduling-readiness summary, Phase 4B
  structured multi-guest, multi-service internal create, and Phase 4C whole-
  root internal reschedule/cancel workflows. Masked notification recovery is a
  Platform Operations workflow. Explicit authority selection, preview, review,
  commit, conflict/recovery, inverse-run context, and internal technical
  configuration live in the Platform tenant Technical tab.
- `manleai_calendar` has a registered executor for verified aggregate
  availability and atomic create across structured guests, ordered service
  units, concrete staff assignments, and pooled resource allocations. Phase 4C
  adds target-origin, version-fenced whole-root reschedule/cancel with exact
  lifecycle replay and no POS/provider evidence.
- Phase 3 `manleai_calendar` configuration is owned by
  `backend/modules/scheduling_manleai_calendar` and V48. The aggregate combines
  the root policy/config fence, ManleAI-owned local hours, canonical staff and
  service policy children, optional resource pools, exceptions, immutable
  configuration events, backend constraints, and typed readiness blockers.
- `configuration_ready` proves only that the persisted configuration passes
  the configuration rules. Phase 4C exposes operation capabilities:
  `staff_only_availability` and `staff_only_create` become true only when the
  selected authority, current activation/config fence, and staff-only policy
  are ready. `pooled_capacity` becomes true when the same engine fence is ready
  and at least one enabled pooled policy exists; `party_create` becomes true
  when that fence is ready, at least one service is enabled, and the configured
  maximum party size exceeds one. `reschedule` and `cancel` become true under
  the same selected-authority/current-activation engine fence when at least one
  service is enabled. Aggregate `execution_ready` is true only when all six
  declared capabilities are true. These readiness flags describe new work;
  persisted internal targets still resolve by originating authority after a
  later authority change. Activation records owner
  audit for one exact config version; any later scheduling-relevant change
  makes it stale and requires re-activation. Activation does not switch
  authority.
- Every configuration mutation requires a stable `action_key` and
  `expected_config_version`. Exact action replay returns the current aggregate;
  changed payload reuse conflicts; stale versions fail. The config version is
  a monotonic fence that may advance more than once for a multi-child mutation,
  not a mutation counter.
- `salon_business_hour_periods` rows with `source=local_override` are the only
  hours eligible for internal-calendar readiness. Provider-imported and legacy
  migrated hours remain separate and must not be copied or reinterpreted.
- Staff-to-service eligibility is a canonical tenant-fenced relationship that
  can be managed from the Staff parent before a service-policy row exists; do
  not introduce setup-order dependency between Staff-first and Service-first UI.
- ManleAI Calendar technical UI lives in the Platform tenant Technical tab:
  salon-wide policy, local hours, staff weekly schedules, service execution
  policies, shared resources, exceptions, activation, and authority switching.
  Tenant and Platform Business surfaces still share canonical service, staff,
  staff-service eligibility, price, and business-hour management. Tenant
  Appointments shows business scheduling work and owns structured internal
  create plus whole-root lifecycle workflows.
- V49 owns authority/config-fenced availability quotes, internal booking and
  appointment evidence, immutable execution events, normalized slot/resource
  evidence, and the active internal staff-overlap exclusion. Internal rows have
  no persisted POS/provider evidence.
- V50 adds database-enforced aggregate quote integrity, exact committed book
  graph and guest-party invariants, sorted pool locking, capacity/override
  validation, concurrent over-capacity prevention, and immutable consumed quote
  and execution history without creating a second reservation ledger.
- V51 adds lifecycle release ownership and event-version uniqueness, permits
  exactly one owned release of an active internal plan, makes cancelled roots
  terminal, and validates a contiguous one-event-per-version lifecycle graph.
  Reschedule must release the exact old plan and atomically install the exact
  current quote/attempt/new-plan graph. Cancel must release the exact old plan,
  persist that old plan as attempt history, and leave no active child plan.
- Internal availability uses canonical services/staff, V48 policies,
  `local_override` hours, weekly schedules, buffers, exceptions, resource
  requirements, resource-pool capacity and overrides, notice and horizon rules.
  It assigns every quantity-one segment deterministically, schedules services
  for the same guest sequentially, allows different guests to overlap only when
  staff/resource capacity permits, rejects ambiguous or nonexistent DST wall
  times, and treats intervals as half-open `[start,end)`.
- Internal create revalidates the exact ordered guest/service/staff/resource
  graph under current authority, config, activation, catalog, schedule,
  conflict, and capacity fences. It commits one root appointment plus every
  child service and resource allocation atomically. Exact committed replay
  after response loss returns the same durable root appointment and attempt IDs
  and child graph; changed operation-key reuse conflicts.
- Internal reschedule is a whole-root replacement. Target-aware availability
  is bound to the persisted internal target and its exact appointment version;
  it preserves party size, service order, and guest mapping while returning a
  newly assigned complete plan. Execution requires the current internal config
  and activation fence, revalidates the exact quote and replacement graph,
  releases the prior active plan, advances the root by one version, and commits
  the new plan atomically.
- Internal cancel requires no availability quote and does not require the
  current salon authority or a current config activation. It still requires
  the persisted internal target origin, exact expected target version, and an
  open cancellation cutoff. It releases the exact active plan atomically,
  records that prior plan as immutable attempt evidence, advances the root by
  one version, and leaves zero active children. A missing cutoff is treated as
  no backend cutoff; dashboard clients fail closed when cutoff evidence is
  missing or invalid.
- Exact lifecycle replay returns the historical event's attempt snapshot,
  target version, result version, status, and child plan even after later
  lifecycle mutations. Candidate and appointment hydration expose only the
  current version's unreleased active plan, so cancelled roots have no active
  children and historical released plans are not presented as current state.
- `manleai_calendar` requires an atomic conflict-safe internal transaction and
  a durable appointment ID before confirmation.
- `external_provider` retains every provider ID, version, fence, idempotency,
  fallback, error, and reconciliation safeguard in the POS and Square docs.
- Every appointment mutation, retry, and reconciliation path must preserve and
  use the operation's originating authority. Party operations are all-or-none.
- External lease recovery and provider-calendar persistence/matching/
  reconciliation explicitly fence mutations to `external_provider`. Square
  webhook target/repair selection remains provider/connection-scoped so it can
  preserve historical external mirrors after a later salon switch; downstream
  calendar writes cannot mutate internal-origin rows. Do not describe this
  maintenance as current-setting dispatch.
- Exact external create-mirror canonicalization fills only missing
  `confirmed_at` and `confirmation_source`, preserves existing provenance, and
  does not invent `confirmed_by_user_id`; repeated recovery must be timestamp-
  idempotent.
- Authority-not-ready HTTP responses are generic sanitized `409` errors.
  Square gate handlers must not expose wrapped internal error details.

## Mandatory Mapping Load And Drift Guard

- Before any repository triage, diagnosis, planning, implementation, review, or
  subagent handoff, read `docs/agents/codebase-map.md` and use its feature,
  function, utility, helper, UI, keyword, and owner-surface routing before
  scanning code.
- Treat `docs/agents/codebase-map.md` as the mandatory first-pass map, not as
  proof of current behavior. If code evidence conflicts with the map, code
  evidence wins and the map must be corrected in the same approved scope.
- Any code change that adds, updates, deletes, renames, or moves feature
  behavior, API routes, UI routes/components, services, repositories, provider
  adapters, migrations, DTOs/types, utilities, helpers, tests, runtime config,
  or ownership boundaries must check whether `docs/agents/codebase-map.md`
  needs an update.
- If a code change affects the mapping, update `docs/agents/codebase-map.md`
  before reporting the work complete. If it does not affect the mapping, the
  final response must include `Mapping impact: none` with the reason.
- Subagents and review skills must classify mapping impact as `map update
  required`, `map already accurate`, or `map conflict found` when reviewing or
  planning code changes.
- Do not use triage keywords as production logic. They are only for agent
  routing, search acceleration, and root-cause investigation.

## Repository Layout

- `backend/`: Go/Fiber API, PostgreSQL persistence, startup SQL migrations, Ent schemas, domain modules.
- `frontend/`: Next.js TypeScript admin UI.
- `docs/`: architecture, integration, deployment, testing, and agent guidance.
- `.agents/skills/`: repo-local Codex skills for repeatable workflows.
- `.codex/agents/`: project-scoped custom subagents.

## Conversation Style

- The user may use the Vietnamese "mày-tao" register, but the assistant must not mirror it or use that register to address the user or refer to itself.
- The assistant must use a respectful neutral register such as "mình/bạn", "tôi/bạn", or omit pronouns when possible.
- Before every response, scan the final text for banned self/user pronouns: "mày", "tao", "ta", "mi", and "tớ". Rewrite the response before sending if any appear as assistant self-reference or user address.
- If those words must be mentioned to discuss this rule or quote evidence, keep the quote minimal and clearly frame it as a quoted term, not assistant register.

## Evidence And Source Discipline

- Do not give vendor-console navigation, setup instructions, or operational diagnoses as bare assertions. Ground them in visible user-provided UI/log text, repository files, command output, or official vendor documentation.
- When answering current product behavior, inspect the codebase first if the behavior depends on routes, UI, data models, runtime flow, or repository logic.
- Do not use shorthand labels that mix UI, API, database, and runtime behavior. Split them into separate columns or separate statements.
- Do not say "A or B", "maybe", "could be", or "X / Y" when codebase evidence can prove the exact current state.
- For every claim about existing behavior, identify whether it is confirmed by code, confirmed by screenshot/log, inferred, or proposed.
- If the current code has backend support but no UI, say exactly: "backend exists; UI does not exist."
- If UI exists in a different page than expected, name the exact page and cite the frontend file.
- Use options only for future implementation choices. Label them as "proposal", not as current behavior.
- If correcting a previous answer, state the exact wrong phrase, the corrected statement, and the file evidence.
- When interpreting logs or screenshots, quote the exact field names, values, timestamps, error codes, and request IDs that support the conclusion. Separate confirmed facts from inference.
- When recommending where to click in a third-party console, cite the source of that path: current screenshot/appshot, official documentation URL, or clearly label it as an unverified UI-memory guess and ask the user to confirm the screen.
- Prefer official vendor docs for external-service behavior. Include links when internet sources are used, and avoid half-sourced instructions such as "go to X -> Y" without origin or evidence.
- If the evidence is insufficient, say exactly what is missing and what needs to be inspected next instead of filling the gap with a confident-sounding answer.

## Product Proposal Quality Gate

- Before proposing any product, UI, workflow, or architecture change, complete the proposal gate mentally and expose the relevant parts in the answer when the decision is non-trivial.
- Do not choose UI placement, module ownership, or workflow ownership from backend module names, API route grouping, docs section names, existing implementation accidents, or previous assistant claims alone.
- Identify the actor workflow first: actor, trigger, operational object, user goal, source of truth, and existing neighboring controls.
- Identify the primary parent object and the grain of the new field/action before choosing UI placement. If the requested data is a child attribute, child action, learned phrase, mapping, exception, setting, or status of one parent object, place it inside that parent object's existing row, card, detail panel, or edit flow by default.
- Do not create a standalone panel, page, or management table for child data unless the primary workflow is explicitly cross-object bulk review, audit, import/export, reporting, deduplication, or conflict resolution.
- When a child-control placement is chosen, remove redundant target selectors that are already implied by the parent object. Do not make the owner re-select the service, staff member, customer, category, or appointment currently being edited unless the workflow is intentionally moving the child record to another parent.
- Compare candidate placements or ownership boundaries when more than one location is plausible. Explicitly reject the wrong placement and state why it would confuse ownership, workflow, or source of truth.
- Place features near the workflow and operational object they affect. Structured operational data belongs near the operational management surface; free-text policy/FAQ/notes belong near knowledge/training; provider setup belongs near integrations; review/audit workflows must not be treated as the primary management surface unless that is the actual owner workflow.
- Before presenting a plan, run this failure check: would this confuse structured operational data with free-text knowledge, setup/configuration, provider integration, review/audit state, or unsupported production behavior? If yes, revise the proposal before presenting it.
- If backend/API grouping conflicts with the owner workflow, explicitly name the conflict and prefer the owner workflow unless the backend contract makes that impossible. If the backend contract is blocking, propose the backend contract change separately.
- Use examples only as examples. Do not write rules that solve only the latest mistaken case; write the general decision rule that would have prevented it.

## User Confirmation Gate

- For every feature request, bugfix, refactor, or task that changes code, stop after investigation and present the intended implementation plan for user review.
- Do not edit production code, migrations, tests, frontend files, or runtime configuration until the user explicitly confirms and allows implementation.
- Read-only exploration, repo inspection, and answering questions are allowed before confirmation.
- Documentation-only or agent-rule changes may be applied directly only when the user explicitly asks to change those files.
- Treat phrases such as "need", "cần", "should", "want", "add", "create", "fix", "build", or "implement" as a request to analyze and propose a plan, not as approval to write files.
- Treat questions, complaints, screenshots, appshots, status checks, review requests, "why" prompts, and partial instructions as read-only investigation requests, not approval to write files.
- The latest user message controls write permission. Older approval does not carry over after scope changes, after the assistant sends a final answer, or when the latest user message is a question or complaint instead of an action-oriented approval.
- Do not create, scaffold, patch, format, generate, move, delete, or otherwise write any code, tests, docs, skills, agents, migrations, runtime configuration, or generated artifacts before explicit approval for that exact scope.
- Do not request escalated permissions for any write operation before the user has approved the scope and the specific need for escalation.
- Explicit approval must be clear and action-oriented, such as "approved", "cho phép ghi file", "triển khai theo plan này", or "sửa các file này"; otherwise continue in read-only planning mode.
- One explicit approval is sufficient when it directly answers an agent-proposed scope or plan. Do not ask for a second confirmation for the same scope; execute the approved scope.
- If the user says "implement", "fix", "build", or similar, still present the plan first and wait for explicit approval before changing code.
- For any request that changes the UI or user-facing layout, provide a **Mockup as Text** before implementation and wait for explicit approval.

## Codex Working Contract

- Before any workspace write, decide from the latest user message whether file changes are actually allowed. Workspace writes include code patches, tests, docs, skills, agents, migrations, generated files, formatting commands, and configuration changes.
- File changes are not allowed when the latest message contains any unresolved question, concern, objection, condition, edge case, scope change, ambiguity, status request, screenshot/log review, complaint, or request for explanation. This remains true even when the message also contains approval language.
- File changes are allowed only when the latest message is an action-oriented approval for an exact scope that has already been investigated and planned, and no unresolved decision remains in that latest message.
- Do not use keyword matching as the approval gate. The gate is semantic: ask whether a reasonable user would still expect an answer, clarification, or decision before files are changed. If yes, stay read-only.
- If the user approves but also raises a new edge case or asks what happens in a scenario, answer that decision gate first. If the answer changes scope, risk, files, tests, or behavior, present the updated plan and wait for a fresh action-oriented approval.
- Before any approved implementation, state in commentary exactly which scope is being implemented. If that sentence cannot be stated truthfully and specifically, do not write files.
- For documentation-only or agent-rule changes explicitly requested by the user, the exact write scope is the requested documentation/rule update. Do not expand that scope into code or tests unless separately approved.
- Before implementation planning, load the repo-local skills that fit the work, such as planning, triage, root-cause investigation, review, frontend workflow, backend workflow, booking safety, POS adapter, voice runtime, or business analysis skills.
- Classify the work before planning as one of: task, defect, new feature, refactor, review, documentation-only, or agent-rule change. If more than one applies, name the primary classification and the secondary risk area.
- Decode the smallest relevant codebase surface before proposing implementation: owner module, UI page or component, API route, service, repository, schema, docs, runtime config, logs, screenshot, or vendor evidence as applicable.
- Separate findings into confirmed current behavior, inference, and proposed change. Cite file paths and line numbers, command output, screenshot/log fields, or official docs for claims about current behavior.
- Present a reviewable implementation listing before editing: exact scope, non-goals, business rule, expected files, tests or checks to run, covered edge cases, and remaining unknowns.
- Do not implement until the user has reviewed the listing, has no unresolved questions or scope changes, and gives clear action-oriented approval for that exact scope.
- Never solve product, conversation, workflow, parsing, UI, or test issues by hardcoding the latest user phrase, transcript wording, screenshot text, salon name, service name, date, time, staff name, customer name, or narrow example.
- Treat user examples as evidence and regression fixtures, not product logic. Runtime behavior must be data-driven, state-driven, catalog-backed, provider-backed, or contract-driven as appropriate.
- Do not use keyword-only gates for user approval, caller intent, service selection, escalation, or conversation behavior when semantic state or structured data is required.
- Before implementing a fix, identify the general rule that prevents the class of failure and the dynamic inputs that must remain variable, such as services, categories, aliases, staff, dates, times, party size, customer details, business hours, POS response, and conversation state.
- If a proposed fix depends on one exact wording, one exact transcript, one exact service combination, or one exact date/time, stop and redesign it as a general rule before editing files.
- Every implementation plan must include an execution contract: exact scope, non-goals, business rule, expected files, tests to run, covered edge cases, and any remaining unknowns.
- Conversation, parser, formatter, booking, and workflow changes must include at least one regression test using different wording or different data from the original reported example to prove the implementation is not hardcoded.
- After implementation, review the full relevant user-facing flow, not only unit state. If the output would sound robotic, misleading, repetitive, or hard to answer in the real workflow, the slice is not done.
- If this contract is violated, stop immediately. Do not patch, format, test, or "fix forward" until the user chooses how to handle the current worktree. First classify the diff into approved changes, unapproved changes, and risk areas.

## No-Hardcoding Contract

This contract is mandatory across every thread for diagnosis, proposals, plans,
implementation, review, tests, documentation, skills, and subagent handoffs. It
applies even when a narrow hardcoded patch would be faster to write.

### Source Of Truth Before Solution

- Before proposing a fix, identify the actor workflow, owning module, persisted
  or provider-backed source of truth, dynamic inputs, runtime consumers, and
  existing UI/API used to manage that data.
- Inspect existing profiles, catalogs, schemas, state machines, provider data,
  contracts, and configuration before inventing a matcher, prompt table,
  decision list, or fallback.
- When an existing structured owner can represent the behavior, use or extend
  that owner. Do not create a parallel list, map, prompt builder, UI option set,
  or second taxonomy in another layer.
- If the required source of truth does not exist, propose the smallest durable
  data model or contract that should own it. Do not hide missing product data in
  code constants or prompt prose.
- A model or AI consultation profile must receive the relevant structured
  source data. Do not discard that data upstream and then recreate its meaning
  with deterministic phrases or field-specific copy downstream.

### Forbidden Product Logic

- Do not encode caller meaning, product decisions, salon policy, consultation
  behavior, service selection, staff selection, workflow transitions, or
  recommendations from exact phrases, transcript wording, screenshots, salon
  names, service names, dates, times, staff names, customer names, or the latest
  reported example.
- Do not use phrase lists, keyword maps, regular expressions, substring checks,
  allowlists, denylists, fixed menus, fixed question order, or switch branches
  as the primary semantic classifier or business decision engine.
- Do not duplicate dynamic enums, profile fields, catalog entries, aliases,
  categories, provider capabilities, or UI choices in a second hardcoded list.
  Derive them from the owning contract or expose shared metadata from that
  owner.
- Do not implement a "deterministic fallback" that guesses semantic meaning
  from keywords when an AI or provider dependency fails. Preserve validated
  state and use a generic clarification, safe retry, explicit failure, or human
  handoff according to the workflow contract.
- Do not let user-facing copy become hidden parser or business logic. Copy may
  present a state-derived question or safe fallback, but changing the wording
  must not change the underlying decision.
- Do not approve a proposal whose behavior works only for one wording, one
  transcript, one salon, one service combination, or one date/time example.

### Narrowly Allowed Constants

- Stable protocol tokens, API paths, status and field identifiers, schema
  versions, security and booking-safety invariants, and validated numeric bounds
  may be constants when their owner contract defines them as fixed.
- Bounded parsing is allowed for an explicit protocol or active state contract,
  such as selecting one of the currently offered options. It must not expand
  into general caller-intent inference.
- Presentation copy and emergency fallback copy may be fixed, but their choices
  and next action must come from structured state, catalog data, provider data,
  or a documented contract.
- Every proposed fixed value must name its authoritative owner and explain why
  it is invariant rather than salon-, user-, provider-, locale-, or
  conversation-specific data.

### Mandatory Hardcoding Audit

- Every non-trivial triage, proposal, implementation plan, review, and subagent
  handoff must include:
  - `Source of truth`: the exact data, profile, schema, provider, state, or
    contract that owns the behavior.
  - `Dynamic inputs`: the values that must remain data-driven at runtime.
  - `Fixed invariants`: any constants that remain and why they are truly fixed.
  - `Hardcoding audit`: `passed`, `failed`, or `existing risk`, with concrete
    file/function evidence.
- A plan with `Hardcoding audit: failed` is not review-ready. Stop and redesign
  it before showing code or requesting implementation approval.
- When current code is hardcoded, identify the exact duplicated owner and
  propose a migration to the authoritative data source. Do not extend or copy
  the hardcoded path just because it already exists.
- Review agents and subagents must classify hardcode risk as `none`, `existing`,
  or `introduced`. An `introduced` classification blocks approval.

### Generalization And Review Evidence

- Treat the reported wording and data as one regression fixture, never as the
  rule itself.
- Tests for conversation, parser, formatter, workflow, booking, and UI-derived
  behavior must include at least one materially different wording or data set
  and one counterexample that must not match the reported case.
- Where relevant, test dependency failure without semantic guessing, repeated
  execution/idempotency, tenant isolation, empty or disabled source data, and
  stale or conflicting state.
- Before showing a large code listing or diff for review, present the source of
  truth, data flow, general decision rule, and Hardcoding Audit. Do not spend
  review effort on code whose ownership model has not passed this gate.
- If the user rejects a hardcoded approach, withdraw it immediately. Do not
  rename, wrap, relocate, or rephrase the same duplicated logic as a new
  proposal.
- After implementation, the final response must report
  `Hardcoding audit: passed` with the data owner used, or stop and disclose the
  unresolved risk.
- Any change that creates, moves, or removes source-of-truth ownership must
  update `docs/agents/codebase-map.md` in the same approved scope.

## Agent Response Shape Contract

- For triage, root-cause, planning, review, and UI-change requests, answer in a concrete listing format instead of vague narrative.
- Start with a short conclusion that names the most likely issue or decision. If the conclusion is not fully proven, label the unproven part as inference.
- Include a **Work Type** line using one primary label: `task`, `defect`, `new feature`, `refactor`, `review`, `documentation-only`, or `agent-rule change`.
- Include **Evidence** with file paths and line numbers, command output, screenshot/log fields, or official docs. Do not cite broad folders when exact files are known.
- Include **Root Cause** for defects and performance issues. If root cause is not confirmed yet, say what evidence is missing and what must be inspected next.
- For UI changes, include **Mockup as Text** before any implementation plan. The mockup must show the relevant page/component, visible labels, states, and responsive behavior.
- Include **Proposed Fix** as a numbered list of concrete actions. Each item should describe one change and the reason it solves the diagnosed issue.
- Include **Scope** with separate `Will change` and `Will not change` bullets so backend, frontend, tests, docs, migrations, and config boundaries are explicit.
- Include **Checks** listing the exact tests, builds, typechecks, lint, screenshots, or manual verification that should run after implementation.
- End planning responses with the exact confirmation request needed to proceed, such as `Confirm this scope and I will implement it.` Do not imply implementation has started before approval.
- Keep the format compact. Prefer a useful 5-10 item listing over long prose, unless the user asks for deeper analysis.

## Product-Grade Standard

- Build and review every approved slice as commercial-grade production software, not MVP, demo, prototype, or happy-path scaffolding.
- Before proposing or implementing, check repeated-use behavior, idempotency, duplicate prevention, retry/rerun semantics, conflict handling, rollback or cleanup needs, disabled/error/empty states, tenant boundaries, and security/privacy exposure.
- For export/import, sync, webhook, booking, provider, AI training, or any workflow that can run more than once, define stable keys, upsert or dedupe behavior, and prove repeated execution will not create duplicate or rubbish records.
- If external systems will consume exported data, include contract stability, schema versioning, stable identifiers, and import semantics even when the importer is outside the current implementation slice.
- Narrow vertical slices are allowed only when the slice is internally complete and scope-honest. Do not call a slice done if predictable product-grade failure modes are unhandled, hidden, or left ambiguous.
- Do not ship fake, placeholder, or demo behavior as production behavior. Local seed/demo data is allowed only when isolated from production paths.
- Acceptance criteria and test plans must cover critical edge cases and regression risks, not just the happy path.

## AI Receptionist Conversation Quality Contract

- Treat the AI receptionist as a real phone receptionist for an operating nail salon, not a generic chatbot or a backend state machine with spoken output attached.
- The assistant owns the salon-operations reasoning burden. The user is not expected to provide nail-salon domain control language. When domain evidence is incomplete, state assumptions, reason from real salon operations, repository evidence, and provided transcripts, then propose how to validate.
- Do not fix booking conversations by patching only the latest transcript phrase. Derive the general conversation rule that prevents the class of failure across booking, reschedule, cancellation, party booking, availability, fallback, and confirmation flows.
- Before proposing or implementing any conversation change, map the real call flow: caller goal, known fields, missing fields, service/category ambiguity, staff preference, date/time constraints, scheduling-authority availability gate, authority-specific booking gate, confirmation-evidence gate, and owner handoff gate.
- Every AI reply must be concise, natural, context-aware, operationally useful, and easy for a caller to answer verbally. Do not expose internal state, parser state, stored-field reminders, or mechanical scaffolding such as repeated "noted" wording.
- Preserve known information silently. Use captured date, service, staff preference, party size, or customer details for logic without repeating them unless it helps the caller choose or confirm.
- Ask one useful question at a time. If the caller asks for a menu while a required field is missing, answer from catalog-backed services/categories, then return to the unresolved question. Do not treat a bare affirmative after an open menu as a service selection.
- Use human grouping in availability replies: same day once, same staff/team phrase once, repeated services by count. Use ordinal labels only when options differ enough that ordinals help.
- For party bookings, ask group-specific clarifications and distinguish service categories from concrete services.
- If no common time fits, prefer safe authority-backed split/staggered options before saying unavailable or handing off. `owner_manual` must keep the request pending instead of inventing availability.
- Never confirm without the selected authority's durable evidence:
  `owner_manual` never auto-confirms; `manleai_calendar` requires an atomic
  internal commit and durable appointment ID; `external_provider` requires
  provider success and the required booking ID. For split/multi-child booking,
  every child must succeed under one all-or-none operation; partial or unknown
  outcomes must rollback when supported and avoid confirmed wording.
- Conversation changes must include golden transcript tests, not only state assertions.
- Before calling a conversation slice complete, read or simulate the full transcript as a caller would hear it. If it sounds robotic, repetitive, misleading, or hard to answer by phone, the slice is not done even when backend tests pass.

## Mockup As Text

For UI-changing requests, the Mockup as Text must include:

- Page or component name.
- Target user and primary workflow.
- Layout structure using text wireframe blocks.
- Visible copy, labels, buttons, status badges, table columns, and form fields.
- Loading, empty, error, success, and disabled/gated states.
- Responsive behavior for desktop and mobile.
- API/data dependencies and any backend contract assumptions.

## Backend Rules

- Keep HTTP handlers thin. Put business rules in services and persistence in repositories.
- Booking API and conversation scheduling callers must depend on the
  authority-neutral `modules/scheduling` boundary. Only
  `modules/scheduling_external_provider` delegates to the existing booking
  service and `modules/pos.POSProvider`;
  `modules/scheduling_owner_manual` owns pending-request persistence without a
  POS dependency, and `modules/scheduling_manleai_calendar` owns internal
  availability and atomic appointment persistence. No authority-neutral or
  conversation code may import `modules/pos_square`. Do not model internal
  scheduling as a fake POS adapter.
- Keep Square-specific auth, payloads, API URLs, error mapping, and token handling inside `backend/modules/pos_square`.
- Store POS tokens encrypted only. Never expose raw or encrypted POS tokens to the frontend.
- Enforce tenant ownership by `salon_id` before returning or mutating salon-scoped data.
- Add SQL migrations under `backend/migrations` for schema changes and keep Ent schemas aligned.
- Use transactions for authority-native booking, appointment, attempt,
  notification, and audit-log writes. `manleai_calendar` must commit every
  required appointment segment atomically. `external_provider` retains the POS
  attempt/error/reconciliation transaction. `owner_manual` persists one
  deduplicated pending owner-review operation without a confirmed appointment.

## Frontend Rules

- Build a real operational dashboard, not decorative placeholder screens.
- Read and follow `DESIGN.md` before proposing, implementing, or reviewing UI/UX changes.
- Before changing UI, provide a Mockup as Text for review and wait for explicit approval.
- Every production page must handle loading, empty, error, and success states.
- Keep API calls in `frontend/lib/api` or feature-local data helpers; do not scatter raw fetch logic across components.
- Preserve the SaaS dashboard visual language already established in `frontend/components/ui` and `frontend/components/layout`.
- Use Owner-first, authority-aware wording. Square Appointments is the only
  current external-provider integration; do not present it as a prerequisite
  for implemented `owner_manual` request collection or future
  `manleai_calendar` operation, and do not claim broad external-provider
  support.

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
docker compose up -d postgres redis
```

The API and worker run the embedded startup migrator when `AUTO_MIGRATE=true`.

## Change Discipline

- Prefer narrow, vertical slices that keep backend, frontend, docs, and tests coherent.
- Respect the User Confirmation Gate before editing code.
- Do not implement fake production paths. Local seed/demo data is allowed only when isolated from production behavior.
- If a requested change crosses API contracts, update DTOs, mappers, docs, and UI states together.
- Every implementation plan must include the product-grade edge cases relevant to the slice, especially repeated execution/idempotency, duplicate prevention, conflict handling, and safe failure states.
- For high-risk booking, auth, token, tenant, or POS behavior, add tests before or alongside the change.
