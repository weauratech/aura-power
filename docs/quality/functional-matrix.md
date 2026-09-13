# Functional verification matrix

The installed EKS v2.1.7 result is retained separately from the candidate branch. A passing simulated test does not replace a native Kubernetes result, and a result against the installed release does not validate unpublished code.

| Capability | Candidate evidence | EKS v2.1.7 baseline | Candidate result |
|---|---|---|---|
| Domain priority, schedules and time | unit, property, fuzz and acceptance contracts | not required | pass-simulated |
| Discovery of Deployment, StatefulSet and CronJob | expanded Kind journey with three native objects | three types observed | pass-real |
| Identity and exact selection | homonymous objects remain distinct; exact UID-bound Deployment ref leaves StatefulSet and CronJob unchanged | not mutated | pass-real |
| Namespace groups and labels | Kind policy intersects group, namespace label and workload label | not mutated | pass-real |
| Replica snapshot and restore | Kind preserves Deployment 2 and StatefulSet 1 across shutdown/restore | Deployment snapshot incorrectly persisted 0 | pass-real; baseline failed |
| CronJob restoration | Kind restores `suspend=false`; executor tests cover original true/false and repeated restore | not mutated | pass-real |
| Built-in schedules and enum semantics | API/CRD contracts use string `on`/`off` and valid objects seed | baseline issue identified | pass-simulated |
| Preview/explain versus execution | API uses the domain preview and resolved scope; contract tests compare selected targets/conflicts | not exercised | pass-simulated |
| Audit truthfulness | action/result/rule grouping and approval audit contracts; real Kind approval creates one audit object | read-only inventory | pass-real for approval; pass-simulated for delivery grouping |
| Authentication and RBAC | login/session/refresh/logout and member/approver/admin verb matrix | not exercised | pass-simulated and browser-real |
| Approval workflow | real server and Kind API: member submits, admin approves, one policy and audit object created, replay returns 409 | not exercised | pass-real |
| Notifications and webhooks | controlled HTTP receiver, retry/error and bounded queue contracts; URL may come from Secret | real channel intentionally not used | pass-simulated; EKS blocked by safety boundary |
| Frontend and accessibility | 75 live checks passed against the final Kind candidate in Chromium, Firefox, WebKit and mobile Chromium; three desktop mobile-only cases were skipped by design | not exposed | pass-real |
| Helm install and upgrade | clean Kind install, stable auth/TLS secrets, default fail-closed admission, invalid timezone rejected | existing release observed | pass-real |
| Argo CD coexistence | action state machine detects sustained contention after convergence grace; GitOps matrix records ownership modes | Argo present; mutation stopped after baseline restore failure | pass-simulated; full Argo matrix remains compatibility evidence, not release blocker |
| HPA, KEDA and Flux | explicitly documented as unproven integrations | not exercised | inconclusive/non-claimed |
| Recovery and concurrency | snapshot-before-mutation, UID validation, crash replay, status conflict, stale discovery and legacy upgrade contracts | baseline restoration failed | pass-simulated plus native transition |
| Controller memory | bounded discovery and notification queues; repeated load contract in release gate | read-only footprint observed | pass-load |

## Release decision

The candidate passed `make quality quality-acceptance quality-load`, the expanded Kind journey, Helm upgrade/admission checks, legacy snapshot migration, the real approval workflow, and the clean multi-browser Playwright run. Production applications remain outside the mutation scope; the EKS result documents the installed baseline defect and verified cleanup.
