# Architecture

## Current Milestone

This codebase implements Milestone 1, Milestone 2, and backend booking slices for Milestone 3 of the AI Receptionist system.

The backend is organized as:

```txt
cmd/api              Fiber HTTP server
cmd/worker           worker entrypoint for later async jobs
internal/config      environment config
internal/database    PostgreSQL connection bootstrap and startup migrations
internal/encryption  AES-GCM token encryption
internal/middleware  JWT middleware
modules/auth         login, refresh, roles
modules/salon        salon profile, settings, business hours
modules/pos          provider-neutral POS contracts and persistence
modules/pos_square   Square adapter and Square integration routes
modules/booking      booking attempts, appointments, and fallback pending safety
```

The frontend is organized as:

```txt
app/                 Next.js routes
components/ui        reusable UI primitives
components/layout    dashboard shell
features/auth        login flow
features/dashboard   dashboard home
features/integrations Square integration page
features/onboarding salon profile creation
lib/api              typed API client
types                API response types
```

## Core Boundary

Correct dependency direction:

```txt
HTTP handler -> service -> repository/provider interface -> concrete adapter
```

The booking service depends on `modules/pos.POSProvider`. It must not import `modules/pos_square`.

Square create-booking, reschedule, cancel, and dashboard test-booking gate operations are implemented inside `modules/pos_square` and routed through the provider-neutral booking service where appointment state changes are required. Until a provider returns a POS booking ID and booking version, booking requests must be stored as fallback pending attempts and must not create confirmed appointments. Reschedule, cancel, and test-booking cleanup requests must leave the internal appointment unchanged unless the provider succeeds.

The AI conversation engine planned for Milestone 4 must call booking tools only. It must not know Square OAuth, Square API payloads, Square location IDs, or token storage.

## Data Ownership

SQL files under `backend/migrations` are the current database source of truth. The API startup migrator applies them once and records checksums in `app_schema_migrations`. Ent schema files mirror the table structure so generated clients can be introduced without changing the domain boundaries.

Tenant isolation is enforced by owner-scoped queries in repositories and by salon ownership checks before POS actions.
