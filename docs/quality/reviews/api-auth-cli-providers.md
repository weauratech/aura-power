# API, authentication, CLI and provider review

Reviewed baseline: `4727bc56f1442ca48d0e27bfe734d9a4b94cc1a8`.

This review follows behavior across the HTTP boundary, persisted authentication
state, CLI calls, metrics/cost providers, and notification webhooks. A passing
unit test means the dependency is simulated. Tests carrying the `acceptance`
build tag encode required product behavior and intentionally fail while the
corresponding verified gap remains open.

## Coverage manifest

| Subsystem | Files reviewed | Executable evidence |
|---|---|---|
| API routing and middleware | `server.go`, `auth_middleware.go`, `metrics.go` | `contracts_test.go` |
| API resources and reports | `handlers.go`, `metrics_handlers.go` | `contracts_test.go`, `acceptance_test.go` |
| Authentication and approval | `auth_handlers.go`, `auth/jwt.go`, `auth/store.go`, `auth/sqlite_store.go` | `auth/store_test.go`, API contract and acceptance tests |
| CLI | all files in `internal/cli`, `cmd/aura-power/main.go` | `httpclient_test.go`, CLI acceptance tests |
| Notifications | `notifications/dispatcher.go`, `notifications/senders.go` | `senders_test.go`, notification acceptance test |
| Prometheus | `prometheus/client.go`, API metrics handlers | `prometheus/client_test.go` |
| OpenCost | `opencost/client.go`, API cost handlers | `opencost/client_test.go` |
| Server composition | `cmd/server/main.go` | source trace plus package tests; real-cluster validation belongs to the Kind/EKS layer |

Generated CRD types were treated as contracts and traced to their source type
definitions. Security-sensitive authentication findings and their raw test
output are retained in the restricted campaign evidence directory, following
`SECURITY.md`; this public review does not contain exploit details.

## Verified functional gaps

| ID | Priority | Contract | Reproduction |
|---|---:|---|---|
| AP-API-001 | P1 | Approving a pending change applies the represented create/update/delete operation before reporting success. | `go test -tags=acceptance ./internal/adapters/driving/api -run TestAcceptanceApprovedPolicyIsApplied` returns 200 but finds no created policy. |
| AP-API-002 | P2 | User roles remain within the public `member`, `approver`, `admin` enum on create and update. | `TestAcceptanceRoleChangeRejectsUnsupportedRole` persists `owner` and returns 200. |
| AP-API-003 | P1 | Explain identifies a workload by namespace, name, and kind. | `TestAcceptanceExplainDisambiguatesWorkloadKind` asks for a StatefulSet and receives the homonymous Deployment. |
| AP-CLI-001 | P2 | `preview -f` accepts the YAML input documented by the CLI. | `TestAcceptancePreviewConvertsYAMLToJSON` observes YAML bytes under `application/json` and HTTP 400. |
| AP-CLI-002 | P1 | Automatic token refresh preserves a mutating request body exactly. | `TestAcceptanceRequestBodySurvivesTokenRefresh` observes an empty body on the retry. |
| AP-CLI-003 | P2 | Credentials containing valid JSON-special characters reach the login API unchanged. | `TestAcceptanceLoginSerializesCredentialsAsJSON` receives malformed JSON because the payload is assembled by string interpolation. |
| AP-NOTIFY-001 | P2 | Cancelling webhook delivery stops attempts and backoff promptly. | `TestAcceptanceCancelledDeliveryStopsWithoutBackoff` takes about five seconds after cancellation. |
| AP-NOTIFY-002 | P2 | A channel configured with the public `urlFrom` contract resolves and uses its Secret. | `TestAcceptanceNotificationResolvesSecretURL` observes zero deliveries from an enabled secret-backed channel. |
| AP-NOTIFY-003 | P2 | Batch deduplication preserves kind as part of workload identity. | `TestAcceptanceNotificationKeepsHomonymousKinds` observes that Deployment and StatefulSet with the same namespace/name are represented as one workload. |

These failures are deterministic and require no network or cluster. They should
remain visible in the campaign report and be converted into regression tests
without the build tag when product fixes are implemented.

## Additional review findings requiring focused reproduction

- Policy and override preview evaluate namespaces only. Workload names, labels,
  namespace labels, and groups present in the public scope type are ignored, so
  preview can disagree with selection and execution.
- Status and dashboard ignore errors when listing policies, overrides, and audit
  events. A partial dependency failure can therefore be serialized as a valid
  zero or incomplete result.
- Notification throttle state is recorded before delivery. A failed send can
  suppress a later retry for the whole throttle interval, and batch keys use the
  first action rather than the full workload identity.
- Human-readable CLI rendering uses unchecked numeric type assertions. A
  compatible API response with a missing or differently typed numeric field can
  terminate the process instead of returning a diagnostic.
- The HTTP surface lacks update operations for overrides and namespace groups,
  despite those resources being presented as manageable objects. Public API and
  UI documentation should state immutability or expose consistent update paths.

## Passing contracts added

- SQLite user lifecycle, bcrypt storage, persistence after reopen, deletion, and
  terminal approval decisions.
- JWT configured lifetimes, signature validation, expiry, and malformed-token
  rejection.
- HTTP authentication, cookie flags, refresh, logout cookie clearing, complete
  role matrix, readiness dependencies, malformed JSON, filters, and preview
  non-persistence.
- CLI config round trip with mode `0600`, URL normalization, bearer propagation,
  request payload transmission, local input validation, and the HTTP contract of
  every implemented read command plus override creation.
- Prometheus request encoding, authentication, sample parsing, and provider error
  propagation.
- OpenCost availability, allocation aggregation, workload lookup, and malformed
  or non-2xx responses.
- Generic webhook schema, target identity, success handling, transient retry,
  channel filters, status persistence, throttling, failure accounting, and exact
  event deduplication.

## Recommended implementation sequence

1. Fix approval execution and make acknowledgement mean that the requested
   Kubernetes mutation completed or failed explicitly.
2. Introduce a typed target identity shared by API, CRD, CLI, and frontend, with
   `kind`, namespace, name, and UID where available.
3. Centralize scope matching so preview and execution invoke the same evaluator.
4. Make the CLI buffer request bytes before refresh and serialize all JSON with
   `encoding/json`; parse YAML into the same API request type.
5. Complete secret-backed notifications, then key throttle/deduplication by the
   full event identity and update throttle state only after success.
6. Replace partial-success responses with explicit dependency status or fail the
   request when required reads fail.

Each change should promote its associated acceptance test into the default suite
and add a real API/Kubernetes integration scenario before closing the issue.
