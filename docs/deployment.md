# Deployment

This release deploys the Go API, background worker, PostgreSQL, Redis, the
owner admin dashboard, the public salon landing app, and the POS calendar app.
Public HTTP and HTTPS are owned by the shared VPS edge gateway, not by the
ManleAI Docker Compose project.

Scheduling load and concurrency verification is a separate non-production
release operation. It must not run in the deployment stack or against its
database. Provision an explicitly attested isolated database and follow
`docs/operations/scheduling-load-harness.md`; a passing harness report is not a
production capacity claim without an approved witnessed representative run.

## Production Domains

- Admin dashboard: `https://ai.knasoftware.com`
- API through the admin origin: `https://ai.knasoftware.com/api/*`
- Public salon landing app: `https://salon.knasoftware.com`
- POS calendar app: `https://pos.knasoftware.com`
- Square redirect URL: `https://ai.knasoftware.com/api/integrations/square/callback`
- Voice public base URL: `https://ai.knasoftware.com`

All DNS `A` records must point to the VPS before Caddy can issue certificates.

## Edge Gateway

The VPS has one shared systemd Caddy service. It owns ports `80` and `443`; its
managed root config imports `/etc/caddy/projects/*.caddy`. ManleAI does not run
its own Caddy container and must never edit `/etc/caddy/Caddyfile` directly.

`project-edgectl` is the only supported project route manager. It stores each
project route under `/etc/caddy/projects/<project-id>.caddy`, records ownership
in `/var/lib/project-edge/registry/<project-id>.json`, validates the combined
Caddy config, rejects domain or upstream conflicts, backs up the project route,
and reloads Caddy with rollback on failure.

ManleAI exposes:

```txt
127.0.0.1:18089 -> api:8080
127.0.0.1:13088 -> frontend:3000
127.0.0.1:13090 -> landing:3000
127.0.0.1:13091 -> pos-calendar:3000
```

For `/api/*`, the rendered Caddy route overwrites
`X-ManleAI-Client-IP` with `{remote_host}` before proxying. The API uses only
that edge-owned header (or the direct socket IP outside Caddy), HMACs the value,
and never accepts a caller-selected rate-limit identity.

The CI/CD deploy job renders `deploy/manleai.caddy.template` and
`deploy/manleai.edge-manifest.template`, then runs:

```bash
sudo -n project-edgectl validate manleai <route-file> <manifest-file>
sudo -n project-edgectl upsert manleai <route-file> <manifest-file>
```

`upsert` runs only after the ManleAI API loopback healthcheck succeeds. It can
change only the ManleAI route and manifest; existing project route files remain
owned by their registered projects.

## GitHub Actions

The workflow in `.github/workflows/ci-cd.yml` runs only when a release tag
matching `v*` is pushed. Commit pushes to `main` do not trigger this workflow.
The normal release command is:

```bash
make release TAG=v2026.06.25.1
```

That command requires a clean `main` worktree, creates an annotated git tag, and
pushes the tag to `origin`. The tag push runs the complete backend test/vet
contract, a bounded high-risk race suite, all three web-app typecheck/build
jobs, and the isolated PostgreSQL Owner-first release gate documented in
`docs/operations/release-gate.md`. The PostgreSQL gate starts from an empty
dedicated database, proves migrate-twice/checksum behavior, verifies every V46
through latest migration ledger entry, runs the declared integration packages
serially in isolated per-package database clones, and executes the explicit
tenant/security contract. Image publication and deployment require all of those
jobs to pass and remain tag-only.

The workflow then builds and publishes the five application images to GitHub
Container Registry (GHCR) with that immutable release tag. The VPS receives
only the Compose/Caddy deployment bundle, `project.env`, and the image tag
metadata. It logs into GHCR with the job's ephemeral `GITHUB_TOKEN`, pulls the
tagged images, logs out, and starts the stack without compiling application
source code. Each image carries the `org.opencontainers.image.source` label for
this repository, so the GHCR package inherits repository access permissions.

A passing release gate is code-readiness evidence only. It does not prove live
salon-scoped provider configuration, credentials, callback reachability,
provider delivery, production backup capacity/retention, a witnessed restore,
alert routing, or on-call readiness. Those remain separate dashboard-backed
runtime checks and operational approvals.

Every deploy SSH/SCP connection has a 15-second connection timeout and sends
keepalives during long-running work. Before replacing containers, the release
requires an exact tag-specific declaration that the previous image is
compatible with the candidate forward schema, takes and validates a private
pre-deploy PostgreSQL backup, and tags the currently running application images
locally. If the API healthcheck or edge-gateway validation/reload fails, it may
restore those images only under that compatibility declaration and keeps the
previous `/opt/manleai/current` release active. Image rollback does not restore
the database; PostgreSQL remains at its forward schema. Only after both checks
succeed does the workflow move `/opt/manleai/current` to the new release:

```bash
docker login ghcr.io
for image in manleai-api manleai-worker manleai-frontend manleai-landing manleai-pos-calendar; do
  docker pull "ghcr.io/khiemnd777/$image:<release-tag>"
done
docker logout ghcr.io
docker compose \
  --env-file /opt/manleai/project.env \
  --env-file /opt/manleai/releases/<release>/images.env \
  -f docker-compose.prod.yml -p manleai up -d --no-build --remove-orphans
sudo -n project-edgectl validate manleai <route-file> <manifest-file>
sudo -n project-edgectl upsert manleai <route-file> <manifest-file>
```

The workflow uses an SSH private key for the deploy identity. The GHCR token is
written to a mode-`600` temporary file on the VPS only for `docker login`, then
removed on normal success and failure paths; diagnostics also attempt removal.
It is never added to `project.env`, committed to the repository, passed as a
remote command argument, or left in Docker's credential store after
`docker logout`. The workflow uses the repository-scoped `GITHUB_TOKEN`; no
additional long-lived GHCR credential is required.

The VPS pulls release images one at a time to bound memory, CPU, network, and
disk-extraction pressure. It does not prune shared Docker build cache. Docker
Compose starts containers only after every required image is present locally.
During the first GHCR migration, an old running container can refer to image
metadata that Docker no longer retains. The workflow reports that case and
proceeds without automatic image rollback; after the first successful GHCR
release, every running application image is tag-addressable and rollback is
available again.

If the remote deploy command fails or its SSH session disconnects, the workflow
opens a short diagnostic SSH session and reports memory, deploy-path disk
usage, Docker disk usage, Compose status, and bounded application logs. It does
not print `project.env` or any deployment secret. If that follow-up connection
also fails, the original deploy failure remains the authoritative result.

Required GitHub Secrets:

```txt
SERVER_IP
REMOTE_USER
SSH_PRIVATE_KEY
PROJECT_ENV_B64
```

Use the VPS host and dedicated deploy identity for `SERVER_IP` and
`REMOTE_USER`. `SSH_PRIVATE_KEY` must be the matching private key. Do not
commit the private key or production environment file.

Required variables in the protected `production` GitHub environment:

```txt
MIGRATION_COMPATIBILITY_RELEASE_TAG
PREVIOUS_IMAGE_DB_COMPATIBLE
MIGRATION_COMPATIBILITY_APPROVER
POSTGRES_BACKUP_STORAGE_APPROVAL
```

The compatibility tag, decision, and approver are release-specific. The release
tag must match exactly, compatibility must be the literal `true`, and the
approver must be a bounded reviewer identity. A prior compatibility declaration
does not authorize a later tag. Storage approval must be the exact value
`encrypted-private:/opt/manleai/backups` and remains valid only while the path,
access controls, encryption, retention, and key ownership remain approved. The
deploy fails before database mutation when the declaration or storage approval
is missing, stale, malformed, or false. The storage value is an operator
attestation; it does not encrypt the path.

## Database Backup, Restore Drill, And Rollback

The release bundle includes `backend/migrations` for checksum validation plus
the bounded database scripts in `deploy/`:

- `postgres-backup.sh` creates a custom-format `pg_dump`, checks its catalog
  with `pg_restore --list`, and writes an SHA-256 sidecar;
- `postgres-restore-drill.sh` restores only into one new explicit
  `manleai_restore_drill_...` database and never drops or overwrites a target;
- `postgres-verify-restore.sh` checks exact `app_schema_migrations` file/name/
  checksum parity, required tables/constraints, and bounded tenant smoke
  queries.

Before a non-initial deployment starts the candidate API/migrator, the workflow
backs up the explicit `POSTGRES_DB` on the existing Compose PostgreSQL service
to `/opt/manleai/backups/predeploy-<release-id>.dump`. The artifact, checksum,
compatibility declaration, and backup record use private permissions and must
live on an approved encrypted volume. If an existing container is stopped, or
the project volume exists without a Compose container, the source is ambiguous
and deployment stops. Only a truly empty initial deployment with no database
container and no project volume may record backup as not applicable.

Database restore is never part of automatic deploy failure handling. Use the
manual `.github/workflows/postgres-restore-drill.yml` only with its protected
non-production environment and dedicated `DRILL_SERVER_IP`,
`DRILL_REMOTE_USER`, and `DRILL_SSH_PRIVATE_KEY` secrets. It takes explicit
source/target names and RPO/RTO targets, leaves the private dump and isolated
target on the non-production host, and uploads only a sanitized drill report.
Never configure those secrets to the production VPS.

Complete procedures and evidence requirements are binding in:

- [PostgreSQL Backup And Isolated Restore Drills](operations/postgres-backup-restore.md)
- [Migration Release And Rollback](operations/migration-rollback.md)

Migrations are forward-only expand/contract changes. The previous image must be
proven compatible with the expanded schema before automated image rollback is
available. A database recovery first restores and validates a selected artifact
in an isolated database, then requires a separate DBA-approved production
recovery/cutover plan that accounts for writes after the snapshot and provider-
side reconciliation. No repository script automatically deletes or replaces a
production database.

## Project Env

Create the production env file from the template:

```bash
cp deploy/project.env.example project.env
```

Generate replacement values for the required placeholders:

```bash
openssl rand -hex 24
openssl rand -base64 48
openssl rand -base64 32
```

Use separate URL-safe random values for `POSTGRES_PASSWORD` and
`DATABASE_RUNTIME_PASSWORD`, the 48-byte base64 value for `JWT_SECRET`, and the
32-byte base64 value for `TOKEN_ENCRYPTION_KEY_BASE64`. `DATABASE_URL` must use
the non-owner `DATABASE_RUNTIME_ROLE`; `MIGRATION_DATABASE_URL` must use the
migration/table owner. Never make the runtime role a member of the migration
role and never grant it `SUPERUSER` or `BYPASSRLS`.

On a new PostgreSQL volume, `deploy/postgres-init-runtime-role.sh` creates the
runtime login with `NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS`.
For an existing volume, create or rotate that role once through an approved DBA
change before deploying this release. The exact statement is equivalent to:

```sql
CREATE ROLE manleai_runtime LOGIN PASSWORD '<separate-secret>'
  NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS;
```

Do not put the password in shell history. The candidate API uses the migration
connection to apply V63-V72 and grant table/sequence/function privileges to the
already-existing runtime role, then closes that connection. API requests and
the worker use only the runtime connection. The worker does not receive
`MIGRATION_DATABASE_URL`; production startup fails if the connected role name,
ownership, `SUPERUSER`, `BYPASSRLS`, or RLS policy checks are unsafe.

Production forces rate limiting on (the template records
`RATE_LIMIT_ENABLED=true`), uses `REDIS_URL` as a required request-protection
dependency, and keeps
`RATE_LIMIT_CLIENT_IP_HEADER=X-ManleAI-Client-IP` aligned with the Caddy route.
API startup fails if the configured Redis endpoint cannot be reached, runtime
Redis failures return generic `503 RATE_LIMIT_UNAVAILABLE`, and `/healthz`
fails until Redis recovers. Local Compose leaves the limiter off by default;
set `RATE_LIMIT_ENABLED=true` to exercise it against the local Redis service.

The dashboard and POS apps use credentialed requests with an HttpOnly refresh
cookie and memory-only access tokens. Production CORS therefore allows
credentials only for the configured exact origins. The three web apps install
a per-request nonce CSP and force dynamic rendering so Next.js scripts and
styles receive the matching nonce. Production policy has no wildcard,
`unsafe-inline`, or `unsafe-eval`; validate the emitted
`Content-Security-Policy` header after deployment rather than inferring it from
the template alone.

After editing `project.env`, store it as a single-line base64 GitHub Secret:

```bash
gh secret set PROJECT_ENV_B64 --body "$(base64 -i project.env | tr -d '\n')"
```

Or paste the output of this command into the GitHub Secrets UI:

```bash
base64 -i project.env | tr -d '\n'
```

`project.env` is ignored by git.

## VPS Requirements

- Docker Engine with Docker Compose v2.
- Ports `80` and `443` open to the internet.
- DNS for `ai.knasoftware.com`, `salon.knasoftware.com`, and
  `pos.knasoftware.com` pointing to the VPS before certificate issuance.
- Systemd Caddy active with managed root `/etc/caddy/Caddyfile` and
  `project-edgectl` installed.
- A dedicated deploy identity that can access Docker, owns `/opt/manleai`, and
  has passwordless sudo only for `/usr/local/bin/project-edgectl`.
- SSH public-key access for that deploy identity; the matching private key is
  stored only in the `SSH_PRIVATE_KEY` GitHub secret.

Existing project Caddy routes are not modified by this deployment. Do not
install another Caddy container, restart Caddy manually, or bind application
containers to ports `80` or `443`.

## First Platform Administrator Bootstrap

V64 intentionally does not add an unauthenticated Platform-admin HTTP
bootstrap. After the migration is applied, an operator may promote one exact
existing active user only while no active Platform Admin exists:

```bash
cd backend
go run ./cmd/platform-access bootstrap-admin \
  --email operator@example.com \
  --action-key initial-platform-admin-2026-07-26 \
  --reason approved-change-reference
```

The command prefers the migration-owner `MIGRATION_DATABASE_URL` (and uses
`DATABASE_URL` only in non-RLS legacy/local setups), takes an advisory
lock, verifies the target account is active, creates the assignment plus
immutable action/event evidence atomically, and prints only the bounded
assignment result. Exact action replay returns the same result. Changed action
reuse conflicts, and the command closes permanently as soon as any active
Platform Admin exists. All later Platform role, tenant membership, salon
assignment, capability, and PII-grant changes use the authenticated
`/api/platform/access/*` APIs. Do not use direct SQL or the legacy
`super_admin` role as an authorization substitute.

`--reason` is an opaque operator change reference, not a note. It must match
`[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}` and must not contain customer data.

## Dashboard-Managed Provider Configuration

Square Appointments, Twilio, and OpenAI credentials are configured in the
Platform tenant detail's Technical tab and stored encrypted per salon in
`salon_integration_configs`. Repository env files and env templates contain
infrastructure settings only; do not add provider credentials, provider model
settings, webhook paths, transports, or provider URLs to them.

For active-runtime diagnosis, use `/platform/tenants/:tenant_id/technical`,
`GET /api/platform/tenants/:tenant_id/technical/integration-configs`, the relevant readiness/debug endpoint
such as `GET /api/salons/:id/voice/status`, persisted provider records, and the
runtime resolver code. Never use `project.env`, `.env`, Compose defaults,
GitHub secrets, or process environment values as evidence of the active
salon-scoped provider configuration.

The backend retains legacy environment fallback code for compatibility. That
path is not the normal production configuration workflow and is intentionally
absent from repository env templates. Inspect it only for an explicitly scoped
bootstrap/legacy task after proving that the salon has no stored provider
configuration. `ResolveSquareConfig`, `ResolveTwilioConfig`, and
`ResolveOpenAIConfig` enter that legacy bootstrap path only when the repository
returns the exact integration-config `ErrNotFound`. A repository failure,
malformed/non-string persisted settings object, or stored-secret decryption
failure propagates and stops provider runtime. Once a salon/provider row exists,
that row owns enabled state, settings, and credentials: missing stored
credentials never inherit environment secrets, and a disabled stored provider
cannot be re-enabled by process configuration. Stable protocol defaults such as
Twilio webhook paths, Square API version/host selection, and the OpenAI provider
URL may still be applied by code where their owning contract defines them; they
are not salon credentials.

The authenticated integration response follows the same ownership boundary.
When no stored row exists it may label an available bootstrap secret source as
`environment`. When a stored row exists but its secret is empty or unreadable,
the response reports source `none` and configured `false`; it never relabels an
environment secret as active, and it never returns the secret value.
Whole-response contract tests also prohibit write-only credential, SID,
destination, and `clear_*` request fields from appearing in serialized reads.

The owner-notification Twilio Messaging resolver is an explicit exception to
that legacy behavior: it resolves only the salon's stored encrypted record and
fails closed on missing, disabled, invalid, repository, or decryption state. It
never reads provider configuration from environment variables. Its dashboard
contract includes explicit owner-SMS enablement, the exact E.164 owner
destination and fresh consent attestation, Account SID/Auth Token, one
Messaging Service SID or sender number, and public HTTPS status/inbound callback
URLs. The response exposes only configured flags, a masked destination, and the
computed callback URLs.

Public buffered-TTS playback uses a separate narrow exception:
`ResolveStoredTwilioAuthToken` reads only the salon's encrypted Twilio auth
token from `salon_integration_configs`. It never uses legacy environment
fallback and does not require Account SID, Messaging Service, sender,
owner-SMS enablement, or owner-SMS consent. Voice-only salons can therefore
sign `/api/voice/audio/:id` capabilities without enabling messaging. If the
stored token is missing or cannot be decrypted, the backend emits no unsigned
audio URL and the phone response falls back to safe TwiML speech. Token rotation
immediately invalidates any previously generated audio capabilities.

Square booking-webhook verification is also salon-scoped dashboard data. Store
the exact public HTTPS notification URL and write-only signature key in the
Platform tenant Technical tab; do not add either value to repository env templates.
`webhook_configured` means verifier credentials are present, not that the
Square subscription or deliveries are healthy. Configuration transfer
preserves the destination deployment URL and requires secret re-entry. The API
process receives and durably enqueues events; the worker processes fenced
claims and runs scheduled calendar repair. Migrations V39-V42 add booking
integrity/reconciliation/quotes, fail-closed consultation defaults, Square
webhook/repair persistence, and conversation state revision. They remain under
the existing startup migrator and must not be run manually during source review.
V60 adds bounded dead-letter/requeue state, action-key-idempotent owner actions,
safe diagnostic class/code fields, and calendar-repair health evidence. The
connected Square card reads that state through the authenticated
`/api/salons/:id/square-webhook-events` operations API. It never receives raw
payloads, signatures, claim tokens, provider identifiers/responses, customer
data, or raw errors. A requeue is allowed only when the backend returns
`can_requeue=true`; after an uncertain browser response the dashboard repeats
the same action key and uses `X-Idempotent-Replay` only to label exact recovery.
For a separately hosted dashboard/API deployment, CORS must expose that response
header. Header absence must not be interpreted as replay or as failure.
V63 removes historical provider-controlled POS/Square diagnostic text and
payloads and narrows legacy Twilio/OpenAI Realtime failure audit payloads to the
same fixed-message/allowlisted contract enforced by the current runtime.
V64 is an expand-only SaaS access-control migration. It retains
`salons.owner_user_id`, legacy roles, and every previous owner query while
adding owner-membership backfill/synchronization, Platform role and per-salon
delegation tables, maximum-24-hour Platform PII grants, and immutable access
actions/events. The previous image remains schema-compatible, but the V64
backfill and access integration tests must pass. V65-V72 complete the shared
Business contract, Platform technical plane, runtime membership boundary, RLS,
tenant quotas/fairness, audited AI-runtime control, public safe projection, and
exact Platform PII-scope enforcement. All V63-V72 entries must appear exactly
once in `app_schema_migrations` before the candidate is healthy.
Migration V43 adds the monotonic location-scoped provider snapshot generation
used to reject stale and out-of-order full imports. On first deployment it
preserves Square credentials and terminal connection states, but changes every
previously runnable Square connection to `connected`, clears `last_sync_at`,
and requires one successful full sync for the selected location before catalog,
availability, retry, or booking paths become ready again. Do not bypass that
resync by editing connection status or timestamps. Every later full snapshot
also clears `last_sync_at` when it begins; an error completion keeps it empty,
and only an `active` successful completion restores the timestamp and synced
readiness.

The configured OpenAI reply model may serve guarded style-only rewriting,
strict structured interpretation for freeform semantic-lane turns, and phrasing
of one backend-selected consultation question. Turn input is limited to a
PII-reduced caller utterance, boolean customer-field presence,
selected/catalog service and staff references, ready consultation profiles and
revisions, party-group references, pending act, booking action, dialog phase,
and draft revision. The backend rejects low-confidence goals, invalid
entity/operation combinations, malformed counts, and non-catalog IDs. The model
cannot mutate the draft, rank/recommend services, call booking, or produce
confirmed appointment state. When the provider is disabled or unavailable,
ambiguous mutations preserve the current draft and enter safe
clarification/handoff behavior.

After saving salon-scoped OpenAI settings, run:

```txt
POST /api/salons/:id/voice/semantic-check
```

This owner-authenticated probe uses the same configured reply model and strict
structured-turn schema as live interpretation, but contains no caller
transcript and creates no conversation/POS state. `verified=true` proves the
provider accepted the current contract. A nonretryable live 4xx contract
rejection opens a local circuit scoped to salon plus provider/model/key/schema
fingerprint so repeated caller turns do not resend the same invalid request;
changing that configuration resets the scope, and a successful semantic check
closes the matching circuit. Operational output is limited to request ID,
status class, safe provider error type/code/parameter, schema fingerprint, and
circuit state; response bodies and provider messages are never logged or
returned.

## Consultation Data Pack After Deployment

Schema migration V38 creates the consultation profile table, but schema
migrations do not contain tenant-specific salon data. To reproduce the investor
demo catalog configuration without manual re-entry:

1. Deploy the release and connect the target salon to Square Appointments.
2. Sync Square until the seven target services exist with matching names and
   durations and are active-provider, POS-linked, synced, and AI-bookable.
3. In `/dashboard/settings`, open Configuration transfer and choose
   `docs/lotus-investor-demo-consultation-pack-v7.json`.
4. Confirm the pack contains 5 service categories and 20 category aliases;
   Preview reports 25 taxonomy creates, 7 service alias creates, and 7 service
   consultation profile creates with zero conflicts, then Apply once.
5. Verify the consultation toggle and profile coverage in Settings before the
   production call test.

The scoped v7 pack does not overwrite salon profile, integration configuration,
AI runtime settings, secrets, services, or POS mappings. Missing, ambiguous, or
ineligible profile targets block Apply. Reapplying the same payload is
idempotent and does not create duplicate profiles or increment unchanged profile
revisions.

## Production Rules

- Do not run `backend/seed/local.sql` in production.
- Do not log Square access or refresh tokens.
- Keep `AUTO_MIGRATE=true` for the API unless another release process applies
  the same SQL migrations. The worker runs with `AUTO_MIGRATE=false` and starts
  only after the API healthcheck succeeds.
- V39 intentionally aborts when one request fingerprint has multiple historical
  attempts whose provider dispatch cannot be proven not started. Reconcile
  those POS outcomes before retrying the migration; do not force the unique
  fingerprint index by deleting or auto-closing unknown attempts.
- Configure the Square redirect URL in the Platform tenant Technical tab to the deployed API callback.
- Configure the exact Square booking-webhook HTTPS notification URL and signature key in the Platform tenant Technical tab, then verify subscription/delivery health separately in Square; the local configured badge is not a delivery-health check.
- Monitor the connected Square card's webhook backlog, failed/dead-letter
  counts, recent success window, and calendar-repair state. Treat
  `processing`, repeated failures, dead letters, a degraded repair backstop, or
  stale worker evidence as incident inputs. Requeue only records with backend
  `can_requeue=true`; an `ignored` event is read-only, and webhook processing is
  never direct appointment-confirmation evidence.
- Run the worker for POS sync, booking lease expiry, bounded availability-quote cleanup, Square webhook processing, scheduled calendar repair, owner/customer-notification delivery, call retention, and V61 scheduling-PII retention. The generic scheduler persists start/heartbeat/finish evidence in V57, uses a distributed job lease across worker replicas, and stores only safe class/code diagnostics. Each job still has an independent synchronous recurring loop, so a slow webhook/repair batch cannot starve lease recovery. Quote cleanup runs every five minutes, drains at most eight batches of 250 quotes per run, keeps a 24-hour post-expiry grace for unconsumed quotes and a 30-day audit window for orphaned consumed quotes, and preserves every quote still linked to a booking attempt. `scheduling_pii_retention` runs every five minutes with a default maximum of 100 retention work items, uses bounded `SKIP LOCKED` claims and one-row transactions, and makes no provider calls. Its baseline 90-day expiry applies only after terminal business and delivery state; pending/contacted requests, live leases, queued/retrying/unknown outcomes, open reconciliation, and active customer consent/STOP routing keys remain intact. See `operations/operations-health.md`; deployed code is not configured proactive monitoring until the worker is running and an external monitor/on-call process evaluates the owner status endpoint.
- Treat the V61 90-day duration as a technical baseline, not legal approval. Before production rollout, obtain the applicable privacy/legal decision for scheduling, notification, audio, backup, litigation-hold, and deletion/DSAR obligations; document any approved policy-version change and keep the worker batch/cadence capacity-monitored.
- Configure the Platform tenant Technical Twilio public base URL to the deployed API origin used in Twilio webhook settings.
- Keep the Platform-managed Twilio auth token present for signed buffered-TTS playback.
  Audio URLs are database-expiry-bounded HMAC capabilities; do not rewrite,
  log, or persist their query strings. Rotating the token revokes outstanding
  playback URLs and new phone turns generate capabilities with the new token.
- For owner-operational SMS, configure explicit owner consent/destination,
  Account SID/Auth Token, Messaging Service SID or sender, and the displayed
  status/inbound callback URLs in the Platform tenant Technical tab. Preserve the
  exact public URL/form parameters for `X-Twilio-Signature` verification,
  configure Messaging Service Advanced Opt-Out, and monitor the
  `notification_delivery` job/backlog/dead-letter evidence. `queued`,
  `provider_accepted`, and `sent` are not delivery proof. Follow
  `operations/owner-notification-delivery.md` for retry and unknown-outcome
  handling.
- For customer appointment SMS, configure the same salon-scoped Twilio
  transport in `/platform/tenants/:tenant_id/technical`, then separately enable the
  default-off customer policy and quiet hours in `/dashboard/settings`.
  Configure Twilio Messaging Advanced Opt-Out and the displayed salon-scoped
  inbound URL. The inbound/status callbacks require exact signatures plus the
  stored Account SID and Messaging Service SID (or exact sender `To`) binding;
  shared auth tokens do not authorize cross-salon routing. Do not infer consent
  from caller ID or webhook `Body`.
- Run and monitor the separate `customer_notification_delivery` worker job and
  `customer_notifications` queue. Treat queued, accepted, and sent as pending
  states, not delivery proof. Unknown post-dispatch outcomes and stale
  request/appointment copy are fail-closed; requeue only when the owner API
  returns `can_requeue=true`.
- Configure realtime phone mode from the Platform tenant Technical tab: Twilio `voice_transport=realtime_stream`; OpenAI realtime model/voice and the location-neutral background-noise handling policy (`automatic` is the default); and `speech_output_mode=streaming_tts` for low-latency backend-approved output. `buffered_realtime` is a legacy rollback mode.
- Keep Twilio and OpenAI secrets out of logs and docs; dashboard responses expose only configured/source metadata.
- Enable OpenAI voice AI in the Platform tenant Technical tab only when external AI voice turns should be enabled.
- Keep OpenAI model and voice settings configurable through the dashboard so model changes do not require code changes.
- Restrict CORS to the deployed admin, landing, and POS calendar origins.

### V39 booking-attempt preflight

Run this read-only query against the pre-V39 database during an approved change
window, after taking the normal deployment backup. It intentionally uses only
columns that exist before V39; `superseded_at` is created by V39 and must not be
referenced by the preflight.

```sql
SELECT
    salon_id,
    operation_type,
    request_fingerprint,
    count(*) FILTER (WHERE provider_outcome <> 'not_started') AS dispatched_or_uncertain_count,
    array_agg(id ORDER BY created_at, id) AS attempt_ids,
    array_agg(status ORDER BY created_at, id) AS statuses,
    array_agg(provider_outcome ORDER BY created_at, id) AS provider_outcomes,
    array_agg(COALESCE(pos_booking_id, '') ORDER BY created_at, id) AS provider_booking_ids
FROM booking_attempts
WHERE request_fingerprint IS NOT NULL
  AND (
      status = 'pos_pending'
      OR reconciliation_status = 'required'
  )
GROUP BY salon_id, operation_type, request_fingerprint
HAVING count(*) FILTER (WHERE provider_outcome <> 'not_started') > 1
ORDER BY salon_id, operation_type, request_fingerprint;
```

Zero rows means the V39 duplicate-safety guard has no known ambiguous group to
block. Any returned row blocks the release. V39 cannot create its reconciliation
tables until the migration transaction commits, so there is no in-product V39
resolution workflow available for this preflight state. For each returned
attempt, verify the authoritative POS booking outcome, provider booking ID and
version, requested time, customer, and idempotency evidence under the normal
provider-access and privacy controls. Escalate required corrections through a
peer-reviewed DBA/product reconciliation change that keeps the booking attempt,
appointment, provider identity/version, notification, and audit records
consistent in one transaction. Do not delete, merge, auto-close, or mark an
unknown/dispatched attempt as `not_started` merely to make the query or index
pass. Rerun the read-only query only after authoritative reconciliation proves
that each fingerprint group has at most one dispatched or uncertain attempt.
