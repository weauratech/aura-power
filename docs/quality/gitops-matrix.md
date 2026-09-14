# GitOps compatibility matrix

The authority question is evaluated separately for diff detection, sync,
runtime health, and ownership of `spec.replicas` or `spec.suspend`.

| Tracking and sync mode | Expected contract | Evidence | Status |
|---|---|---|---|
| Argo annotation tracking, self-heal off | Aura may change an opted-in fixture; an explicit sync restores undelegated fields | pinned Argo CD v2.14.20 Kind journey; Deployment, StatefulSet and CronJob are powered down/suspended and then restored by manual sync | pass-real |
| Argo annotation tracking, self-heal on | Without an agreed field owner, Aura performs one transition per target, records one deterministic audit per target, and settles in `Contended` without replaying mutations | candidate controller plus native Argo self-heal for Deployment, StatefulSet and CronJob; distinct and stable audit IDs, stable attempt timestamps, restored Argo-owned fields and `Contended` phases are observed for an additional 45 seconds | pass-real |
| `ignoreDifferences` only | Difference is hidden from comparison, but a sync caused by another Git change still writes replicas | pinned Argo CD v2.14.20 Kind journey | pass-real; confirmed unsafe for Aura ownership |
| `ignoreDifferences` plus `RespectIgnoreDifferences=true` | Existing resource leaves ignored replicas/suspend to Aura while other Git changes sync | native Deployment, StatefulSet and CronJob plus an unrelated Git revision | pass-real |
| Standard tracking label | Target ownership must be detected and require explicit opt-in | domain tests | partial |
| Custom tracking label | Configured label must be consumed by discovery | no configurable implementation found | unsupported |
| ApplicationSet-generated Application | Same field-authority rules apply to generated apps | generated Application, self-heal, unrelated Git revision and exact restore for Deployment, StatefulSet and CronJob in Kind | pass-real |
| HPA (`autoscaling/v2`) | Exact Deployment/StatefulSet `scaleTargetRef` produces persisted `OwnershipHPA`; an uncached mutation-boundary LIST plus UID-bound workload and Namespace GETs close HPA-creation and opt-in-removal races and fail closed on API errors; explicit opt-in permits snapshot/off/restore; standard HPA holds a zero target; a controlled external scale becomes stable `Contended` without a write loop | native Kind HPA journey plus discovery, guardrail, creation-race, opt-in-removal, LIST-failure and GET-failure tests | pass-real for standard HPA detection, zero hold and recovery; activation-from-zero autoscalers are not claimed |
| KEDA | Autoscaler ownership and scale-to-zero behavior must be explicit | no first-class discovery or native journey | unsupported/inconclusive |
| Flux | Ownership detection exists but no full reconciliation contract | unit signal only | pass-simulated only |

`ignoreDifferences` affects comparison. Argo CD requires
`RespectIgnoreDifferences=true` to apply the same rule during sync, and this
pre-patch behavior applies only once the live resource exists. The acceptance
gate therefore requires observing both the Aura transition and a later,
unrelated Git change on the same existing resource.

The automated journey installs Argo CD v2.14.20 from an integrity-checked
manifest and uses a Git repository served only inside the disposable cluster.
It proves manual sync for all three supported workload kinds and real automated
self-heal without field delegation. For each Deployment, StatefulSet and
CronJob under self-heal, the gate requires exactly one successful Aura mutation
audit followed by a stable `Contended` action. The three audit IDs must be
distinct; each attempt timestamp, audit ID, audit count and Argo-owned field
must remain unchanged during the stability interval. It also
proves the unsafe `ignoreDifferences` only mode, complete field delegation,
unrelated Git metadata delivery, and an ApplicationSet-generated Application
covering Deployment, StatefulSet and CronJob.

For CronJob, the journey captures a pre-existing Job's name and UID before
suspension and verifies that exact identity during suspension and after resume.
It records the set of Job UIDs once `suspend=true` is observable, crosses a
calculated minute boundary without accepting a new Job, and only accepts the
resume catch-up when a new Job carries
`batch.kubernetes.io/cronjob-scheduled-timestamp` earlier than the recorded
resume time. A normal tick after resume cannot satisfy this assertion. Cleanup
removes every fixture, Argo namespace and cluster-scoped Argo resource.

This matrix establishes the supported annotation-tracking contract on the
pinned version. Other Argo CD versions and label/custom tracking modes remain
separate compatibility claims.

The HPA journey creates real `autoscaling/v2` resources for a Deployment and a
StatefulSet, binds ownership by exact `scaleTargetRef`, and proves zero Aura
mutations before opt-in. For the opted-in Deployment it proves snapshot capture,
one audited scale-to-zero, the native HPA zero-replica hold, exact restoration,
and native HPA conditions. A separately identified external scale write proves
stable `Contended` handling without replay; it is not attributed to HPA. The
journey does not claim KEDA, metrics-backed scaling behavior, activation from
zero, or custom scale targets.
