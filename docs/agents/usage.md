# Using Repo Agents And Skills

## AGENTS.md

Codex reads `AGENTS.md` at session start and layers nested files from root to the current directory. Keep repo-wide rules in the root file and only add nested `AGENTS.md` files when a subtree needs different rules.

This repo's root `AGENTS.md` includes a User Confirmation Gate:

- Feature requests, bugfixes, refactors, and code-changing tasks require an implementation plan and explicit user approval before code edits.
- UI-changing tasks require a Mockup as Text and explicit user approval before frontend edits.
- Read-only exploration and answering questions are allowed before approval.
- Questions, complaints, screenshots, appshots, status checks, and partial instructions are not approval to write files.
- Project `.codex/hooks.json` adds a `PreToolUse` write approval gate for `apply_patch`, `Edit`, and `Write`; review/trust the hook in Codex when prompted after changes.

## Skills

Repo-local skills live in `.agents/skills`. Analysis and review skills may be selected implicitly. Skills that can lead to edits disable implicit invocation in `agents/openai.yaml`; invoke them explicitly and still require plan-first approval before writes.

Recommended prompts:

```txt
Use $ai-receptionist-repo to investigate the next backend/frontend slice, identify owning files, and propose a plan before edits.
```

```txt
Use $pos-adapter-slice to investigate Square token refresh ownership and propose an approval-ready plan without leaking Square logic into booking.
```

```txt
Use $booking-safety-tdd to plan the test-first booking creation and cancellation slice, including POS confirmation and duplicate-prevention checks.
```

```txt
Use $production-release-review to review this branch for production readiness.
```

```txt
Use $business-analysis to turn this workflow idea into requirements, acceptance criteria, and edge cases.
```

```txt
Use $business-logic-overlap-analysis to check whether this change collides with booking, POS sync, voice, or dashboard gates.
```

```txt
Use $salon-ops-workflow to analyze the owner/caller workflow before changing dashboard, service category, party request, or AI behavior.
```

```txt
Use $voice-ai-runtime to investigate AI tone, service understanding, salon-scoped service aliases, service categories, category aliases, supported party booking, owner-review party exceptions, name-slot repair, and conversation golden regressions, then propose an approval-ready plan.
```

## Subagents

Project-scoped custom agents live in `.codex/agents`. Ask Codex to spawn them explicitly.

Recommended prompts:

```txt
Have repo_mapper map the booking path, then have pos_backend_reviewer review the planned files before implementation.
```

```txt
Review this dashboard change with frontend_product_reviewer and security_privacy_reviewer, wait for both, then summarize findings.
```

```txt
Have production_release_planner split the next approval-loop slice into vertical backend, frontend, docs, and test work.
```

```txt
Have repo_mapper map service_aliases, service_categories, service_category_aliases, owner corrections, and party_booking_requests, then have frontend_product_reviewer check the Services, Calls, and AI Training UI contracts.
```

```txt
Have repo_mapper map ai_tone from Settings/API/config transfer through conversation and voice adapters, then have frontend_product_reviewer review the speaking-style UI contract.
```

## Maintenance

- Keep skills concise. Move long reference material into `docs/` or skill `references/`.
- Update `CONTEXT.md` when domain terms become stable.
- Update this inventory when new repo-local skill triggers, project agents, or domain source-of-truth tables/settings such as `service_aliases`, `service_categories`, `service_category_aliases`, `party_booking_requests`, or `salon_settings.ai_tone` are added.
- Add ADRs when a decision should stop future agents from re-opening the same debate.
- Prefer read-only subagents for review and exploration; let the parent agent make final edits.
