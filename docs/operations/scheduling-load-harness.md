# Scheduling Load And Concurrency Harness

## Purpose And Evidence Boundary

`backend/cmd/scheduling-load-harness` is a bounded, synthetic verification
tool for Owner-first scheduling concurrency and persistence invariants. It
exercises production repositories and services for:

- `owner_manual` request creation, exact replay, changed-payload conflict,
  owner tenant fencing, and optimistic-version status transition races;
- `manleai_calendar` aggregate availability, exact party-create replay, atomic
  two-guest/two-staff pooled-resource commits, and concurrent over-capacity
  conflict handling; and
- scheduling-authority preview and commit replay plus concurrent commit CAS.

The external authority uses an in-process readiness fake at the authority
switch boundary. The harness does not initialize or call Square, Twilio,
OpenAI, another POS adapter, notification delivery, API, or worker runtime.

A passing report verifies the declared safety invariants for that exact
release, migration set, database, run ID, seed, concurrency, and operation
count. It is not production capacity proof. Production capacity may be claimed
only after a separately approved, witnessed run in a representative isolated
environment with an agreed workload and acceptance target.

## Mandatory Isolation Gates

The command fails before synthetic seeding unless all of these are true:

1. `--attestation` is exactly
   `NON_PRODUCTION_ISOLATED_SCHEDULING_LOAD`.
2. PostgreSQL `current_database()` exactly matches `--expected-database`, uses
   the lower-case dedicated prefix (default `manleai_load_`), and does not
   contain a reserved production/live name.
3. PostgreSQL `current_user` exactly matches `--expected-database-user`. Use a
   database role dedicated to the approved load environment; do not reuse a
   production application identity.
4. Every embedded release migration exists in `app_schema_migrations`, with
   the exact version and SHA-256 checksum, and no extra applied migration is
   present.
5. The run UUID has never been seeded in that database.
6. Concurrency, operations, and duration remain inside the compiled bounds:
   concurrency `2..64`, operations per workload `2..1000`, and duration
   `5s..10m`.

The harness never creates, drops, truncates, renames, or cleans a database and
contains no row-deletion path. Provision and migrate the isolated target with
the normal approved database workflow before invoking the command. Each run
creates unique synthetic users and salons and intentionally leaves its audit
evidence in place. Use a new UUID for every run; exact UUID reuse is rejected.

## Run Procedure

Provision a fresh non-production PostgreSQL database with a name such as
`manleai_load_release_candidate_01`, apply the candidate release migrations,
and grant the dedicated load role only the access approved for that isolated
database. Confirm source and target identity before continuing.

Provide the database URL through a task-scoped environment variable so it is
not present in shell history as a flag value. The report is JSON on stdout;
diagnostics are on stderr.

```bash
export SCHEDULING_LOAD_DATABASE_URL='<isolated database URL>'

cd backend
go run ./cmd/scheduling-load-harness \
  --expected-database manleai_load_release_candidate_01 \
  --expected-database-user manleai_load_runner \
  --database-prefix manleai_load_ \
  --attestation NON_PRODUCTION_ISOLATED_SCHEDULING_LOAD \
  --release vYYYY.MM.DD.N \
  --run-id '<new UUID>' \
  --seed 17 \
  --concurrency 8 \
  --operations 16 \
  --duration 2m \
  > scheduling-load-report.json
```

`SIGINT` and `SIGTERM` cancel the shared run context. Database work receives
the same deadline; cancellation or deadline expiry fails the report. Do not
raise bounds during an incident or use the harness against a shared database.

## Report Contract And Pass Gate

Reports use schema `scheduling-load-report/v1` and include:

- release, run UUID, stable seed, fixed synthetic clock, configured bounds,
  elapsed time, and synthetic salon UUIDs;
- exact database name/user, migration count, per-migration checksum, aggregate
  migration fingerprint, and final SQL pool statistics;
- per-workload attempts, successes, exact replays, expected conflicts,
  unexpected errors, throughput, and p50/p95/p99/max latency; and
- safety, tenant, idempotency, duplicate, orphan, provider-evidence, and
  provider-call violation counts. Totals label latency as the worst per-
  workload percentile rather than presenting it as a combined percentile.

`passed=true` requires zero unexpected workload errors and zero violations in
every invariant category. Expected CAS, stale-quote, and capacity conflicts are
reported separately and are not failures. Any tenant escape, duplicate
operation, incomplete graph/outbox/event, internal row with provider evidence,
or provider runtime call fails the run regardless of latency or throughput.

Synthetic names, phones, and `.invalid.example` emails are generated from the
run UUID. The report contains identifiers and counts, not customer data,
provider payloads, credentials, transcript text, or notification bodies.

## Fresh PostgreSQL Integration Mode

The opt-in integration test uses the same guard and runs twice with different
UUIDs, then proves exact UUID reuse is rejected. It may apply embedded
migrations only when the target is a newly provisioned isolated test database
and the migration attestation is set explicitly:

```bash
cd backend
SCHEDULING_LOAD_FRESH_DATABASE_URL='<fresh isolated database URL>' \
SCHEDULING_LOAD_FRESH_DATABASE_MIGRATE=NON_PRODUCTION_ISOLATED_SCHEDULING_LOAD \
go test -run TestRunAgainstFreshIsolatedPostgres -v ./internal/schedulingload
```

This test helper does not drop or clean the database. A checksum drift fails
closed; create a new isolated database for the current migration set instead
of rewriting applied migration history.

## Witnessed Release Evidence

For a production-readiness decision, record the approval reference, release,
database identity, run UUID, seed, bounds, environment shape, start/end time,
operator, witness, report checksum, and pass/fail decision. Store the sanitized
JSON report in the approved private release-evidence location. A repository
test pass or an unwitnessed local run closes implementation verification only;
it does not close the production capacity checklist item.
