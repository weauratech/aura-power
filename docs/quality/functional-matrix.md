# Functional verification matrix

Status values distinguish **pass-real**, **pass-simulated**, **failed**,
**blocked**, **inconclusive**, and **not-implemented**. A passing HTTP response is
not evidence that a workload action completed.

| Capability | Contract and evidence | Local | EKS v2.1.7 | Result |
|---|---|---:|---:|---|
| Domain priority and schedule | Deterministic unit/property contracts | pass | not needed | pass-simulated |
| Discovery of Deployment, StatefulSet, CronJob | Native API objects become targets | pass | observed installed controller | pass-real, limited |
| Workload identity | Namespace, kind, name and UID remain distinct | two homonymous kinds collapse | not mutated | failed |
| Exact namespace/name selection | Scope identifies exact workload pairs | frontend and converter contracts fail | not mutated | failed |
| Namespace groups | Group membership reaches decision engine | acceptance contract fails | not mutated | failed |
| Workload and namespace labels | Labels survive discovery and match policies | acceptance contracts fail | not mutated | failed |
| Deployment/StatefulSet shutdown | Native workloads reach zero | pass | Deployment with 2 replicas reached zero | pass-real |
| Replica restoration | Restore the original 2 and 1 replicas | snapshots persisted as zero; final replicas zero | snapshot was zero; after 5 minutes replicas remained zero | failed |
| CronJob restoration | Preserve an originally suspended CronJob | executor contract fails | not mutated | failed |
| Built-in schedules | Seed valid `on`/`off` schedules | API server rejects generated boolean enum | installed objects not readable cluster-wide | failed |
| Preview/explain versus execution | Same selected set and state transition | override preview contract fails; UI shows desired on, replicas zero | not mutated | failed |
| Audit truthfulness | One decision/action with correct kind and outcome | duplicate restore events observed | read-only inventory only | failed |
| Authentication/RBAC | Login and role matrix | existing API E2E passes; deeper contracts separate | endpoint not exercised | partial |
| Approval workflow | Create, approve/reject, apply and audit | frontend is static placeholder | not exercised | not-implemented |
| Notifications | Provider error remains visible; controlled receiver gets event | frontend 504 contract fails | no channel configured | failed/blocked |
| Frontend journeys | Real backend, accessible controls, deterministic browser tests | 17/17 Chromium; desktop Firefox/WebKit pass; mobile navigation fails | not exposed by this run | partial/failed mobile |
| Helm installation | Fresh install becomes ready using built images | pass in Kind | v2.1.7 already installed | pass-real |
| Argo CD coexistence | Drift, sync and field authority remain stable | self-heal oscillates; RespectIgnoreDifferences stabilizes replica ownership | Argo CD 2.10.13 observed; mutation withheld after P1 restore failure | failed by default; partial with explicit config |
| HPA/KEDA/Flux | Explicit supported behavior | no complete adapter contract | not exercised | inconclusive |
| Recovery under conflict/failure | No snapshot loss or false success | acceptance conflicts reproduce loss | not exercised | failed |

## Release gate

The current product must not be declared fully approved while target identity,
selection, snapshot persistence, restoration, approval semantics, or audit
truthfulness fail. Those are central invariants rather than optional coverage.
