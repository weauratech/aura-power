# GitOps compatibility matrix

The authority question is evaluated separately for diff detection, sync,
runtime health, and ownership of `spec.replicas` or `spec.suspend`.

| Tracking and sync mode | Expected contract | Evidence | Status |
|---|---|---|---|
| Argo annotation tracking, self-heal off | Aura may change an opted-in fixture; an explicit sync restores undelegated fields | pinned Argo CD v2.14.20 Kind journey; Deployment, StatefulSet and CronJob are powered down/suspended and then restored by manual sync | pass-real |
| Argo annotation tracking, self-heal on | Without an agreed field owner, Aura performs one transition, records one deterministic audit, and settles in `Contended` without replaying the mutation | candidate controller plus native Argo self-heal; stable `attemptedAt`, `auditEventID`, replica count and `Contended` phase are observed for an additional 45 seconds | pass-real |
| `ignoreDifferences` only | Difference is hidden from comparison, but a sync caused by another Git change still writes replicas | pinned Argo CD v2.14.20 Kind journey | pass-real; confirmed unsafe for Aura ownership |
| `ignoreDifferences` plus `RespectIgnoreDifferences=true` | Existing resource leaves ignored replicas/suspend to Aura while other Git changes sync | native Deployment, StatefulSet and CronJob plus an unrelated Git revision | pass-real |
| Standard tracking label | Target ownership must be detected and require explicit opt-in | domain tests | partial |
| Custom tracking label | Configured label must be consumed by discovery | no configurable implementation found | unsupported |
| ApplicationSet-generated Application | Same field-authority rules apply to generated apps | generated Application, self-heal, unrelated Git revision and exact restore for Deployment, StatefulSet and CronJob in Kind | pass-real |
| HPA/KEDA | Controller ownership and scale subresource must be explicit | discovery supports neither as a first-class target | unsupported/inconclusive |
| Flux | Ownership detection exists but no full reconciliation contract | unit signal only | pass-simulated only |

`ignoreDifferences` affects comparison. Argo CD requires
`RespectIgnoreDifferences=true` to apply the same rule during sync, and this
pre-patch behavior applies only once the live resource exists. The acceptance
gate therefore requires observing both the Aura transition and a later,
unrelated Git change on the same existing resource.

The automated journey installs Argo CD v2.14.20 from an integrity-checked
manifest and uses a Git repository served only inside the disposable cluster.
It proves manual sync for all three supported workload kinds and real automated
self-heal without field delegation. For self-heal, the gate requires exactly
one successful Aura mutation audit followed by a stable `Contended` action;
the attempt timestamp, deterministic audit ID, audit count and Argo-owned
replica count must remain unchanged during the stability interval. It also
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
