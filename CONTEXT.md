# AI Receptionist Domain Context

This file defines shared language for agents working in this repo. Use these terms consistently in code, docs, tests, PRDs, and review notes.

## Product Language

**Salon**
The tenant business using the AI receptionist. All operational data is scoped by `salon_id`.

**Owner**
The primary salon user who configures settings, connects Square, receives handoffs, and reviews dashboard activity.

**Canonical Operational Data**
ManleAI-owned salon data such as services, staff, customers, AI controls, owner workflow state, fallback pending requests, logs, and training data. POS providers are projections and booking execution layers, not the primary identity for this data.

**AI Receptionist**
The voice agent that answers inbound calls, collects booking details, answers FAQs, and routes work to booking and handoff tools.

**AI Receptionist Tone**
A salon-scoped speaking-style preset stored on `salon_settings.ai_tone`. It can make backend-approved replies sound warmer, more natural, younger, or more concise, but it is not a booking rule, provider credential, or free-form prompt override.

**Conversation Engine**
The state machine and tool-calling layer behind the AI Receptionist. It must not know Square payloads, OAuth tokens, or provider-specific booking details.

**Service Understanding**
The backend-owned interpretation layer that maps customer service utterances to active salon services. It uses the salon catalog and active service aliases, asks clarification for generic or fuzzy family matches, and records diagnostic transcript metadata.

**Service Alias**
A salon-scoped learned phrase for a real service, stored in `service_aliases` with a normalized alias key. Owner corrections can create or update service aliases; aliases are operational data, not prompt text.

**Booking Service**
The internal domain service that creates, reschedules, cancels, and records appointments through the active `POSProvider`.

**POS Provider**
The provider-neutral interface in `backend/modules/pos`. Booking and AI code use this interface only.

**POS Adapter**
A concrete provider implementation such as `SquareAdapter`. It owns provider-specific auth, API calls, payload mapping, retries, and error normalization.

**SquareAdapter**
The first real POS Adapter. It is the only production POS implementation in the pilot.

**POS Entity Link**
A mapping between a ManleAI canonical record and a provider-side record, such as a service, staff member, or customer. Provider IDs are mappings, not primary product identity.

**Provider Projection**
Provider-side data imported from or written to an active POS provider so ManleAI records can be used for availability and booking execution.

**Sync Status**
The state of a canonical record's provider mapping, such as `local_only`, `syncing`, `synced`, `sync_failed`, `unmapped`, or `archived`.

**AI Bookable**
An internal dashboard control that can only be enabled when a canonical service or staff record is active and has a valid link for the active POS provider.

**Fallback Pending Request**
A non-confirmed booking request created when POS booking fails, times out, is disconnected, or lacks permission. The AI may take customer information, but must not say the appointment is confirmed.

**Booking Attempt**
An auditable record of a customer booking flow, including status, POS provider, POS booking ID when available, and any error.

**POS Error**
A normalized, salon-scoped record of provider failures. Use codes such as `POS_TOKEN_EXPIRED`, `POS_BOOKING_FAILED`, and `POS_TIMEOUT`.

## Product Invariants

- Confirmed booking means the active POS provider returned success.
- Internal calendar records are mirrors or logs, not the source of booking truth for the pilot.
- ManleAI owns canonical salon operational data; the active POS owns booking execution.
- Service recognition must be catalog-backed and salon-scoped. Do not hardcode per-salon service keywords into prompts or generic matchers.
- AI tone changes reply style only. It must not change required booking slots, handoff decisions, availability checks, service selection, or confirmed-booking wording.
- Square is the first real integration; future POS names are architecture targets, not implemented features.
- Vietnamese language support is product scope, but English remains the primary pilot language unless a feature explicitly says otherwise.

## Avoid These Terms

- "Universal POS support" for the pilot.
- "Square is the source of truth" for all salon operational data.
- "Confirmed" when only an internal record exists.
- "Mock POS" as a production path.
- "Placeholder" for required pilot behavior.
