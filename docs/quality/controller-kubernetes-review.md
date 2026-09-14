# Controller and Kubernetes quality review

Reviewed commit: `4727bc56f1442ca48d0e27bfe734d9a4b94cc1a8`

Review date: 2026-09-12

Scope: domain, ports, Kubernetes adapters, discovery/background jobs, reconcilers, admission handlers, CRD Go types, generated CRDs, controller wiring and controller RBAC.

## Contract under review

For a workload identified by cluster, API kind, namespace, name and UID, the controller must select the exact intended object, record its pre-mutation state durably, perform at most one effective transition, and restore that exact state. Preview, audit and notification output must describe the same decision and effect. A namespaced installation must not silently control resources belonging to another installation.

The ordinary `quality` tests encode behavior already satisfied by the implementation. Tests under the `acceptance` build tag encode product contracts that are currently violated. Acceptance failures are deliberate evidence and are kept out of the default green suite; they must be run and reported, never silently skipped or converted to approval.

## Verified findings

| ID | Priority | Contract violation | Reproduction |
|---|---:|---|---|
| CTRL-01 | P1 | Restoring an originally suspended CronJob always writes `suspend=false`, enabling work that was disabled before Aura Power acted. | `TestAcceptanceCTRL01CronJobRestorePreservesOriginallySuspended` |
| CTRL-02 | P2 | Resource estimation falls back to limits only when the entire requests map is nil. A container with only a CPU request and only a memory limit reports zero memory. | `TestAcceptanceCTRL02ResourceFallbackIsPerResource` |
| CTRL-03 | P1 | PowerTarget name is only `<namespace>--<name>`. Deployment, StatefulSet and CronJob homonyms collapse into one target whose original kind remains in `spec.targetRef`. | `TestAcceptanceCTRL03HomonymousKindsHaveDistinctPowerTargets` |
| CTRL-04 | P1 | `spec.scope.namespaceGroups` is advertised by the CRD but discarded by both policy and override conversion. A group-only rule becomes an empty scope, which matches the whole cluster. | `TestAcceptanceCTRL04PolicyNamespaceGroupsReachDomain` |
| CTRL-05 | P1 | Discovery observes workload and namespace labels, but PowerTarget has nowhere to persist them. Reconciliation evaluates selectors against PowerTarget controller labels and an empty namespace-label map, so label-based selection cannot represent the discovered target. | `TestAcceptanceCTRL05WorkloadAndNamespaceLabelsReachDecisionEngine` |
| CTRL-06 | P1 | Workload mutation happens before snapshot status persistence. A failed/conflicting status update is logged and returned as success with a requeue; the next reconcile can repeat power-down from stale observed state and replace the original replica snapshot. | `TestAcceptanceCTRL06StatusConflictCannotLoseSnapshotAndRepeatMutation` |
| CTRL-07 | P2 | The port and CRD use action `execution.error`; the notification allowlist checks `workload.execution_error`. Controller execution failures therefore never enqueue notifications. | `TestAcceptanceCTRL07ExecutionErrorsAreNotifiable` |
| CTRL-08 | P2 | Audit labels and target filtering omit workload kind. Audit queries for a Deployment include homonymous StatefulSet/CronJob events. | `TestAcceptanceCTRL08AuditTargetFilterIncludesKind` |
| CTRL-13 | P2 | Override preview passes current policies as both current and hypothetical policies, so a state change introduced by the override can be reported only as a conflict and `totalAffected=0`. | `TestAcceptanceCTRL13PreviewOverrideReportsChangedTarget` |
| CTRL-14 | P2 | A selector with an empty label value matches a target where the key is absent, because map lookup does not check key presence. | `TestAcceptanceCTRL14EmptyLabelValueRequiresKeyPresence` |
| CTRL-15 | P2 | Domain expiration uses `now.After(expiresAt)`, while the reconciler uses `expiresAt.Before(now)`. At the exact deadline the override is still treated as active. | `TestAcceptanceCTRL15OverrideExpiresAtExactDeadline` |

## Architectural findings requiring cluster-level confirmation

| ID | Priority | Finding and implication | Primary code evidence |
|---|---:|---|---|
| CTRL-A01 | P1 | The controller hardcodes `aura-system` for targets, audits and discovery. Policy/override list calls are cluster-wide and workload RBAC is cluster-wide. Helm release namespace and leader-election ID do not isolate a second installation's decisions or mutations. | `cmd/controller/main.go`, `TargetReconciler.loadPolicies`, `TargetReconciler.loadOverrides`, chart controller RBAC |
| CTRL-A02 | P1 | Workload identity contains no Kubernetes UID or cluster identity. Delete/recreate can inherit the prior PowerTarget and snapshot, and multi-cluster semantics cannot be represented. | `domain.WorkloadRef`, `TargetReference`, discovery target naming |
| CTRL-A03 | P1 | Resolved for `autoscaling/v2` HPAs targeting Deployments/StatefulSets: discovery indexes exact `scaleTargetRef` identities and persists `OwnershipHPA`; an uncached mutation-boundary LIST and UID-bound workload/Namespace reads recompute current opt-in and fail closed on creation, opt-in-removal, UID and read-error races; targets block without opt-in and have a native Kind coexistence journey. KEDA and custom scale targets remain unsupported. | `domain.OwnershipHPA`, `Discoverer`, `TargetReconciler.admitLiveHPA`, `kind-hpa-journey.sh` |
| CTRL-A04 | P1 | Validator handlers exist, but the manager does not register a webhook server path and the chart contains no ValidatingWebhookConfiguration/certificate resources. Runtime admission therefore relies only on CRD OpenAPI validation and cannot enforce timezone, future expiry or richer rules. | `cmd/controller/main.go`, `internal/adapters/driving/webhook`, chart templates |
| CTRL-A05 | P2 | Policy and override reconcilers do not enqueue affected PowerTargets. Changes take effect only through each target's periodic requeue; deletion follows the same delayed path. | reconciler setup methods |
| CTRL-A06 | P2 | Status and audit errors are commonly logged or discarded. A successful mutation can return a normal reconcile result despite missing durable status/audit evidence, creating false operational success. | target and policy reconcilers |
| CTRL-A07 | P2 | Discovery lists every namespace and every supported workload each minute. No cache index, selector or installation ownership bound limits target cardinality. This increases API load and reinforces cross-release interference. | Kubernetes discoverer and discovery loop |
| CTRL-A08 | P2 | Built-in `weekdays-only` ends at `23:59` while windows use an exclusive end, leaving the final minute powered off on weekdays. Built-in schedules are seeded once and later corrections are not reconciled. | `builtin_schedules.go`, `IsInWindow` |
| CTRL-A09 | P2 | Cleanup deletes every PowerTarget in the configured control namespace whose generated name is absent from the current discovery result. A partial-but-successful discovery cycle, naming collision or another controller's targets can be treated as orphaned. | `cleanupOrphans` |
| CTRL-A10 | P3 | Several public ports (`SnapshotStore`, repositories) have no controller implementation; snapshot durability is instead coupled to mutable target status. This makes transaction boundaries and recovery behavior implicit. | `internal/ports`, target reconciler |

## File review ledger

Every authored source file in this assigned scope was read. Generated deep-copy and generated CRD YAML were checked against their source types rather than reviewed as independent business logic.

| Subsystem | Files reviewed | Result |
|---|---|---|
| CRD API | `groupversion_info.go`, all `*_types.go` under `api/v1alpha1`, `zz_generated.deepcopy.go` | NamespaceGroups, identity, label persistence, schemas and status contracts assessed; generated artifacts checked for presence. |
| Domain | `engine.go`, `guardrails.go`, `preview.go`, `savings.go`, `scope.go`, `timewindow.go`, `types.go` plus existing unit/property tests | Priority, schedule, scope, preview, restore guardrail, empty/zero semantics and permutations exercised. |
| Ports | `audit.go`, `metrics.go`, `metrics_provider.go`, `repositories.go`, `snapshot.go`, `workload.go` | Interface meaning compared with adapters; unused durability/repository boundaries recorded. |
| Kubernetes driven adapters | `audit.go`, `cached_client.go`, `discovery.go`, `executor.go`, `helpers.go` | Real object field mutation, resources, discovery metadata, audit identity and cache consistency reviewed. |
| Background | `builtin_schedules.go`, `discovery.go` | Discovery breadth, target lifecycle, implicit schedules, deletion behavior and namespace handling reviewed. |
| Reconcilers | `target_reconciler.go`, `policy_reconciler.go`, `override_reconciler.go` | Decision-to-mutation-to-status ordering, failures, requeues, watch graph and conversions reviewed. |
| Admission | `policy_validator.go`, `override_validator.go` | Handler rules tested; deployment registration gap recorded. |
| Runtime/chart | `cmd/controller/main.go`, controller Deployment/Service/RBAC and CRD templates | Effective namespace, leader election, RBAC, webhook wiring and chart/runtime configuration compared. |

## Validation commands

Expected-green contracts:

```bash
go test -mod=readonly \
  ./internal/core/domain \
  ./internal/ports \
  ./internal/adapters/driven/kubernetes \
  ./internal/adapters/driving/background \
  ./internal/adapters/driving/reconciler \
  ./internal/adapters/driving/webhook \
  -race -count=1
```

Known-failing acceptance contracts:

```bash
go test -mod=readonly -tags=acceptance \
  ./internal/core/domain \
  ./internal/adapters/driven/kubernetes \
  ./internal/adapters/driving/background \
  ./internal/adapters/driving/reconciler \
  -count=1
```

The second command is successful only when the documented gaps are fixed. Current expected outcome is 11 named failures, CTRL-01 through CTRL-08 and CTRL-13 through CTRL-15.

## Refactoring direction

1. Define a versioned target identity containing cluster identity, Group/Kind, namespace, name and UID. Migrate PowerTarget names and audit labels without reusing snapshots across UIDs.
2. Resolve namespace groups before domain evaluation and persist an immutable discovery projection containing workload labels, namespace labels, annotations, ownership and observed generation.
3. Introduce a durable action state machine: planned, snapshot persisted, mutation issued, effect observed, restored. Use optimistic concurrency, a stable action ID and retry-safe transitions.
4. Establish field ownership with server-side apply or narrow patches. Keep snapshot until restoration is observed, and surface persistence/audit failure as a failed condition.
5. Replace global list-and-poll behavior with indexed watches from policies/overrides and explicit installation scope. Make control namespace, audit namespace and discovery selectors effective configuration.
6. Implement autoscaler discovery and a tested GitOps field-authority contract before relaxing guardrails. Register admission or move every safety-critical invariant into CRD CEL/OpenAPI validation.

No product behavior was changed by this review. The additions are tests and review documentation only.
