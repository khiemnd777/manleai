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
- `salon-dashboard-ui`: build polished frontend dashboard flows.
- `voice-ai-runtime`: implement AI/voice provider abstractions and conversation rules.
- `pilot-release-review`: review release readiness and scope honesty.

## Configuration Memos

- `docs/agents/phone-booking-demo-config-memo.md`: step-by-step agent runbook for guiding live phone booking demo configuration across backend public URL, Square, Twilio, OpenAI, readiness, simulator, and live call testing.

## Suggested Subagent Use

Ask Codex to spawn:

- `repo_mapper` before broad changes or unfamiliar flows.
- `pos_backend_reviewer` for POS, auth, tenant, token, and migration changes.
- `frontend_product_reviewer` for dashboard UX and frontend contracts.
- `security_privacy_reviewer` for secrets, tenant isolation, phone/SMS/call data, and auth.
- `pilot_release_planner` when converting a milestone into work slices.

Subagents inherit the parent sandbox and approvals. Keep most review agents read-only and let the parent agent apply final edits.
