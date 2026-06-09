---
name: voice-ai-runtime
description: Design or implement AI receptionist, conversation engine, telephony, SMS, STT, LLM, TTS, transcript, summary, handoff, and voice provider abstractions for this repo.
---

# Voice AI Runtime

Use this skill for Milestone 4 and Milestone 5 work.

## Boundaries

- Conversation Engine owns state, intent, entity extraction, and tool routing.
- Booking tools call Booking Service.
- Booking Service calls `POSProvider`.
- AI runtime must not call Square or read POS tokens.
- Telephony, SMS, STT, LLM, and TTS must be provider abstractions.

## Conversation Rules

- Ask one question at a time.
- Keep responses short.
- Do not invent prices.
- Use "starting at" when price varies by design, length, add-on, or technician.
- Transfer to the owner for human requests, complaints, refunds, payment disputes, complex group bookings, and low confidence.
- Never say a booking is confirmed unless POS booking succeeded.

## Implementation Flow

1. Define provider interface and domain DTOs first.
2. Add persistence for call sessions, transcript, summary, handoff, and booking attempt linkage.
3. Implement a deterministic simulator before live telephony.
4. Add tests for state transitions and fallback text.
5. Wire Twilio/OpenAI/provider specifics behind adapters only.

