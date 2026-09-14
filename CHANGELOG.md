# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [2.2.1] - 2026-09-14

### Fixed
- Export and verify the disposable Kind kubeconfig before release acceptance
  scripts run, preserving their fail-closed context check in GitHub Actions.

## [2.2.0] - 2026-09-14

### Added
- End-to-end acceptance layers for Go contracts, envtest, Kind, Helm upgrades,
  browser journeys, CLI behavior, Argo CD coexistence, HPA ownership, failure
  recovery, controller memory, and an isolated EKS fixture.
- Durable two-phase approval decisions with resource-version preconditions and
  audit correlation from request through execution.
- Runtime version and commit identity in binaries, containers, and health data.
- Namespace-owned notification suppression for controlled fixtures, with the
  immutable decision exposed in action status, audit API, CSV, and the panel.
- Signed multi-architecture images, reproducible Helm packages, per-platform
  SPDX SBOMs, provenance attestations, and an immutable release manifest.

### Changed
- Controller discovery, reconciliation, API watches, admission, RBAC, and
  notifications are scoped to the configured control namespace.
- Workload identity includes namespace, kind, name, and UID. Snapshots are
  persisted before mutation and restored with optimistic concurrency checks.
- Argo CD and autoscaler coexistence fail closed unless current namespace and
  workload annotations explicitly authorize Aura Power ownership.
- Audit pagination, discovery priority, authentication input, request bodies,
  and external-provider calls now have explicit bounds and timeouts.
- Release promotion is serialized, monotonic, digest-verified, and publishes the
  GitHub Release only after every artifact and attestation has been verified.

### Fixed
- Exact target selection across namespaces and workload kinds, including
  homonyms, stale UIDs, opt-in and opt-out changes, and resource recreation.
- Restore behavior for zero replicas, suspended CronJobs, missing snapshots,
  retries, process interruption, leadership changes, and concurrent edits.
- Approval authorization, lease ownership, expired-session behavior, refresh
  request replay, logout, and user removal or role changes.
- Preview and execution drift, false-positive UI assertions, double submission,
  notification delivery reporting, and incomplete CLI exit/error contracts.

### Upgrade notes
- Apply the `v2.2.0` CRDs before upgrading the Helm release.
- Keep the existing server Secret and PVC; do not regenerate the admin password
  or SQLite state during upgrade.
- Configure one control namespace per installation. A second controller must
  not share targets or policies with an existing installation.
- Review explicit opt-in annotations before Aura Power takes ownership from HPA,
  KEDA, Argo CD, or another reconciler. See `docs/gitops.md`.

## [2.0.14] - 2026-08-02

### Added
- Frontend redesign with Aura Power Design System (Material UI + custom theme)
- Dashboard: governance coverage gauge, savings card, activity feed, namespace chart, provider status indicators
- Targets: search, filter by state, group-by namespace, clickable namespace drill-down
- Schedule creation drawer (right panel) with namespace/workload autocomplete and impact preview
- Overrides merged into unified Schedules view with "Temporary" badge and countdown
- Audit Log page with timeline feed, colored action chips, search
- Metrics page with Prometheus time-series charts (CPU, Memory, 1h-7d range selector)
- Impact Preview ("Preview Impact" button shows affected targets before creation)
- Discovery Mode banner (onboarding when no policies exist)
- Success/error Snackbar notifications on all CRUD operations
- Provider status chips on Dashboard (Prometheus/OpenCost connected or not)
- Dark/light mode toggle with localStorage persistence
- Geist + Geist Mono typography (CDN)
- Favicon set with SVG dark mode support, PWA manifest
- PowerNamespaceGroup CRD (was missing from chart)
- Configurable audit event retention via AUDIT_RETENTION_DAYS env var

### Fixed
- Cookie auth: SameSite=Lax + auto-detect Secure from X-Forwarded-Proto
- Override CRD enum (was true/false, now on/off)
- Dashboard card alignment (equal height grid)
- StatusChip dot centering
- Release pipeline: native arm64 runners (5min build vs 40min QEMU)
- Chart kubeVersion constraint for EKS semver compatibility

### Documentation
- Quick Start guide (docs/quick-start.md)
- GitOps coexistence guide (docs/gitops.md) — ArgoCD, Flux, Helm, HPA
- CRD reference (docs/crds.md)
- API reference (docs/api-reference.md)
- Troubleshooting guide (docs/troubleshooting.md)
- Upgrade guide v1→v2 (docs/upgrade-v2.md)
- Helm chart README with full values reference

### Testing
- Shell smoke test script (35 API tests)
- Go E2E test suite (23 tests, CI-integrated)
- Playwright browser tests (28 page + 3 flow tests)
- GitHub Actions smoke test workflow (on-demand)

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
