# Operations Health And Worker Heartbeats

## Scope

Phase 6B persists safe technical evidence for recurring background jobs and
shows an authenticated salon owner only that salon's queue totals. It is a
diagnostic surface, not booking confirmation evidence, provider-delivery
proof, an alerting service, or permission to replay work.

The source of truth is V57:

- `worker_job_heartbeats` owns the latest global technical state for one stable
  job name.
- `worker_job_runs` owns the bounded audit history for each claimed run.
- `backend/modules/operations_health` owns distributed run fencing, safe status
  classification, tenant-scoped queue aggregation, and the owner API.
- `backend/cmd/worker/scheduler.go` owns start, recurring heartbeat, panic and
  cancellation capture, and terminal finish recording for every registered
  recurring job.

Neither table stores raw errors, request or webhook payloads, provider entity
IDs, customer data, secrets, or destination details. The owner response omits
worker instance IDs and run IDs and exposes only bounded counts, timestamps,
safe error class/code, and owner-workflow links.

## Stable Jobs

The worker currently registers `pos_sync_jobs`, `booking_lease_recovery`,
`availability_quote_cleanup`, `square_booking_webhooks`,
`square_calendar_repair`, `conversation_retention`, and
`notification_delivery`, `customer_notification_delivery`,
`scheduling_pii_retention`, and `openai_runtime_verification`. A newly
registered future job automatically gets a
ledger row and appears under its stable technical name; owner copy and a safe
workflow link should be added to the operations-health display policy when the
job becomes a supported product operation.

Each process has a generated worker instance ID. `StartRun` locks the current
job row and refuses a second claim while its lease is live. An expired claim is
closed as `abandoned` with `JOB_LEASE_EXPIRED` before a new run is installed.
Heartbeats and finishes require the exact job, run, and worker instance; an old
replica cannot complete a replacement run. A terminal run also retains its
configured interval as a short cadence fence, so another replica cannot run the
same job again immediately after a fast completion. Terminal run rows are
immutable.

The scheduler heartbeats every 15 seconds with a 90-second lease. Job health is
stale after the larger of two minutes or three configured intervals, capped at
30 days. Persisted processed counts are bounded to 1,000,000. Cancellation,
deadline, dependency failure, panic, and expired lease use safe codes rather
than raw error text.

## Owner Status API

`GET /api/salons/:id/operations/status` requires authentication and an exact
salon-owner match. Global worker rows are combined with only the requested
salon's aggregate backlog, oldest timestamp, and dead-letter count for owner
notification delivery, customer notifications, owner-review requests,
availability-quote cleanup, conversation retention, V61 scheduling-PII
retention, and, when external-provider history is relevant, V86
`external_slot_claims_pre_dispatch` and `external_slot_claims_unknown`; when
Square history is relevant it also includes POS sync, booking lease recovery,
Square webhooks, and calendar repair. The scheduling-retention row is
only a due-record count and oldest due timestamp for that salon; it never
contains message, destination, request, event-payload, or audio content.

Provider-specific rows are omitted when the salon has no relevant Square
connection. Internal-calendar failures are never queried from or written to
`pos_errors`. Notification metrics fail closed with
`NOTIFICATION_METRICS_UNAVAILABLE` if the V56 delivery columns are absent;
other unavailable aggregates return `QUEUE_METRICS_UNAVAILABLE` without
leaking a SQL or provider message.

Status meanings:

- `healthy`: a recent successful run or an empty/young salon queue.
- `running`: an actively heartbeating run with a live lease.
- `degraded`: a recent failed/cancelled/abandoned run, a dead letter, or a
  backlog older than the queue's stable grace threshold.
- `stale`: the heartbeat/success evidence is older than its recorded threshold
  or a running lease expired.
- `unknown`: the job has never recorded evidence or its safe queue metric could
  not be read.

The aggregate uses fail-closed precedence: `unknown`, then `stale`, then
`degraded`. The Settings card supports refresh and navigation only. Delivery
requeue remains in the owner-notification workflow and no replay mutation is
added here.

## Deployment And Incident Use

Code-ready means V57, the scheduler wrapper, the API, and the Settings component
are present and tested. Configured monitoring additionally requires:

1. the API release to apply V57 successfully;
2. at least one worker replica running the same release;
3. owner authentication and tenant-scoped API access working; and
4. an external monitor or on-call process polling/evaluating the endpoint if
   proactive paging is required.

The product does not currently send automatic operations-health alerts. A
green Settings card proves only recent persisted technical evidence; it does
not prove Square/Twilio delivery, appointment confirmation, or provider-console
health.

For an incident, first verify the deployed API and worker versions, migration
records for V57, V86, and V87, and worker process health. Then use the safe job code and the
owner-scoped queue row to select the linked workflow. Inspect provider state
only through the dashboard-backed integration configuration and relevant
readiness/debug endpoints. Never copy raw payloads, credentials, provider IDs,
or customer data into the health ledger or owner response.

For `external_slot_claims_pre_dispatch`, correlate the claim with its booking
attempt and persisted `provider_outcome`. Lease recovery may release the claim
only when dispatch is proven `not_started`; age alone is not proof. For
`external_slot_claims_unknown`, inspect the existing reconciliation workflow
and exact authoritative provider candidate. Never blind-retry, force-release,
or create a replacement booking while dispatch-started, unknown, or
reconciliation-required evidence still owns the interval. A verified
`not_created` resolution releases the claim; an exact provider attachment or
calendar convergence confirms it. If exact evidence is unavailable, retain the
claim and escalate rather than reopening the time.

For a buyer-write Square pilot, monitor `SLOT_COMMIT_CONFLICT` volume, oldest
pre-dispatch claim age, `dispatched_unknown` count/age, reconciliation backlog
and age, database-pool wait, and claim commit latency. The first four are
correctness/incident signals; pool wait and claim latency are saturation
signals. A retained unknown claim is expected safe behavior, while blind
release, a conflict-loser provider dispatch, or a duplicate confirmation is a
safety incident. Local `scheduling-load-report/v2` evidence verifies only its
isolated synthetic run and is not production capacity evidence.
