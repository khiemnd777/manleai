# Agent Configuration

This repo uses three Codex configuration layers:

- `AGENTS.md` for repo-wide working instructions.
- `.agents/skills/**/SKILL.md` for reusable repo-local workflows.
- `.codex/agents/*.toml` for project-scoped custom subagents.

OpenAI's Codex docs say skills are directories with a required `SKILL.md` plus optional `scripts`, `references`, `assets`, and `agents/openai.yaml`. Repo skills are discovered under `.agents/skills` from the current directory up to the repo root.

OpenAI's subagent docs say project-scoped custom agents live under `.codex/agents` and each standalone TOML file must define `name`, `description`, and `developer_instructions`.

## Repo Skills

- `ai-receptionist-repo`: orient work around the product/domain architecture.
- `pos-adapter-slice`: implement POS provider work without leaking provider details.
- `booking-safety-tdd`: implement booking behavior with test-first confirmation safety.
- `salon-dashboard-ui`: build polished frontend dashboard flows, including AI receptionist settings and tone controls.
- `voice-ai-runtime`: implement AI/voice provider abstractions, AI tone flow, conversation rules, service understanding, and realtime safety.
- `business-analysis`: turn ambiguous requests into product requirements and acceptance criteria.
- `business-logic-overlap-analysis`: find overlapping rules and regression risks before changes.
- `salon-ops-workflow`: analyze real salon owner/caller/staff workflows, including correction-to-knowledge and correction-to-service-alias decisions.
- `pilot-release-review`: review release readiness and scope honesty.

## Configuration Memos

- `docs/agents/phone-booking-demo-config-memo.md`: step-by-step agent runbook for guiding live phone booking demo configuration across backend public URL, Square, Twilio, OpenAI, readiness, simulator, and live call testing.

## AI Tone Runtime Inventory

- Owner UI: `/dashboard/settings` exposes `Speaking style` and saves `ai_tone`.
- API and persistence: `GET/PUT /api/salons/:id/settings` reads and writes `salon_settings.ai_tone`.
- Config transfer: `manleai.salon_configuration.v3` includes `ai_receptionist.ai_tone`; legacy imports default to `professional_warm`.
- Runtime path: `conversation.RuntimeConfig` -> `voice.ModelRequest` -> provider-specific reply adapters.
- Guardrail: tone changes spoken style only. Slot collection, handoff routing, service understanding, availability, and POS-first confirmation remain backend-owned.

## Suggested Subagent Use

Ask Codex to spawn:

- `repo_mapper` before broad changes or unfamiliar flows.
- `pos_backend_reviewer` for POS, auth, tenant, token, and migration changes.
- `frontend_product_reviewer` for dashboard UX and frontend contracts.
- `security_privacy_reviewer` for secrets, tenant isolation, phone/SMS/call data, and auth.
- `pilot_release_planner` when converting a milestone into work slices.

For service-understanding or AI Training correction work, ask `repo_mapper` to map `modules/conversation`, `modules/training`, `service_aliases`, transcript metadata, and golden tests before implementation.

For AI tone work, ask `repo_mapper` to map the settings/API/config-transfer/runtime path, `frontend_product_reviewer` to review the Settings UI contract, and `pos_backend_reviewer` to verify no booking guardrail changed.

Subagents inherit the parent sandbox and approvals. Keep most review agents read-only and let the parent agent apply final edits.
