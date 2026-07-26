# Agent Configuration

This repo uses these Codex configuration layers:

- `AGENTS.md` for repo-wide working instructions.
- `docs/agents/codebase-map.md` for mandatory feature/function/utility/helper,
  UI, keyword, and owner-surface routing before triage or code scanning.
- `.agents/skills/**/SKILL.md` for reusable repo-local workflows.
- `.codex/agents/*.toml` for project-scoped custom subagents.
- `.codex/hooks.json` for project-scoped lifecycle hooks, including the write approval gate.

OpenAI's Codex docs say skills are directories with a required `SKILL.md` plus optional `scripts`, `references`, `assets`, and `agents/openai.yaml`. Repo skills are discovered under `.agents/skills` from the current directory up to the repo root.

OpenAI's subagent docs say project-scoped custom agents live under `.codex/agents` and each standalone TOML file must define `name`, `description`, and `developer_instructions`.

## Mandatory Codebase Map

- Agents and subagents must read `docs/agents/codebase-map.md` before
  repository triage, diagnosis, planning, implementation, review, or handoff.
- Use the map's triage keywords to choose the first docs, files, skills, and
  subagents to inspect. Keywords accelerate search only; they are not runtime
  business logic.
- If code evidence conflicts with the map, code wins and the map must be
  corrected in the same approved scope.
- Any mapping-affecting code change must update
  `docs/agents/codebase-map.md` before the task is reported complete. If no map
  update is needed, the final response must include `Mapping impact: none` with
  the reason.
- Subagents must classify mapping impact as `map update required`, `map already
  accurate`, or `map conflict found`.

## Repo Skills

- `ai-receptionist-repo`: orient and plan work around the product/domain architecture.
- `pos-adapter-slice`: investigate, review, and plan POS provider work without leaking provider details; implicit invocation is disabled.
- `booking-safety-tdd`: investigate and plan booking behavior with test-first confirmation safety; implicit invocation is disabled.
- `salon-dashboard-ui`: investigate, review, and plan frontend dashboard flows, including AI receptionist settings and tone controls; implicit invocation is disabled.
- `voice-ai-runtime`: investigate and plan AI/voice provider abstractions, AI tone flow, conversation rules, service understanding, and realtime safety; implicit invocation is disabled.
- `business-analysis`: turn ambiguous requests into product requirements and acceptance criteria.
- `business-logic-overlap-analysis`: find overlapping rules and regression risks before changes.
- `salon-ops-workflow`: analyze real salon owner/caller/staff workflows, including correction-to-knowledge and correction-to-service-alias decisions.
- `production-release-review`: review release readiness and scope honesty.

## Write Approval Gate

- Repo `AGENTS.md` remains the source of truth: feature work, bugfixes, refactors, UI changes, code edits, tests, docs, skills, agents, migrations, runtime config, and generated artifacts require investigation, plan, and explicit user approval for the exact scope before writes.
- Questions, complaints, screenshots, appshots, status checks, "why" prompts, and partial instructions are read-only investigation requests.
- `.codex/hooks/write_approval_gate.py` is wired through `.codex/hooks.json` as a `PreToolUse` hook for `apply_patch`, `Edit`, and `Write`. It denies write tools unless the latest user prompt contains an explicit approval phrase such as `approved`, `lam di`, `làm đi`, `trien khai theo plan`, `cho phep ghi file`, or `sua cac file nay`.
- Project-local hooks may need to be reviewed/trusted by Codex after changes before they run.

## Configuration Memos

- `docs/agents/phone-booking-demo-config-memo.md`: step-by-step agent runbook for guiding live phone booking demo configuration across backend public URL, Square, Twilio, OpenAI, readiness, simulator, and live call testing.

## AI Tone Runtime Inventory

- Owner UI: `/dashboard/settings` exposes `Speaking style` and saves `ai_tone`.
- API and persistence: `GET/PUT /api/salons/:id/settings` reads and writes `salon_settings.ai_tone`.
- Config transfer: `manleai.salon_configuration.v4` includes `ai_receptionist.ai_tone` and the service category taxonomy; legacy v1-v3 imports default missing tone to `professional_warm`.
- Runtime path: `conversation.RuntimeConfig` -> `voice.ModelRequest` -> provider-specific reply adapters.
- Guardrail: tone changes spoken style only. Slot collection, handoff routing,
  service understanding, availability, and authority-native confirmation remain
  backend-owned: `owner_manual` never auto-confirms, `manleai_calendar` requires
  an atomic internal commit with a durable appointment ID, and
  `external_provider` requires provider success with the required booking
  evidence.

## Service Understanding Inventory

- Owner UI: `/dashboard/services` manages service categories, category aliases, suggestion refresh, service category assignment, and accept/clear suggestion actions.
- API and persistence: `service_categories`, `service_category_aliases`, and service category assignment fields on `services` are owned by `backend/modules/pos`.
- Runtime path: `conversation.Repository.ListBookableServices` loads category fields, `ListActiveServiceCategoryAliases` loads active category aliases, `loadAnswerContext` passes them into service understanding, and simulator/phone turns share `conversation.Message`.
- Guardrail: categories are clarification groups, not directly bookable services. Exact catalog services win; category/category-alias matches ask for a real service in that group.

## Party Booking Request Inventory

- Owner UI: `/dashboard/calls` exposes `Party booking requests` with `pending`, `contacted`, `resolved`, and `dismissed` workflow states.
- API and persistence: `party_booking_requests` are owned by `backend/modules/conversation` and keyed by salon, call session, and turn event key for owner-review exception records.
- Runtime path: supported group or party booking detection converts clear party size and guest-service counts into catalog-backed booking segments, then uses provider-neutral availability and booking tools.
- Guardrail: party operations are all-or-none under their captured authority:
  `owner_manual` creates pending owner review, `manleai_calendar` requires one
  atomic complete root/child commit, and `external_provider` requires complete
  provider success or safe rollback/reconciliation. `party_booking_requests`
  and owner workflow status changes are not confirmed appointments.

## Suggested Subagent Use

Ask Codex to spawn:

- `repo_mapper` before broad changes, unfamiliar flows, or map conflicts. The
  parent agent must still load `docs/agents/codebase-map.md` before handoff.
- `pos_backend_reviewer` for POS, auth, tenant, token, and migration changes.
- `frontend_product_reviewer` for dashboard UX and frontend contracts.
- `security_privacy_reviewer` for secrets, tenant isolation, phone/SMS/call data, and auth.
- `production_release_planner` when converting a milestone into work slices.

For service-understanding or AI Training correction work, ask `repo_mapper` to map `modules/conversation`, `modules/training`, `service_aliases`, `service_categories`, `service_category_aliases`, transcript metadata, answer-context caching, and golden tests before planning implementation.

For party booking work, ask `repo_mapper` to map group detection, party segment
planning, availability/booking tool routing, authority-native confirmation
boundaries, owner-review exception records, Calls dashboard workflow, and
redaction before planning implementation.

For AI tone work, ask `repo_mapper` to map the settings/API/config-transfer/runtime path, `frontend_product_reviewer` to review the Settings UI contract, and `pos_backend_reviewer` to verify no booking guardrail changed.

Subagents inherit the parent sandbox and approvals. Keep most review agents read-only and let the parent agent apply final edits.

## Maintenance

- Keep `docs/agents/codebase-map.md` current whenever feature behavior,
  functions, utilities, helpers, UI surfaces, API routes, tests, or ownership
  boundaries change.
- Do not finish a mapping-affecting code change until the map has been updated
  or the final response explicitly states `Mapping impact: none` with the
  reason.
- Add triage keywords when they would materially speed future root-cause search.
- Do not use triage keywords as runtime logic.
