# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [2.0.0] - 2026-08-01

### Added
- Architecture split: separate Server (API + Panel) from Controller (Reconciler)
- HttpOnly cookie-based authentication for the web panel
- Informer-cached Kubernetes client for the server (zero API calls per request)
- Role-based access control enforcement on mutation endpoints (member/approver/admin)
- Savings calculated from actual off-time (persistent across restarts)
- Namespace-level `aura.sh/power-eligible` annotation inheritance
- Gateway API HTTPRoute support in Helm chart
- Cloudflare DNS-01 cert-manager solver support
- Integration tests (auth flow, RBAC, health probes)
- Users management page in the web panel
- Sortable columns and stat cards on Targets page
- OpenCost integration for real cluster cost data
- Audit endpoint pagination (default limit 50, sorted by most recent)

### Changed
- Controller no longer serves HTTP (reconciler-only binary, ~30MB image)
- Server is a StatefulSet with PVC for SQLite persistence
- Helm chart restructured: 2 Deployments, 2 ServiceAccounts, split RBAC
- CLI refactored to use HTTP client (connects to server, not K8s API directly)
- Auth mandatory in v2.0 (no more AUTH_ENABLED=false for server)
- Admin role auto-restored on startup if accidentally changed

### Security
- Server cannot PATCH workloads (RBAC enforced)
- Controller has no external HTTP surface
- NetworkPolicy templates available
- Members cannot create/update/delete policies (403)

## [1.2.0] - 2026-07-30

### Added
- Namespace groups (PowerNamespaceGroup CRD)
- Schedule visualization (24x7 grid with click-to-edit)
- User authentication (SQLite + JWT + 3 roles)
- Pending approval workflow
- Metrics page (CPU, Memory, Nodes with Capacity/Requested/Usage)
- Prometheus and OpenCost integration
- Power-eligible annotation on namespaces

### Changed
- Dashboard with schedule overview
- Targets page with rule column
- Sidebar navigation (240px dark theme)

## [1.1.0] - 2026-07-30

### Added
- Prometheus metrics integration (cluster, namespace, workload metrics)
- Time range selector (1h, 6h, 24h, 7d)
- Node count and cost graphs
- OpenCost support via Helm values

## [1.0.0] - 2026-07-30

### Added
- Initial release
- Core decision engine with priority-based conflict resolution
- 4 Reconcilers (Target, Policy, Override, Schedule)
- Discovery loop (auto-discovers Deployments, StatefulSets, CronJobs)
- 5 CRDs (PowerPolicy, PowerTarget, PowerOverride, PowerSchedule, PowerAuditEvent)
- Guardrails (system namespace protection, ArgoCD/Helm/Flux detection, HPA detection)
- React web panel (Dashboard, Targets, Rules, Schedule, Metrics, Savings, Blocked)
- CLI (status, explain, preview, override, savings, discover)
- Helm chart with full configurability
- Property-based tests (pgregory.net/rapid)
- 88% domain test coverage
