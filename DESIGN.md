# DESIGN.md

## Purpose

This file is the design, UI, and UX contract for ManleAI agents. Use it before proposing, implementing, or reviewing any user-facing frontend work.

The product is an operational SaaS dashboard for nail salon owners, not a
marketing site. The UI should help an owner configure the salon, understand the
selected scheduling authority and its readiness, optionally connect Square
Appointments for `external_provider`, monitor phone receptionist readiness,
and understand failures quickly.

The public customer-facing catalog in `landing/` is a separate surface. It can
use a simpler customer-facing layout, but it must stay public-safe: call-to-request
only, no confirmed web booking claims, no staff contact details, no POS IDs, no
sync errors, no owner identifiers, and no provider tokens.

## Product Experience

- Keep the interface quiet, dense, professional, and fast to scan.
- Prioritize operational clarity over decorative visuals.
- Show real workflow status, next actions, and blockers. Do not fill production pages with fake activity.
- Keep product claims Owner-first and scope-honest. The current UI includes the
  Phase 2 `owner_manual` review queue and the existing Square-backed
  `external_provider` workflows. Phase 4B includes owner-managed
  `manleai_calendar` configuration/readiness plus structured multi-guest,
  multi-service staff-only and pooled internal availability/create and
  whole-root lifecycle actions. V52-V55 implement explicit owner-reviewed
  authority preview and commit, immutable audit history, and inverse-run
  context in Settings. Provider integrations never switch authority implicitly.
- Derive confirmation copy from scheduling-authority evidence:
  `owner_manual` remains pending, `manleai_calendar` requires an atomic internal
  commit and durable appointment ID, and `external_provider` requires provider
  success with the required booking ID and metadata.
- When a workflow is not available yet, show a disabled or gated state with the real dependency, not a pretend success path.

## Platform Access Experience

Access controls follow the object they govern. Global Platform roles live at
`/platform/access`; salon membership, salon-specific Platform support
capabilities, and temporary sensitive-data grants live in the selected salon's
`Access` tab. The salon detail route implies the salon, so the page must not ask
the administrator to select it again.

One login identity belongs to exactly one immutable authorization realm:
`tenant` or `platform`. A human who works in both realms uses two distinct
login identities. Role labels, salon membership, and frontend filtering must
never be used to convert or infer that realm.

- **Platform roles** embeds a Platform-only identity lookup inside the
  assign-role workflow. Tenant identities never appear and cannot be targeted
  by direct mutation. It is not a standalone user directory and must explain
  that Platform Ops receives salon capabilities separately.
- **Salon team** uses the selected salon's Tenant-only identity lookup, manages
  Tenant Business Managers beside that salon, and keeps the owner membership
  visibly protected. Platform identities never appear.
- **Platform support access** selects an active Platform Ops account and renders
  salon-delegable capabilities, display names, and dependencies from the
  backend capability catalog. The frontend must not maintain a parallel list or
  infer read/write dependencies.
- **Owner-authorized support** is a second, separate control for Services, AI
  Training, and Calls. Platform Admin requests an exact Platform identity,
  capability set, opaque support reference, and expiry; the salon Owner reviews,
  approves/rejects, and may revoke it from the existing Settings workflow. Base
  Platform role/assignment access remains required. The UI must never imply that
  Platform Admin bypasses Owner approval. Calls approval includes its linked
  Calls PII scope and limits the whole request to 24 hours; non-PII support is
  limited to 30 days.
- **Temporary sensitive data access** explains that PII means customer, call,
  appointment, or notification records. A Platform role or salon capability
  alone does not reveal PII. The control requires an exact scope, opaque
  approved change reference, and duration of at most 24 hours; it shows expiry,
  revocation, and audit behavior before the administrator acts.
- The general temporary-sensitive-data control does not grant Calls. Calls PII
  is created only by Owner approval of the linked support request.
- Platform role and salon Access navigation is visible only to current Platform
  Administrators. Direct routes remain backend-authorized and fail closed.
- Existing access rows use the API's nested user summary and never display a raw
  user UUID because a search result page did not contain that account.

Loading uses content-shaped skeletons. Load and mutation failures retain a
clear retry path; empty sections explain what has not been assigned; mutations
disable conflicting controls; exact replay is reported as recovered rather
than duplicated. Desktop uses compact grids and rows. Mobile stacks fields and
uses full-width primary actions without horizontal page scrolling.

Mockup as Text:

```text
Platform roles                                      [Refresh]
Assign a Platform role
  Find a Platform identity: [Name or email] [Search]
  [Platform identity only] [Platform Ops | Platform Admin] [Assign role]
Current Platform roles
  Account · role · status · version                  [Revoke]

Nail salon detail
Business | Services | AI Training | Calls | Technical settings | Operations | Access | Audit

Access                                              [Refresh]
Salon team
  [Find Tenant identity for this salon]             [Add manager]
  Owner · protected
  Business Manager · status                         [Revoke]

Platform support access
  [Active Platform Ops]
  [Backend capability checkboxes and dependencies]  [Assign access]

Request Owner authorization
  [Platform account] [Services / AI Training / Calls capabilities]
  [Support reference] [Expiry] [Calls PII when required] [Request approval]
  Pending Owner review · exact capabilities · expiry    [Cancel request]

Temporary sensitive data access
  [Platform account] [Data scope] [Change reference] [1–24 hours]
                                                     [Grant temporarily]
  Account · exact scope · expiry · reference         [Revoke now]
```

## Scheduling Authority Experience

Authority-aware scheduling surfaces must show the selected authority,
readiness, blockers, and the exact owner action that resolves a blocker when
those controls exist.
Connecting, syncing, or testing Square must never look like an implicit
authority switch.

- `owner_manual`: focus on pending request review and notification state. Do
  not show provider setup as a booking prerequisite or promise automatic
  availability.
- `manleai_calendar`: focus on internal hours, staff schedule, conflicts, and
  capacity policy, resources, exceptions, and operation-specific capabilities.
  Never present activation as authority switching. Enable staff-only, pooled,
  party, and new-work lifecycle controls only from their exact backend
  capabilities. A historical internal row remains routed by its persisted
  origin/version after a later selected-authority change.
- `external_provider`: focus on provider connection, selected location, sync,
  mappings, write permission, and reconciliation without weakening provider
  confirmation safety.

Appointment and request views must retain the originating authority so owners
can understand why historical reschedule, cancellation, retry, or
reconciliation actions follow a different path after an explicit switch.

## Calls Readiness

The Calls page presents three equal readiness cards in this order: **Phone
answering**, **Request capture**, and **Automated booking**. They stack on
mobile and use backend-returned readiness and blockers; the frontend must not
infer scheduling capability from Square connection state, provider names, or
copy.

- Phone answering shows telephony configuration, salon phone, signature/input
  mode, webhook endpoints, and voice-AI adapter diagnostics.
- Request capture shows the selected scheduling authority, exact authority
  version, booking mode, and the blockers that prevent collecting a structured
  scheduling request or verified availability.
- Automated booking shows whether the selected authority can return its
  required durable confirmation evidence. `owner_manual` remains visibly
  owner-review only; `manleai_calendar` reflects atomic internal execution;
  provider setup appears here only for `external_provider`.

Each card has loading, ready, and blocked states. Blocked states show the safe
backend message and stable blocker code. A page-level load failure retains a
retry action and never guesses readiness. Legacy `phone_booking_ready` may be
identified as a compatibility alias, but the visible label remains Automated
booking.

The Tenant Calls view may show Owner-operational readiness fields already
available to that workflow. The Owner-authorized Platform Calls view reuses the
same component but receives a business-safe projection: provider URLs, model,
voice, credentials, and diagnostics are represented only as managed in
Technical settings.

## ManleAI Calendar Configuration

The implemented Phase 3 workflow manages structured configuration beside the
operational object it belongs to. It must not create a separate catch-all page
that makes owners reselect the salon, staff member, or service already in
context.

- **Settings** owns the salon-wide setup through the ManleAI Calendar section:
  readiness summary, slot step, minimum notice, booking horizon, reschedule and
  cancellation cutoffs, party limit, default buffers, `local_override` weekly
  salon hours, shared resource pools, salon/resource exceptions, and the
  version-fenced **Activate configuration** audit action. A changed
  configuration is visibly stale and can be activated again after blockers are
  resolved.
- **Staff** owns one staff member's weekly periods, service assignments, and
  staff-scoped exceptions inside that staff member's existing detail/edit
  workflow. The parent staff identity is implied and must not be selected again.
- **Services** owns one service's internal-calendar enablement, `staff_only` or
  `pooled` capacity mode, buffer overrides, eligible staff, and resource
  requirements inside that service's existing detail/edit workflow. The parent
  service identity is implied.
- **Appointments** shows scheduling-authority readiness, links to Settings,
  Staff, and Services setup, and owns the Phase 4B structured internal
  appointment-create flow plus Phase 4C whole-root reschedule/cancel. It does
  not expose an authority switch.

Every surface uses backend-returned constraints and readiness blockers rather
than duplicating ranges, allowed modes, or eligibility logic. Mutations preserve
one action key across safe retry, send the latest `expected_config_version`, and
replace it only when the logical form changes. A version conflict reloads the
latest aggregate and asks the owner to review before saving again.

The readiness UI must show the aggregate and operation dimensions:

- **Configuration** is Ready or Needs attention from
  `configuration_ready` and configuration-dimension blockers.
- **Scheduling execution** comes from aggregate `execution_ready`. It is Ready
  only when all six declared operation capabilities are true; otherwise the UI
  must still expose each true operation capability independently.
- **Available now** comes from backend `readiness.capabilities`. Staff-only
  availability/create, pooled capacity, and party create may each be Ready
  when their backend predicates pass. Reschedule and cancel become Ready for
  new work only under the selected-authority/current-activation engine fence;
  row-origin lifecycle controls remain target-driven rather than current-mode
  driven.

Loading uses content-shaped skeletons. Aggregate/detail failures expose retry;
empty policy, hours, schedules, resource, and exception states identify the
next setup action; saves disable conflicting controls; success uses the returned
aggregate and `replayed` flag. Desktop uses compact grids and rows, while mobile
stacks fields and uses full-width save/archive/cancel actions without horizontal
scrolling. Activation copy must say that it records readiness for the current
config version, can become stale after a later change, and does not switch
scheduling authority. It may enable a capability only after the backend returns
that capability as ready.

## Phase 4B Internal Appointment Create

The implemented child workflow lives on `/dashboard/appointments`. It uses the
selected `manleai_calendar` authority, canonical service/staff configuration,
and neutral scheduling APIs. It never displays provider evidence for an
internal appointment.

Mockup as Text:

```text
Appointments                                      [Create appointment]
Scheduling readiness: ManleAI Calendar
Available now: Staff-only availability · Staff-only create · Pooled capacity · Party create · Reschedule · Cancel

Create appointment
1 Guests & services -> 2 Date -> 3 Verified openings -> 4 Customer -> 5 Review

Guest 1 label: [Guest 1]
  Service 1: [Enabled service] [Specific staff | Anyone] [Staff]
  [+ Add service]
[+ Add guest] (up to backend max_party_size)
Date: [YYYY-MM-DD]
Verified opening:
  Guest 1 · Service 1 · time · concrete staff · occupied range
  Pooled segment · resource pool · units (when required)
Customer: [Name] [Phone] [Email optional]
Review: party size · ordered guest/service/staff/resource plan
                                      [Back] [Confirm appointment]

Submitting: Creating appointment…
Unknown response: Result unknown. [Retry exact operation]
Conflict: Opening changed. [Check availability again]
Success: Confirmed atomically · Root internal appointment ID {durable ID}
```

- The primary user is the owner building one aggregate internal appointment
  from structured guests and ordered service units. The parent scheduling
  authority and salon are implied; the owner does not reselect either.
- Service choices come from enabled policies whose exact create capability is
  ready. Adding a guest or another service row requires `party_create`; pooled
  policies require `pooled_capacity`. The maximum guest count comes from the
  backend aggregate constraint.
- Every guest label is preserved as `guest_reference`; every normalized service
  unit has quantity one. `Anyone` is a search preference, while every offered
  segment and final review shows its concrete assigned staff member. Services
  for the same guest remain ordered/sequential in the verified plan.
- Verified opening selection carries one backend quote ID, slot fingerprint,
  and complete ordered segment/resource graph. Confirmation is shown only
  after the action response returns a non-empty durable root appointment ID and
  booking-attempt ID and its child graph exactly matches the quote.
- A typed quote/conflict `409` clears stale proof and returns the owner to
  availability. A network/response-loss state retains the same logical payload
  and operation key and offers **Retry exact operation** so committed replay can
  return the same IDs.
- Capability, loading, empty, validation, error, submitting, uncertain, replay,
  conflict, and success states are explicit. An incomplete child result,
  resource conflict, or guest/party mismatch is a non-confirmation state for
  the complete operation.
- Desktop uses the compact step grid and side-by-side fields. Mobile stacks the
  steps, fields, slot buttons, and full-width Back/Confirm/Retry actions without
  horizontal scrolling.
- Data dependencies are the ManleAI Calendar aggregate/capabilities plus
  `POST /api/salons/:id/scheduling-availability` and
  `POST /api/salons/:id/scheduling-actions`.

## Phase 4C Internal Appointment Lifecycle

The implemented lifecycle workflow remains inside
`/dashboard/appointments`, next to the internal appointment row it changes.
It does not belong in Settings or Integrations because the operational parent
is the persisted appointment root. The target salon and appointment are
already implied; no redundant selector is shown.

Mockup as Text:

```text
Appointment row · ManleAI Calendar · Confirmed · Version 2
                                      [View] [Reschedule] [Cancel]

Reschedule internal appointment
Current whole root: party size · ordered guests/services · staff/resources · version 2
Cutoff: Open until {verified cutoff timestamp}
1 Preferred date: [YYYY-MM-DD] [Check exact availability]
2 Complete replacement plans
  Option: ordered guests/services · concrete staff · time · resources
3 Current -> New
                                      [Close] [Confirm whole-party reschedule]

Unknown response: Outcome unknown; fields and proof are locked.
                                      [Retry exact reschedule]
Stale target/quote: Reloaded; no appointment change was inferred.
Success: Same durable root · Rescheduled · Version 3 · exact active children

Cancel internal appointment
Current whole root: party size · ordered guests/services · version 3
Cutoff: Open until {verified cutoff timestamp}
Cancellation reason (optional): [................................]
This releases every active child together and retains lifecycle history.
                                      [Close] [Cancel whole appointment]

Unknown response: Outcome unknown; reason, version, and key are locked.
                                      [Retry exact cancellation]
Success: Same durable root · Cancelled · Version 4 · zero active children
```

- A row uses the internal lifecycle dialog only when its persisted
  `scheduling_authority` is `manleai_calendar`, status is `confirmed` or
  `rescheduled`, the root authority ID/version is complete, and every current
  child belongs to that exact version. The salon's current authority does not
  reroute that historical target.
- Reschedule availability carries `target_appointment_id`, exact target
  version, party size, and the immutable ordered service/guest shape. The owner
  selects one complete backend-assigned replacement plan; the browser never
  invents staff, times, or resource allocations.
- Cancel submits the exact target ID/version and optional reason without a
  quote, replacement segments, or time range. Copy must say that the whole root
  is cancelled and history is retained; it must not imply deletion.
- Cutoff presentation is fail closed. Missing (`null`/`undefined`), invalid,
  unparsable, closed, or equality-at-cutoff evidence disables a new action and
  explains why. Only `now < start - cutoff` is visibly open. An already unknown
  submission may retain its locked exact-retry action so response-loss recovery
  does not create a new operation.
- A typed target/version/quote conflict clears local proof, reloads the
  appointment, and shows no-success copy. An untyped response loss locks the
  exact logical payload and operation key until committed replay proves the
  outcome.
- Reschedule success requires the same root ID, non-empty internal attempt ID,
  target version, exactly next result version, `appointment_status=rescheduled`,
  active-child count, and exact replacement child graph. Cancel success
  requires the same root/attempt/version evidence,
  `appointment_status=cancelled`, and `active_child_count=0`. Provider-shaped
  evidence is invalid for both.
- Desktop uses side-by-side current/new comparison; mobile stacks the current
  plan, options, reason, warning, and full-width actions without horizontal
  scrolling.

## Owner Review Requests

The implemented `owner_manual` management surface is a child workflow on
`/dashboard/appointments`, because each row is scheduling work requiring an
owner decision. It is not provider setup, free-text training, or confirmed
appointment history.

- The card title is **Owner review requests** and must state that requests are
  recorded for review, not confirmed, and that the screen does not prove an
  owner notification was sent.
- Filters are **All**, **Pending**, **Contacted**, **Resolved**, and
  **Dismissed**, with bounded counts and a manual **Refresh** action.
- Desktop uses a table with Requested time, Customer, Type, Services, Status,
  and Action columns. Mobile replaces the table with stacked customer/status,
  requested-time, type, service, and full-width **Review request** cards.
- Loading uses row-shaped skeletons. Empty state names the selected status.
  List/detail failures show an actionable retry. Pagination disables while a
  page or transition is active.
- **Review owner request** shows customer contacts, requested time, party size,
  source, ordered service/staff/guest snapshots, optional target, notes,
  resolution reason, and lifecycle timestamps.
- A pending request may be marked **Contacted**, **Resolved**, or
  **Dismissed**. Contacted may then move only to Resolved or Dismissed.
  Resolve/Dismiss require a resolution reason; the review note is optional.
  Terminal requests are read-only.
- Version-conflict state reloads the latest request and asks the owner to review
  it again. Success and terminal copy must repeat that no appointment was
  confirmed, rescheduled, or cancelled by the review status update.

This surface consumes the scheduling-request list/detail/transition APIs
through `frontend/lib/api/scheduling-requests.ts`. It does not manage authority
selection, send notifications, create appointments, call POS, or resolve
external-provider reconciliation.

## Owner Notification Delivery

Owner-operational SMS is a separate sibling card on
`/dashboard/appointments`, immediately after **Owner review requests**. The
operational object is the durable notification row, not the request status or
provider setup, so delivery inspection belongs beside owner work while sender,
consent, and callback configuration remain in Integrations.

- The card title is **Owner notification delivery** and explicitly says that
  provider acceptance is not proof of delivery and SMS status never confirms
  or changes an appointment.
- Summary metrics show queued, in progress/provider accepted, delivered, dead
  letter, and disabled states from backend counts. The UI does not infer status
  from timestamps or provider names.
- Desktop rows show created time, notification type, masked destination,
  delivery status, safe attempt count, and **View delivery**. Mobile uses
  stacked cards with the same evidence and full-width action. Bounded
  Previous/Next pagination keeps older operational evidence reachable.
- Detail shows only masked destination, safe provider/status/error fields,
  timestamps, and immutable safe events. It never shows message body, full
  phone number, provider message ID, raw response, internal error, or secret.
- **Retry delivery** appears only when backend `can_requeue=true`. It preserves
  one action key for uncertain client response and is never offered for
  `DELIVERY_OUTCOME_UNKNOWN`, because duplicate delivery is possible.
- Loading uses row-shaped skeletons; empty state says no delivery records;
  errors retain refresh/retry; success refreshes metrics/detail without
  implying the related scheduling request was resolved.

The Twilio card in `/dashboard/integrations` owns owner-SMS setup: explicit
enablement, E.164 owner destination, fresh consent attestation, write-only
Account SID/Auth Token, write-only Messaging Service SID or sender, callback
paths, and read-only computed HTTPS callback URLs. Changing the destination
requires a new attestation. The screen must label this **owner operational
SMS** and state that customer SMS/consent is not enabled. Blank write-only
values preserve existing secrets unless the owner explicitly selects the
matching clear action.

## Square Webhook Operations

The implemented Square webhook operations surface is a child section inside
the existing **Square Appointments** card on `/dashboard/integrations`. It is
shown only when Square is the active external adapter and a recoverable Square
connection exists. It is not a standalone page, Appointments workflow, or
scheduling-authority selector: the parent object is the connected Square
integration, and historical external-provider repair remains provider/origin
scoped after an authority switch.

Mockup as Text:

```text
Square Appointments                                      [connection status]
Connect OAuth · Location · Sync

Webhook operations                                      [Configured]
Monitor booking-event processing; delivery is not appointment confirmation.
[Pending] [Processing] [Failed] [Dead letter] [Succeeded · 168h]
Calendar repair backstop                                 [Healthy | Degraded]

Processing status [All statuses v]                       [Refresh operations]
Received             Event                Status          Attempts       Action
Jul 24, 9:50 AM      booking · updated    dead letter     10 · 0         [View event]
                                                        [Previous] [Next]

Square webhook event
Event type · status · processing attempts · owner requeues
Safe diagnostic · next attempt
Available timeline evidence
                                      [Requeue failed event]

Unknown response: Result unknown.                        [Retry exact requeue]
Replay success: Exact saved action recovered; no second requeue was created.
```

- The filters are the public contract values `pending`, `processing`, `failed`,
  `dead_letter`, and `succeeded`. An unfiltered response may include backend
  `ignored` records; they are visible read-only and never offered for requeue.
- Metrics and calendar-repair health come directly from the authenticated
  owner-scoped API. The UI does not infer health from local timestamps or
  Square configuration badges.
- Detail and timeline show only event type, bounded status/attempt counts, safe
  error class/code, and returned timestamps. Provider identifiers, raw payload,
  signature/token material, provider responses, customer data, and raw errors
  are never rendered.
- A new requeue button appears only when `can_requeue=true`. One action key is
  retained for that intent; a lost response keeps the same key and presents
  **Retry exact requeue**. `X-Idempotent-Replay=true` produces recovered-action
  copy without implying another event was queued.
- Loading uses row-shaped skeletons. Configuration-missing, empty, list/detail
  error, success, disabled, pagination, submitting, and uncertain-response
  states are explicit. Desktop uses a compact table; mobile uses stacked event
  cards and full-width actions without horizontal scrolling.
- A configured verifier means only that the HTTPS notification URL and
  write-only signature key exist. It does not prove a Square subscription,
  delivery, repair, booking, or appointment confirmation.

## Visual System

Use the established frontend tokens from `frontend/tailwind.config.ts`:

- `ink` `#111827` for primary text.
- `muted` `#6b7280` for secondary text.
- `line` `#d9dee8` for borders and separators.
- `panel` `#ffffff` for cards and panels.
- `shell` `#f5f7fb` for the app background.
- `brand` `#0f766e` for primary actions, active navigation, and key icons.
- `accent` `#b91c1c` for destructive actions.
- `shadow-soft` for elevated panels only.

Use Inter/system sans typography. Keep page headings compact: `text-2xl` is enough for dashboard pages; use `text-base`/`text-sm` inside cards and controls. Do not use viewport-scaled text or negative letter spacing.

Use `rounded-md` for buttons, nav items, alerts, inputs, and small controls. Use `rounded-lg` only for cards/panels. Do not create highly rounded pill-heavy layouts unless the existing component already does so, such as status badges.

## Layout System

Dashboard pages live inside `frontend/components/layout/app-shell.tsx`:

- Desktop uses the fixed left navigation and a top status/action bar.
- Main content is constrained with `max-w-7xl`, `px-5`, and `py-6`.
- Page content should use `space-y-6` and grid gaps of `gap-4` or `gap-6`.
- Prefer responsive grids like `md:grid-cols-2`, `xl:grid-cols-4`, or explicit dashboard ratios.
- Avoid nested cards. Use cards for individual repeated items, focused forms, tables, and operational panels.
- Do not build landing-page hero sections inside the dashboard shell.

For mobile:

- Collapse data into a single readable column.
- Keep primary actions reachable without horizontal scrolling.
- Tables must degrade to cards, compact rows, or horizontally scrollable regions with visible context.
- Text must fit inside buttons, badges, cards, and table cells without overlap.

## Components

Reuse primitives from `frontend/components/ui` before adding new styling:

- `Button` for actions. Use `primary` for the main next step, `secondary` for safe alternatives, `ghost` for low-emphasis shell actions, and `danger` for destructive actions.
- `Card`, `CardTitle`, and `CardDescription` for focused dashboard panels.
- `Badge` for statuses. Add status classes centrally when a new status is introduced.
- `Alert` for blocking errors or success confirmations.
- `Skeleton` for loading states.

Use lucide-react icons for navigation, buttons, and status affordances when an icon adds scan value. Icons should be `h-4 w-4` in buttons/nav and `h-5 w-5` in panel headers unless a local pattern requires otherwise.

If a new reusable primitive is needed, put it in `frontend/components/ui` and match the existing token, radius, typography, disabled, and focus-visible behavior.

### Preserve Mature Operational Workflows

Route or authorization restructuring must preserve a mature workflow component
when its operational object and interaction model have not changed. Do not
replace a full page with a newly invented reduced dashboard merely because the
route, actor, or API prefix changed.

- `ServicesDashboard` remains the parent UI for service categories, category
  aliases, service aliases, service catalog controls, consultation, and
  service-grain calendar policy.
- `CallsDashboard` remains the parent UI for sessions, transcripts, detected
  details, corrections, party review, simulator, archive, and redaction.
- `SettingsDashboard` remains the parent UI for Owner Business settings and
  Owner support-authorization decisions.
- `TrainingDashboard` remains the parent UI for knowledge, corrections, and
  evaluation.

The Platform Business workspace must not render a second reduced Services
editor. The Platform tenant detail's Services tab leads to the shared full
`ServicesDashboard`, which remains the only Platform parent workflow for
services, categories, and their aliases.

Tenant/Platform differences belong in explicit surface-aware data adapters,
safe DTO projections, and capability-gated actions inside those components.
Provider configuration remains under Platform Technical. A separate component
is justified only when the actor workflow or operational parent is genuinely
different, and that decision must be documented before implementation.

Service aliases are structured children of a service and category aliases are
structured children of a service category. Manage both inside the Services
parent workflow. AI Training may offer an explicit correction-to-service-alias
action, but it must persist the canonical structured alias and must not convert
either alias type into free-text knowledge.

## Standard Page Anatomy

Every production dashboard page should have:

1. Header area with page title, short operational description, and primary action/status when relevant.
2. Loading state using skeletons shaped like the final layout.
3. Error state using `Alert` with a clear retry or next action when available.
4. Empty state that explains what is missing and provides the next setup step.
5. Success/data state with real data, timestamps, statuses, and owner actions.
6. Disabled/gated state for workflows blocked by onboarding, selected-authority
   readiness, external-provider setup when applicable, booking safety checks,
   switch conflict, or milestone scope.

Do not ship a page that only says a feature will come later unless it is explicitly a gated placeholder for a milestone, and even then it must tell the owner what dependency unlocks it.

## Copy And Terminology

Use concise operational copy:

- Prefer "Review pending request", "Set up ManleAI calendar", "Connect Square",
  "Sync services", and "Open fallback requests" only when those actions match
  the selected authority.
- Say "Square Appointments" for the current POS integration.
- Say "Owner-first scheduling, with optional Square Appointments integration"
  for broader positioning.
- Say "external-provider adapter architecture" when describing future provider
  support.
- Use "AI phone receptionist" or "AI receptionist" consistently.
- For booking failures, distinguish "pending request" from "confirmed appointment".

Avoid:

- Claims of broad POS support before providers exist.
- Claims that an authority mode or AI booking is live before its backend gates
  pass.
- Decorative feature explanations inside the app UI.
- Placeholder operational metrics that look real.

## Forms And Tables

Forms:

- Group related fields with clear labels and helper text only when it reduces errors.
- Show validation errors near the field and a page-level error when submission fails.
- Disable submit while saving and keep the label action-specific, such as "Save salon profile" or "Connect Square".
- Preserve owner-entered data after failed submissions.

Tables and lists:

- Use columns that support owner decisions: name, status, source, last synced, owner action, and failure reason where relevant.
- Empty rows should become an empty state, not blank table chrome.
- Error/failure rows need visible status badges and next actions.
- Keep raw IDs out of the primary UI unless needed for support/debug workflows.

## State And Data Contract

Frontend state must reflect backend truth:

- Read API calls from `frontend/lib/api` or feature-local data helpers.
- Keep frontend types aligned with backend DTOs. Do not overload
  `active_pos_provider` as a scheduling-authority field.
- Do not expose raw or encrypted POS tokens.
- Tenant-scoped UI must assume backend ownership checks by `salon_id` and must not cache cross-salon data casually.
- When API support is missing, gate the UI instead of fabricating production behavior.

## Agent Workflow

Before frontend/UI edits:

1. Read this file, `AGENTS.md`, and `.agents/skills/salon-dashboard-ui/SKILL.md`.
2. Inspect the existing page, nearest components, API helper, and backend/API docs that own the workflow.
3. Provide a Mockup as Text and wait for explicit user approval before editing UI code.
4. Implement the smallest coherent slice using existing components and tokens.
5. Include loading, empty, error, success, and disabled/gated states.
6. Run `cd frontend && npm run typecheck && npm run build`.
7. For visible layout changes, run the local app and inspect the route on desktop and mobile.

## Review Checklist

Use this checklist before finishing UI work:

- The page looks like an operational dashboard, not a landing page.
- The layout matches the existing app shell, spacing, cards, buttons, badges, and typography.
- The copy is scope-honest about the selected scheduling authority, current
  operation capabilities, Square-backed external path, and AI booking
  readiness.
- Every production page state is represented.
- Buttons and links have clear outcomes and disabled states where needed.
- Mobile layout has no overlap or horizontal overflow except intentional table scrolling.
- API data, DTOs, and frontend types agree.
- No fake confirmation path exists: pending owner review, failed internal
  commits, and incomplete/failed provider writes remain unconfirmed.
- AI Settings presents `booking_mode` as caller-facing scheduling behavior,
  separate from the selected scheduling authority. Pending approval copy says
  that the selected time is not reserved; disabled copy says availability and
  request actions do not run; owner-manual automatic confirmation is disabled.
- Owner review rows and details show the request's target scheduling authority
  separately from the `owner_manual` request origin and repeat that review
  status changes do not reserve, confirm, reschedule, or cancel an appointment.
