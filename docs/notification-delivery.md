# Durable notification delivery

Aura Power records each eligible audit/channel pair as a
`PowerNotificationDelivery` before contacting a webhook. The delivery resource
contains the immutable audit payload, both Kubernetes UIDs, the selected policy,
and an idempotency key. It never contains the webhook URL or Secret data.

The notification controller reconciles these records separately from workload
actions. A full in-memory queue therefore cannot drop an audit handoff, and a
controller restart or leader change resumes non-terminal records. Terminal
records remain authoritative even after they leave the channel's bounded
`recentAttempts` display. Pending and in-progress records block audit cleanup.
Terminal identity and its owning `PowerAuditEvent` are retained for at least
`NOTIFICATION_DELIVERY_RETENTION_DAYS` (30 days by default, never shorter than
the audit retention setting).

Every webhook request includes:

```http
Idempotency-Key: aura-power/delivery-<stable digest>
```

Generic webhook payloads also expose the same correlation through their
delivery and audit references. Receivers should persist this key before applying
their side effect and return the same successful response for a replay.

## Delivery policies

`spec.deliveryPolicy: at-most-once` is the backward-compatible default. Aura
Power sends at most one provider request. If the controller loses the outcome
after the request starts, the record becomes `Ambiguous` and is not replayed.

`spec.deliveryPolicy: at-least-once` retries transient, rejected, and ambiguous
outcomes up to `spec.maxDeliveryAttempts` (default 5, maximum 20). Every retry
uses the same idempotency key. This policy avoids loss when a provider response
is unavailable, but duplicate effects remain possible unless the receiver
honors the key.

Neither policy claims exactly-once delivery. Exactly once requires the receiver
to atomically deduplicate the idempotency key with its own side effect.

Preflight failures, such as a temporarily unavailable Secret, channel, or API
server, remain pending with persistent exponential backoff and do not count as
provider requests. Status and logs contain only a
stable redacted classification; endpoint details and provider response bodies
are never persisted.

Channel throttling is also recovered from terminal delivery records. A pending
record with the same channel, action, result, rule, and target waits until the
configured throttle interval expires; restarting the controller cannot reset
that interval.

## Upgrade behavior

The new CRD must be installed before the candidate controller starts. Existing
channels that omit `deliveryPolicy` continue with `at-most-once` semantics.
Existing audit events are not backfilled because their previous delivery
outcome cannot be reconstructed safely. New audits carry an outbox marker; an
independent audit watcher materializes any handoff interrupted after the audit
write. The existing channel counters and 20-entry attempt
history remain available as an operational projection of terminal outbox
records.

Before downgrading to a controller that does not understand the outbox, stop new
workload actions and wait until every delivery is terminal:

```bash
kubectl get powernotificationdeliveries -n <control-namespace>
```

Do not delete pending or in-progress records to force a downgrade; doing so
discards the only durable delivery decision.
