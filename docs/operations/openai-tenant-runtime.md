# OpenAI Tenant-Bound Runtime Operations

## Scope and invariants

V84 is an additive expand release for OpenAI Voice/AI. Active configuration
remains one encrypted `salon_integration_configs` row per salon/provider. The
runtime must receive a non-empty salon ID before resolution and must reject a
resolver result owned by another salon before any HTTP or WebSocket request.

The only production destination is the server-owned `openai_public` profile:
`https://api.openai.com/v1` for HTTP and the matching `wss` endpoint for
Realtime. Redirects, environment proxies, custom tenant Base URLs, loopback,
private, link-local, metadata, and unsafe DNS/dial results are denied.

`credential_fingerprint_hmac` is a non-reversible uniqueness identity derived
by application code using HMAC-SHA256 with a purpose-separated key. It is not a
provider-project identity, is never returned by API, and must not be selected
for operator evidence. The encrypted API key remains the only runtime
credential.

## Pre-deploy checks

1. Confirm release migrations through V83 are applied and repository migration
   checksums match the release artifact.
2. Confirm the API and worker use the same infrastructure encryption root.
   Do not print or inspect the value.
3. Inventory only safe OpenAI row metadata:

   ```sql
   SELECT salon_id, id AS integration_config_id, enabled, updated_at
   FROM salon_integration_configs
   WHERE provider = 'openai'
   ORDER BY salon_id;
   ```

4. Confirm CI and release self-tests have no live OpenAI calls. Unit and
   integration tests must use pinned fake transports.

## Expand rollout

1. Apply V84 with the normal migrator. Do not backfill plaintext-derived
   credential identity in SQL.
2. Deploy the API and worker together. The API constructs OpenAI only with the
   strict tenant resolver; the worker registers `openai_runtime_verification`.
3. For each tenant, open Platform tenant Technical > OpenAI and save the
   current model/voice settings plus that tenant's write-only API key. Saving
   canonicalizes the destination, creates credential revision 1 for a legacy
   row, and writes the uniqueness HMAC atomically with the encrypted secret and
   technical version.
4. If save returns `OPENAI_CREDENTIAL_TENANT_CONFLICT`, do not copy identity
   fields or disable the unique index. Rotate one tenant to a distinct API key,
   then save again.
5. Select **Verify current tenant runtime**. The POST only queues work. Refresh
   the same card until the latest run is terminal and inspect every required
   capability.
6. Treat the tenant as live verified only when the run is `succeeded`,
   `fresh=true`, and every required capability is `verified`. Realtime may be
   `not_required` only when Realtime is disabled in the saved configuration.
7. Place a bounded real call after Twilio routing is already live verified.
   Missing or failing OpenAI must leave Twilio ingress available through the
   recording/Gather safe path; it must not produce a cross-tenant provider
   request.

## Evidence and monitoring

`GET /api/platform/tenants/:tenant_id/technical/openai/runtime-verification`
is read-only and performs no provider request. Evidence is automatically stale
when config version, credential revision, destination-policy version, or
verification-contract version changes.

Operations Health exposes the `openai_runtime_verification` worker heartbeat
and tenant queue age. Investigation may inspect safe run/capability status,
latency, bounded provider request ID, and error code. Do not expose encrypted
secrets, the credential HMAC, provider response bodies, or raw error messages.

## Configuration Transfer

Platform configuration schema v10 transfers only portable OpenAI model, voice,
speech mode, Realtime toggle/model/voice/noise policy, and instructions. It
does not transfer enabled state, destination profile, credential identity,
encrypted key, credential revision, or verification evidence. A new target row
is disabled; an existing target preserves its own tenant identity and secret.
Any transferred model change advances the technical config fence, making old
verification evidence stale.

Schema v9 remains accepted as an input compatibility format but is normalized
to v10 ownership rules before preview/apply.

## Rollback

V84 is additive. During an expand rollback:

1. Stop the new worker before rolling API code back so no new verification jobs
   are claimed by a partially rolled deployment.
2. Before an older image receives traffic, disable OpenAI for every tenant and
   apply the incident egress control that blocks OpenAI HTTP/WSS dispatch. The
   previous image does not implement the V84 strict destination/resolver
   contract, so running it with OpenAI enabled is not an approved rollback.
   Keep Twilio ingress available through its independent recording/Gather path.
3. Roll the API/worker image back while leaving V84 tables and columns in place.
   The previous image uses explicit column lists and does not erase the added
   OpenAI identity columns on ordinary updates.
4. Do not drop the unique credential index or verification history during an
   incident.
5. Roll forward with the corrected image, remove the temporary egress block,
   re-enable and save each intended tenant config, restart the worker, and queue
   a new verification run. Do not relabel an old run as current.

A later contract release may remove legacy custom-destination/process config
code only after rollout evidence confirms every active OpenAI tenant has a
canonical destination profile, credential revision, and fresh successful
runtime verification.
