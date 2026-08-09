# Owner Notification Delivery

This runbook covers owner-operational SMS created from durable
`owner_notifications` rows. It does not enable customer appointment messaging,
marketing messages, or customer consent management. A scheduling request is
still pending owner review even when its owner notification is delivered.

## Runtime Ownership And Configuration

- `backend/modules/notification_delivery` owns provider-neutral claims, leases,
  attempts, immutable events, status progression, replay, and dead-letter
  policy.
- `backend/modules/notification_twilio` owns Twilio Messaging REST dispatch and
  signed status/inbound callbacks.
- The worker runs the stable `notification_delivery` job every 15 seconds with
  a maximum claim batch of 20. V57 records only safe job health evidence.
- Active Twilio messaging configuration is salon-scoped data in
  `salon_integration_configs`, managed in
  `/platform/tenants/:tenant_id/technical`. The strict
  messaging resolver does not use environment fallback.

Enabling owner SMS requires all of the following dashboard-managed evidence:

- explicit **Owner operational SMS** enablement;
- an owner destination in E.164 form and explicit consent attestation for that
  exact destination;
- encrypted Twilio Account SID and Auth Token;
- either a Messaging Service SID or a sender phone number; and
- a public HTTPS base URL plus notification status and inbound callback paths.

The API returns only configuration flags, masked owner destination, and the
computed callback URLs. Account SID, Auth Token, Messaging Service SID, sender
number, full owner destination, message body, and provider message ID are not
returned by delivery-management endpoints.

Changing the destination requires fresh explicit consent attestation. Existing
queued rows from before V56 are disabled during migration because historical
consent must not be inferred. Configuration transfer excludes notification
delivery records and provider secrets.

## Twilio Callback Setup

Configure the displayed notification status URL as the message status callback
and the displayed inbound URL for the Messaging Service. Both are public POST
endpoints:

- `/api/notifications/twilio/status`
- `/api/notifications/twilio/inbound/:salon_id`

Both endpoints verify `X-Twilio-Signature` against the exact public URL and all
received form parameters. Proxy or Caddy configuration must preserve the public
scheme, host, path, and parameters used to calculate the signature. Do not
filter unknown form fields before verification; Twilio may add webhook
parameters over time.

Twilio documents message creation and the difference between an accepted
`queued` state and later delivery states in its
[Messages resource](https://www.twilio.com/docs/messaging/api/message-resource).
Its [webhook security guide](https://www.twilio.com/docs/usage/webhooks/webhooks-security)
defines exact-URL/form signature validation and recommends accommodating new
parameters.

The signed inbound route deliberately performs no keyword parsing or product
state mutation. Configure Twilio Messaging Service Advanced Opt-Out for the
number/service and verify its STOP/START/HELP behavior operationally. Twilio's
[Advanced Opt-Out guide](https://www.twilio.com/docs/messaging/tutorials/advanced-opt-out)
describes the provider-managed opt-out behavior and `OptOutType` callback
field. This slice does not implement customer messaging.

## Status And Retry Policy

`queued` and `delivering` are internal work states. Twilio `queued`, `accepted`,
or `scheduled` becomes `provider_accepted`; it is not proof of delivery.
`sending`, `sent`, and `delivered` advance monotonically. Signed callbacks are
deduplicated and an older/lower-rank callback cannot downgrade a later state.

Only failures proven to occur before provider dispatch use bounded automatic
retry, with at most five delivery attempts and bounded exponential delay.
V90 makes the database claim itself single-winner: tenant-fair ranking remains
bounded and lock-skipping, but the live row must still be queued/failed, due,
and below its attempt limit both when it is locked and in the atomic update.
The customer-notification queue uses the same rule, including `quiet_hours` as
an eligible due state. A stale ranking snapshot cannot create a second claim,
attempt, or provider-dispatch opportunity.
Lease expiry before `dispatch_started_at` is safe to reclaim. Lease expiry,
network failure, or response parsing failure after dispatch may mean Twilio
accepted the message; these cases move to `dead_letter` with
`DELIVERY_OUTCOME_UNKNOWN` and are never automatically retried.

A definitive terminal provider rejection also dead-letters the row with a safe
error code. A disabled salon configuration moves the row to `disabled`.
Requeue is owner-authenticated, action-key idempotent, and permitted only for a
safe dead letter. Unknown-outcome rows cannot be requeued because that could
send a duplicate SMS.

## Owner Operations

Use `/dashboard/appointments` to inspect the owner-notification delivery card.
The list and detail views expose masked destination, safe status/error code,
attempt count, and immutable delivery events. They do not expose message body,
full phone number, provider message ID, secrets, or raw provider responses.

Use `/dashboard/settings` or
`GET /api/salons/:id/operations/status` to monitor worker heartbeat, backlog,
oldest work, and dead-letter count. The status endpoint is observational; it
does not requeue or mutate work. Configure an external monitor/on-call process
against that evidence before claiming proactive production monitoring.

For a dead letter:

1. Check the safe error code and current Integrations configuration.
2. For `DELIVERY_OUTCOME_UNKNOWN`, verify the message outcome in Twilio using
   authorized provider-console access; do not requeue it from ManleAI.
3. For a safe definitive failure, correct the configuration or provider issue,
   then use the delivery detail's requeue action. Exact action replay returns
   the same result; changed reuse conflicts.
4. Confirm the worker job becomes healthy and the row advances through a new
   attempt. Treat `provider_accepted` or `sent` as non-delivered until a signed
   terminal delivery callback is recorded.

Do not paste full phone numbers, SMS bodies, provider SIDs, auth tokens, or raw
provider responses into logs, tickets, screenshots, or incident reports.
