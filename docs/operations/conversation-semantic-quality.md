# Conversation Semantic Quality Evidence

This document owns the P3 evidence boundary for Conversation AI semantic,
routing, and customer-facing copy quality. It does not redefine operational
data ownership, scheduling authority, or confirmation safety.

## Evidence levels

| Evidence | What it proves | What it does not prove |
| --- | --- | --- |
| Unit and deterministic Conversation Service tests | A supplied structured interpretation follows the expected production route, source, state transition, copy guardrail, and side-effect boundary | The configured model will produce that interpretation |
| Direct-model single-turn pilot | The stored database-configured model satisfies the single-turn semantic contract and the guarded production reply path for selected isolated fixtures | Multi-turn production quality or tenant-runtime catalog behavior |
| Real-salon deterministic runtime | Independently authored multi-turn scripts execute through the production Conversation Service with retained route, state, answer-source, tool, handoff, and confirmation evidence | Model quality; semantics are authored fixtures |
| Real-salon live canary | The stored database-configured model and production Conversation Service pass ten bounded multi-turn journeys plus transcript review | A production SLO or unrestricted rollout |

Corpus generation, repository tests, structural review, and deterministic
runtime never call a model and must never be reported as a model pass.

## Runtime owner matrix

| Concern | Source of truth / owner | Main runtime consumers |
| --- | --- | --- |
| Greeting and recording disclosure | `salon_settings` through Conversation `RuntimeConfig`; normalization in `backend/modules/conversation/service_extraction.go` | Simulator uses `initialReply`; phone session uses `initialPhoneReply` |
| AI tone | `RuntimeConfig.AITone`; bounded values in `aiTone` | Guarded style-only reply generation in `service_booking_flow.go` |
| Informational services, hours, staff, and knowledge | Authority-fenced `AnswerContext`; routing/rendering in `answer_router.go` | Conversation Service answer lane and structured-question handling |
| Caller semantic meaning | Structured contracts and validation in `backend/modules/voice_openai/adapter.go` and provider-neutral voice interfaces | State-selected semantic lane in Conversation Service |
| Turn route and state transition | `turn_kernel.go`, `service.go`, validated acts/questions, and `turn_reducer.go` | Production Conversation Service |
| Deterministic customer copy | `answer_router.go`, `service_prompts.go`, `service_guidance_recovery.go`, and reducer composition helpers | Simulator and phone backend replies |
| Knowledge formatting | `service_matching_parsing.go` and `knowledgeAnswerFromMatch` in `answer_router.go` | Structured answer routing after operational sources |
| Optional style rewrite | `Service.applyReplyGenerator` with `ReplyPolicyStyleOnly` and post-generation guardrails | Non-terminal, non-realtime turns only; operational facts retain safe backend copy |
| Realtime phone delivery/recovery | `backend/modules/voice_twilio` admission, queue, playout, and terminal policies | Twilio realtime calls; it does not own semantic meaning or booking state |
| Evaluation fixtures and evidence | `backend/internal/conversationeval` and `backend/cmd/conversation-eval*` | Offline audit, direct pilot, deterministic runtime, and bounded live canaries |

The semantic model does not own business facts, service selection,
availability, booking, or confirmation. Prompt instructions are changed only
after model-executed evidence reproduces a semantic-contract failure that is
not caused by data, cache, routing, copy, channel transport, or fixtures.

## Failure classification

| Failure class | Owning surface | Required evidence before changing it |
| --- | --- | --- |
| `data_source` | AnswerContext repository/source selection | Wrong or missing source rows and retained answer-source evidence |
| `cache_freshness` | AnswerContext cache/version fence | Stale revision/fence evidence, not reply wording |
| `semantic_model` | Voice OpenAI structured interpretation | A bounded model-executed result with the wrong accepted meaning |
| `turn_routing` | Conversation kernel/service | Correct structured meaning followed by the wrong production route |
| `deterministic_copy` | Answer router/recovery/reducer | Correct route/state with misleading, duplicated, or multi-question backend copy |
| `style_rewrite` | Guarded reply generator | Safe backend reply is correct and an accepted style-only rewrite changes meaning |
| `voice_transport` | Twilio/realtime layer | Correct persisted backend turn but wrong admission, suppression, order, or spoken output |
| `scheduling_or_confirmation` | Scheduling authority boundary/executor | Availability/write/confirmation evidence; this is outside P3 |
| `test_fixture_only` | Conversation evaluation | Authored semantics or assertions do not represent the journey; production behavior is not implicated |

One failure can be reclassified when stronger evidence becomes available, but
it must not be assigned to prompt/model quality solely from customer-facing
wording.

## P3 evidence matrix

| Case | Retained evidence | Classification and result |
| --- | --- | --- |
| Informational hours, `question-003` | `semantic_lane`, accepted interpretation, `structured_business_hours`, reason `business_hours`, zero tool attempts | Correct production routing; no prompt change |
| Informational staff, `question-005` | `semantic_lane`, accepted interpretation, `structured_staff`, reason `staff_question`, zero tool attempts | Correct production routing; no prompt change |
| Missing structured policy, `question-010` | Accepted policy meaning followed by source `booking_redirect`, reason `structured_answer_unavailable` | `turn_routing` fixed: truthful owner fallback is retained instead of a generic service-guidance menu |
| Catalog comparison, `question-008` | `structured_service_catalog` and one question in the reply | `deterministic_copy` fixed: an optional follow-up is not appended when the structured answer already asks a question |
| Hours detour during booking, `repair-003` | Selected `svc_willow_gel` survives, source `structured_business_hours`, one resume question, zero tools and no confirmation/provider ID | `deterministic_copy` fixed without changing draft or scheduling behavior |
| Different-technician request, `pilot-038` | Required `set_field/staff/alternative` act with no invented target | Evaluation contract now measures staff preference rather than only forbidding handoff |
| Non-native staff/policy wording, `pilot-028` and `pilot-042` | Distinct `staff` and `policy` subjects across isolated fixtures | Regression coverage is semantic and data-driven, not tied to the original wording |

The P3 direct pilot remains 50 executions across 45 authored situations and
five phone/simulator pairs. The three pilot positions formerly occupied by
repeated hours cases now cover hours, staff, and policy. The P3 direct
ten-scenario canary is:

```text
pilot-003,pilot-007,pilot-014,pilot-015,pilot-019,pilot-028,pilot-032,pilot-038,pilot-042,pilot-050
```

The real-salon live-canary journeys are:

```text
advice-001,consult-001,question-003,question-005,question-008,question-010,booking-001,repair-003,safety-001,failure-002
```

All ten pass the deterministic production-service preflight with scripted
semantics. No paid live model call was made as part of P3 implementation.

## Retained baseline debt

The complete 100-journey deterministic runtime currently retains 87
`runtime_executed` and 13 `failed` results, with zero model executions. The 13
failures already existed before P3 and remain classified as
`test_fixture_only`: `advice-003`, `consult-003`, `question-001`,
`booking-007`, `booking-009`, `booking-010`, `booking-011`, `booking-013`,
`booking-014`, `booking-017`, `booking-019`, `safety-002`, and `safety-005`.
Their authored later-turn semantic fixtures do not yet express the journey
expectation. They are not silently counted as passes and are outside the
focused P3 runtime fixes.

## Invariants and non-goals

- Informational questions do not call availability, booking, or POS tools.
- Informational business hours are not scheduling availability.
- P3 does not change AnswerContext source selection, cache TTL/version fences,
  scheduling authority, executor activation/capability, booking, or
  confirmation contracts.
- P3 does not add phrase matching, keyword routing, a second taxonomy, a
  migration, API route, or frontend surface.
- Operational facts remain deterministic safe replies; optional style output
  cannot invent or remove booking evidence.
- A live canary requires explicit execution approval, an exact
  database-backed OpenAI configuration salon, an explicit paid-call ceiling,
  checkpointing, and retained output. Repository tests never perform it.

## Repository checks

The focused P3 gate is owned by:

```text
backend/internal/conversationeval/semantic_quality_test.go
backend/modules/conversation/service_guidance_recovery_test.go
```

Regenerate retained evaluation artifacts and run the no-model checks with the
commands documented in `docs/api.md`. A failed full deterministic runtime is
expected until the separately classified 13 fixture-only journeys are repaired;
the command still writes the honest report and exits unsuccessfully.
