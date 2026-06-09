---
name: pos-adapter-slice
description: Implement or review POS adapter work for this repo. Use when changing POSProvider, SquareAdapter, POS connections, sync logs, POS errors, OAuth, provider DTOs, service/staff sync, or future POS integration seams.
---

# POS Adapter Slice

Use this skill for all POS integration work.

## Required Reading

- `docs/pos-adapter-layer.md`
- `docs/square-integration.md`
- `backend/modules/pos/types.go`
- The relevant adapter package, currently `backend/modules/pos_square`

## Non-Negotiables

- Booking Service depends on `pos.POSProvider`, not `pos_square`.
- Square payloads, endpoint URLs, OAuth behavior, token refresh, and provider error mapping stay inside `SquareAdapter`.
- POS tokens are encrypted at rest and never returned to the frontend.
- POS failures are logged to `pos_errors` with normalized codes.
- Sync operations write `pos_sync_logs`.

## Implementation Flow

1. Confirm the provider-neutral DTO or method shape first.
2. Update migrations and Ent schemas when persistence changes.
3. Implement adapter mapping behind the interface.
4. Keep handler responses provider-neutral.
5. Add focused tests for mapping, error normalization, and token handling.
6. Validate with:

```bash
cd backend
GOCACHE=/private/tmp/manleai-go-cache go test ./...
```

## Future Provider Rule

Add future providers as new packages such as `modules/pos_vagaro`. Do not add fake implementations for providers not actually integrated.

