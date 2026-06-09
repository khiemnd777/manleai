# Using Repo Agents And Skills

## AGENTS.md

Codex reads `AGENTS.md` at session start and layers nested files from root to the current directory. Keep repo-wide rules in the root file and only add nested `AGENTS.md` files when a subtree needs different rules.

This repo's root `AGENTS.md` includes a User Confirmation Gate:

- Feature requests, bugfixes, refactors, and code-changing tasks require an implementation plan and explicit user approval before code edits.
- UI-changing tasks require a Mockup as Text and explicit user approval before frontend edits.
- Read-only exploration and answering questions are allowed before approval.

## Skills

Repo-local skills live in `.agents/skills`. Invoke explicitly with `$skill-name` or let Codex choose them from their descriptions.

Recommended prompts:

```txt
Use $ai-receptionist-repo to implement the next backend/frontend slice.
```

```txt
Use $pos-adapter-slice to add Square token refresh without leaking Square logic into booking.
```

```txt
Use $booking-safety-tdd to implement test booking creation and cancellation.
```

```txt
Use $pilot-release-review to review this branch for Milestone 2 readiness.
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
Have pilot_release_planner split Milestone 3 into vertical slices with acceptance criteria and tests.
```

## Maintenance

- Keep skills concise. Move long reference material into `docs/` or skill `references/`.
- Update `CONTEXT.md` when domain terms become stable.
- Add ADRs when a decision should stop future agents from re-opening the same debate.
- Prefer read-only subagents for review and exploration; let the parent agent make final edits.
