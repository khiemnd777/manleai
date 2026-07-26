# PostgreSQL Backup And Isolated Restore Drills

This runbook owns database backup verification and non-production restore
drills for the Compose-managed PostgreSQL service. It never authorizes an
automatic production restore. Migration release and image rollback decisions
are covered separately in [Migration Rollback](migration-rollback.md).

## Safety Boundary

- Every command names the source database and target database explicitly.
- Restore targets must be new databases whose names begin with
  `manleai_restore_drill_`.
- Source and target must differ. `postgres`, `template0`, and `template1` are
  not valid source or restore targets.
- The restore script refuses an existing target. It never drops, truncates,
  renames, or overwrites any database.
- A drill runs only on the protected non-production host configured for the
  `postgres-restore-drill` GitHub environment. Do not point its secrets at the
  production VPS.
- The database archive remains on the private host. GitHub receives only the
  sanitized drill report, never the dump or `project.env`.
- Do not start the worker, route public traffic, deliver webhooks, or invoke
  provider operations against a restored drill database.

The scripts require the caller to pass
`--storage-class encrypted-private`. This is an explicit operator attestation,
not encryption software. The artifact directory must reside on an approved
encrypted volume or encrypted object-storage mount, be accessible only to the
database operations identity, and have backup retention and key rotation
managed outside the repository. Directories are created with mode `700`; dump,
checksum, and report files use mode `600`. A checksum detects corruption but
does not encrypt data.

If an artifact leaves that encrypted private boundary, encrypt it first with
the organization's approved envelope/KMS workflow. Never place a passphrase,
private key, database URL, provider token, or decrypted artifact in a shell
argument, GitHub artifact, issue, chat, or drill report.

## Backup Contract

`deploy/postgres-backup.sh`:

1. validates the explicit source identity and private storage attestation;
2. confirms the source exists in the running Compose PostgreSQL service;
3. creates a custom-format `pg_dump` with no owner or ACL replay;
4. runs `pg_restore --list` against the artifact;
5. writes an SHA-256 sidecar naming exactly that artifact; and
6. atomically publishes the mode-`600` dump and checksum, then emits their
   paths, checksum, and UTC completion timestamp for the bounded backup record.

It refuses an existing artifact ID so a repeated command cannot overwrite an
earlier recovery point.

Example on an approved host:

```bash
deploy/postgres-backup.sh \
  --env-file /opt/manleai/project.env \
  --compose-file /opt/manleai/current/docker-compose.prod.yml \
  --project manleai \
  --source-db ai_receptionist \
  --db-user ai_receptionist \
  --output-dir /opt/manleai/backups \
  --artifact-id predeploy-v2026.07.24.1 \
  --storage-class encrypted-private
```

Database names, user names, artifact IDs, paths, RPO/RTO targets, and release
references are dynamic operator inputs. The names above are examples, not
runtime defaults.

## Manual Non-Production Drill

The preferred entry point is
`.github/workflows/postgres-restore-drill.yml`, started with
`workflow_dispatch` after approval. Its protected GitHub environment requires
dedicated non-production SSH secrets:

```txt
DRILL_SERVER_IP
DRILL_REMOTE_USER
DRILL_SSH_PRIVATE_KEY
```

The protected environment variable
`DRILL_BACKUP_STORAGE_APPROVAL` must be exactly
`encrypted-private:/opt/manleai-restore-drill/backups`. It attests the concrete
private path after an operator has verified the host storage; it does not
provide encryption.

The dedicated host must also have a regular, non-symlink attestation file at
`/opt/manleai-restore-drill/NON_PRODUCTION_RESTORE_DRILL` whose complete
contents are `manleai-non-production-restore-drill-v1`. The workflow fails
before backup or restore if that host-side non-production fence is absent or
changed.

Required inputs are:

- explicit source and new isolated target database names;
- a unique, non-sensitive artifact ID;
- a change/drill approval reference;
- RPO and RTO targets in seconds.

The workflow creates a fresh backup, verifies its checksum and archive catalog,
restores it into the explicit isolated target, validates the restored schema and
tenant fences, and uploads only the sanitized report. The dump, checksum,
restored database, and private environment file remain on the non-production
host. The workflow deliberately leaves the restored target in place for the API
startup handoff and human review.

For a direct approved non-production host drill, create the backup first, then
run:

```bash
deploy/postgres-restore-drill.sh \
  --env-file /opt/manleai/project.env \
  --compose-file /opt/manleai/current/docker-compose.prod.yml \
  --project manleai \
  --source-db ai_receptionist \
  --target-db manleai_restore_drill_20260724_01 \
  --db-user ai_receptionist \
  --artifact /opt/manleai/backups/drill-20260724-01.dump \
  --checksum-file /opt/manleai/backups/drill-20260724-01.dump.sha256 \
  --artifact-id drill-20260724-01 \
  --migrations-dir /opt/manleai/current/backend/migrations \
  --report-file /opt/manleai/restore-drills/drill-20260724-01.report \
  --approver approved-operator \
  --approval-reference approved-change-reference \
  --release-ref v2026.07.24.1 \
  --rpo-target-seconds 3600 \
  --rto-target-seconds 1800 \
  --drill-start-epoch 1784822400 \
  --storage-class encrypted-private
```

Use the migration directory bundled with the exact application image/release
being validated. Do not use a working tree whose migration set differs from the
database snapshot.

## Restore Validation

The restore and `deploy/postgres-verify-restore.sh` must all pass:

- checksum sidecar matches the selected artifact;
- `pg_restore --list` can parse the custom archive;
- the target is a newly created isolated database;
- every release `V*.sql` file maps to exactly one
  `app_schema_migrations` row with the same version, normalized name, and
  SHA-256 checksum, with no missing or extra rows;
- critical scheduling, integration-config, conversation, and internal-calendar
  tables exist;
- required tenant, staff-overlap, release-owner, and lifecycle event-version
  constraints exist;
- bounded smoke queries find no cross-salon appointment/attempt,
  appointment/service, scheduling-request/call-session, or orphaned integration
  configuration relationships.

The report's `api_startup_handoff` becomes ready only after those checks and
the declared RPO/RTO objectives pass. An objective failure keeps the handoff
blocked even when schema validation succeeded.
The next step is a separate operator-controlled health check using the exact
release image, an isolated `DATABASE_URL` that names the restored target,
`AUTO_MIGRATE=false`, a non-public bind port, and no worker. Check `/healthz`
and bounded owner-authenticated read paths without sending customer traffic or
provider operations. Record the health-check result alongside the report. The
restore workflow never repoints production services automatically.

## Drill Record

The sanitized report records:

- pass, failed, or failed-objective status and bounded failure stage;
- UTC drill timestamp;
- artifact ID and SHA-256 checksum;
- release reference and approval reference;
- approver identity;
- RPO target and observed artifact age at restore start;
- RTO target and restore-plus-validation duration;
- total drill duration; and
- API startup handoff status.

It must not contain database URLs, passwords, encryption keys, provider
configuration, secrets, transcripts, customer data, table rows, row counts, or
dump contents. Retain the report according to the operations audit policy; the
workflow's GitHub copy is limited to 30 days.

## Cleanup And Failure Handling

- A failed restore leaves the isolated target for diagnosis; it never falls
  back to deleting a database.
- Use a new target name for the next attempt.
- After review, a DBA may remove only the exact
  `manleai_restore_drill_...` target under a separate approved cleanup action.
  Verify the name, host, environment, absence of active sessions, artifact/report
  retention, and approval again immediately before that destructive action.
- Never adapt the cleanup step into a wildcard, prefix-wide deletion, scheduled
  production task, or failure trap.
- A failed backup, checksum, archive-list, migration-checksum, schema-object,
  tenant-smoke, API-handoff, RPO, or RTO check fails the drill and blocks its use
  as recovery evidence.
