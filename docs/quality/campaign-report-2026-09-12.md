# Quality campaign baseline report — 2026-09-12

This dated report preserves behavior observed before remediation. Current
candidate results live in `functional-matrix.md`; failures recorded here are
historical evidence for the installed baseline.

## Scope and versions

- Source reviewed: `weauratech/aura-power` at `4727bc56f144`.
- Disposable cluster: Kind, Kubernetes 1.31.14, images built from that source.
- Production observation: `eks-aura-prd`, Kubernetes 1.35.6 EKS, Aura Power
  v2.1.7, Argo CD v2.10.13.
- Production and checkout results are kept separate because they are different
  builds and contracts may have changed.

## Confirmed outcomes

The fresh Helm installation became ready and API health, authentication, lists,
CRUD and the existing RBAC E2E suite passed against the real Kind API server.
The component test layer and the non-acceptance Go suite also pass. Browser
automation passed 17/17 scenarios in Chromium and all desktop scenarios in
Firefox and WebKit. Fourteen mobile Chromium journeys could not reach sidebar
destinations because the responsive menu button has no accessible name and the
links remain inside a closed drawer.

The core safety promise does not pass. In a real Kubernetes journey, a Deployment
with two replicas and a StatefulSet with one replica were powered down. Both
`PowerTarget` snapshots were persisted with replica count zero. After changing
the policy to `on`, the controller cleared the snapshots and left both workloads
at zero. The UI simultaneously reported desired `on`, observed `off`, divergent,
and repeated successful restore audit entries.

The same core failure was reproduced against the installed EKS release v2.1.7:
a dedicated Deployment with two replicas reached zero, its snapshot reported
zero, and it remained at zero for the full five-minute restoration deadline.
The runner then scaled it directly and deleted the policy, target and namespace;
post-run queries found no campaign-labelled resources. Early admission runs also
proved the cluster's workload taint requirement and were fully cleaned before
the accepted run.

Discovery produced one target for a Deployment and CronJob sharing namespace and
name, losing the CronJob identity. Built-in schedules also failed admission: the
installed CRD encodes `on` and `off` as boolean enum values while declaring the
field as a string.

## Production inventory

Aura Hub issued a time-limited read-only EKS profile and then the eligible
OperationsAccess profile. The read-only inventory found the server and controller
ready, no Aura Power policies/overrides/targets/channels in `aura-system`, and the
controller at six restarts with its last termination recorded as `OOMKilled`.
Mutating acceptance remains restricted to a unique campaign namespace and the
campaign policy; business workloads are observation-only. A deliberate Argo CD
field-ownership dispute was kept in Kind after restoration failed the P1 gate.
With self-heal enabled and no ownership split, Argo held two replicas while Aura
recorded 33 power-down actions and 10 errors in about 80 seconds. Adding both
`ignoreDifferences` for `/spec/replicas` and
`RespectIgnoreDifferences=true` kept the Application `Synced` and the workload at
zero across five consecutive observations. The EKS Argo installation was not
mutated.

## Readiness decision

Aura Power is not ready for a claim of complete functional correctness. P1 gates
are original-state restoration, collision-free target identity, exact selection,
and truthful action/audit state. Approval processing, errors in integrations,
GitOps authority, recovery, and portability need executable contracts before a
state-of-the-art claim is supportable.

Thirteen verified functional/operational issues were published and are indexed
in [issues-2026-09-12.md](issues-2026-09-12.md). A separate independent manual
security review validated seven medium and one low finding; their details remain
in restricted evidence under the repository's disclosure policy. No critical or
high security finding was validated. The managed security deep-scan worker could
not start under this session's filesystem permission profile, so the report does
not claim managed-scan completeness.
