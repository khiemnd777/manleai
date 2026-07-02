---
name: voice-ai-runtime
description: Design or implement AI receptionist, conversation engine, service understanding, service aliases, telephony, SMS, STT, LLM, TTS, transcript, summary, handoff, and voice provider abstractions for this repo.
---

# Voice AI Runtime

Use this skill for conversation, telephony, external AI provider, realtime stream,
transcript, handoff, call lifecycle, and voice booking work across Milestone 4+.

## Codebase Truth Rule

- Be strictly honest about the current codebase and runtime configuration. Do not flatter, reassure, or infer readiness from docs, milestones, intended architecture, or previous claims.
- Before saying voice, STT, LLM, TTS, telephony, bilingual behavior, or booking-by-phone works or only needs testing, verify the actual runtime wiring, provider config gates, webhook path, prompts, persistence, and tests/status output when available.
- Separate confirmed current behavior from product intent, planned behavior, unverified assumptions, and missing implementation.
- If evidence conflicts, stop and name the conflict directly with file references instead of smoothing over it.

## Product-Grade Rule

- Treat voice and AI runtime work as production-grade pilot behavior, not MVP, demo, prototype, or happy-path scaffolding.
- Before proposing or implementing, check repeated webhook/event behavior, idempotency, duplicate transcript/message/session prevention, retry/rerun semantics, provider timeout handling, tenant/security/privacy boundaries, and safe fallback/handoff states.
- Conversation, telephony, SMS, STT, LLM, TTS, summary, handoff, and booking-tool flows must define stable event/session keys when messages can be replayed or retried.
- Do not mark runtime work complete until repeated provider callbacks cannot create duplicate rubbish state or misleading caller/owner responses.

## Boundaries

- Conversation Engine owns state, intent, entity extraction, and tool routing.
- Service understanding belongs in deterministic backend code. Interpret service utterances against active catalog services and active salon-scoped `service_aliases`; exact catalog names beat aliases, aliases beat generic family matching, and fuzzy/generic family matches should ask catalog-backed clarification instead of selecting a service.
- Booking tools call Booking Service.
- Booking Service calls `POSProvider`.
- AI runtime must not call Square or read POS tokens.
- Telephony, SMS, STT, LLM, TTS, and realtime audio must be provider abstractions.
- AI receptionist tone is style-only salon runtime config. Read it from backend settings, pass it through provider-neutral request DTOs, and keep deterministic conversation guardrails authoritative before any LLM rewrite or audio synthesis.
- Call archive, redaction, retention, and temporary audio outputs must preserve
  audit links while clearing customer PII and transcript/audio content when
  lifecycle rules require it.

## Conversation Rules

- Ask one question at a time.
- Keep responses short.
- Preserve known slots. Do not ask for a known day/time/service again unless the caller corrects it or availability invalidates it.
- Treat service phrases in the customer-name slot as service corrections, not customer names; re-check availability before continuing.
- Do not invent prices.
- Use "starting at" when price varies by design, length, add-on, or technician.
- Transfer to the owner for human requests, complaints, refunds, payment disputes, complex group bookings, and low confidence.
- Never say a booking is confirmed unless POS booking succeeded.
- Tone presets may make a safe reply more natural, human, young, concise, or warm, but must not change required questions, known-slot preservation, handoff decisions, availability checks, or POS confirmation boundaries. Realtime must speak backend-approved replies and must not use independent realtime instructions to bypass those rules.

## Implementation Flow

1. Define provider interface and domain DTOs first.
2. Add persistence for call sessions, transcript, summary, handoff, and booking attempt linkage.
3. Implement a deterministic simulator before live telephony.
4. Add tests for state transitions and fallback text.
5. Add golden conversation regressions for service understanding, alias matches,
   slot preservation, name-quality, terminal booking replies, and no provider
   leakage in customer-facing text.
6. Wire Twilio/OpenAI/provider specifics behind adapters only.
7. For AI tone changes, verify `salon_settings.ai_tone` flows into phone/LLM requests, legacy imports default safely, and tone-only changes do not alter booking, handoff, or service-understanding outcomes.
8. For realtime, retention, or audio-output changes, verify replay/idempotency,
   signature checks, lifecycle gates, expiration, and redaction behavior.
