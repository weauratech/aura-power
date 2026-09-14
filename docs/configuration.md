# Configuration Reference

Complete reference for all environment variables and Helm values.

## Server Environment Variables

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `API_PORT` | HTTP server port | `8080` | No |
| `JWT_SECRET` | Secret key for JWT token signing | — | **Yes** |
| `ADMIN_USERNAME` | Initial admin username | `admin` | No |
| `ADMIN_PASSWORD` | Initial admin password (first install only) | — | **Yes** (first install) |
| `ACCESS_TOKEN_TTL` | Access token lifetime (Go duration) | `1h` | No |
| `REFRESH_TOKEN_TTL` | Refresh token lifetime (Go duration) | `168h` | No |
| `CONTROL_NAMESPACE` | Namespace containing Aura Power CRDs | `aura-system` | No |
| `AUTH_DB_PATH` | SQLite database file path | `/data/aura-power.db` | No |
| `PROMETHEUS_URL` | Prometheus server URL for metrics queries | — | No |
| `OPENCOST_URL` | OpenCost API URL for cost data | — | No |
| `KUBECONFIG` | Path to kubeconfig (local dev only) | — | No (uses in-cluster) |
| `LOG_LEVEL` | Log level (`info` or `debug`) | `info` | No |

## Controller Environment Variables

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `LEADER_ELECTION_ID` | Leader election lease name | `aura-power-controller-leader.power.aura.sh` | No |
| `LEADER_ELECTION_ENABLED` | Enable leader election | `true` | No |
| `CONTROL_NAMESPACE` | Namespace containing Aura Power CRDs and audit events | `aura-system` | No |
| `SYSTEM_NAMESPACES` | Complete comma-separated guardrail blocklist | built-in defaults | No |
| `AUDIT_RETENTION_DAYS` | Days to keep audit events before cleanup | `7` | No |
| `AUDIT_CLEANUP_INTERVAL` | Interval between cleanup runs (Go duration) | `6h` | No |
| `RECONCILIATION_INTERVAL` | Stable target reconciliation interval (Go duration) | `30s` | No |
| `DISCOVERY_INTERVAL` | Full workload discovery interval (Go duration) | `60s` | No |
| `GOMEMLIMIT` | Go runtime soft memory limit; set below the pod memory limit | `192MiB` (Helm) | No |
| `EXTRA_SYSTEM_NAMESPACES` | Additional namespaces to block (comma-separated) | — | No |
| `DEV_MODE` | Enable development logging | `false` | No |

## Helm Values → Environment Variables Mapping

| Helm Value | Maps To | Component |
|------------|---------|-----------|
| `server.auth.jwtSecret` | `JWT_SECRET` | Server |
| `server.auth.existingSecret` | `JWT_SECRET`, `ADMIN_PASSWORD` | Server |
| `server.auth.initialAdmin.username` | `ADMIN_USERNAME` | Server |
| `server.auth.initialAdmin.password` | `ADMIN_PASSWORD` | Server |
| `server.auth.accessTokenTTL` | `ACCESS_TOKEN_TTL` | Server |
| `server.auth.refreshTokenTTL` | `REFRESH_TOKEN_TTL` | Server |
| `server.prometheus.url` | `PROMETHEUS_URL` | Server |
| `server.opencost.url` | `OPENCOST_URL` | Server |
| `server.port` | `API_PORT` | Server |
| `controller.leaderElection.id` | `LEADER_ELECTION_ID` | Controller |
| `controller.leaderElection.enabled` | `LEADER_ELECTION_ENABLED` | Controller |
| Helm release namespace | `CONTROL_NAMESPACE` | Server, Controller |
| `controller.config.systemNamespaceBlocklist` plus release namespace | `SYSTEM_NAMESPACES` | Controller |
| `controller.config.auditRetentionDays` | `AUDIT_RETENTION_DAYS` | Controller |
| `controller.config.auditCleanupInterval` | `AUDIT_CLEANUP_INTERVAL` | Controller |
| `controller.config.reconciliationInterval` | `RECONCILIATION_INTERVAL` | Controller |
| `controller.config.discoveryInterval` | `DISCOVERY_INTERVAL` | Controller |
| `controller.config.goMemLimit` | `GOMEMLIMIT` | Controller |
| `controller.config.pprofBindAddress` | `PPROF_BIND_ADDRESS` | Controller; disabled by default and restricted to loopback |

`CONTROL_NAMESPACE` is an enforcement boundary. The server and controller only
read, reconcile, count, or update namespaced Aura Power decision objects in that
namespace. A `PowerPolicy`, `PowerOverride`, `PowerTarget`, namespace group, or
notification channel created elsewhere is inert. Workload discovery remains
cluster-wide because the selected workloads can live in any application
namespace.

## Annotations

| Annotation | Target | Purpose |
|------------|--------|---------|
| `aura.sh/power-eligible: "true"` | Namespace or Workload | Opt-in to power governance (required for ArgoCD/Flux/Helm-managed workloads) |
| `aura.sh/power-exempt: "true"` | Workload | Permanently exclude from all governance |
| `aura.sh/default-schedule: "<name>"` | Namespace | Apply a named PowerSchedule to all workloads |
| `aura.sh/power-priority: "<int>"` | Namespace | Priority for namespace annotation-based policies |

## Guardrails Configuration

Default blocked namespaces (configurable via `controller.config.systemNamespaceBlocklist`):

```yaml
- kube-system
- kube-public
- kube-node-lease
- aura-system
```

Default blocked workloads (unless opted-in):
- ArgoCD-managed (detected via `argocd.argoproj.io/` annotations)
- Flux-managed (detected via `kustomize.toolkit.fluxcd.io/` labels)
- Helm-managed (detected via `app.kubernetes.io/managed-by: Helm`)
- HPA-controlled (detected via HPA `scaleTargetRef`)

## Ports

| Component | Port | Purpose |
|-----------|------|---------|
| Server | 8080 | HTTP API + Panel + Metrics |
| Controller | 8080 | Prometheus metrics |
| Controller | 8081 | Health probes |
| Controller | 9443 | Admission webhook when enabled |

## Storage

| Component | Storage | Path | Purpose |
|-----------|---------|------|---------|
| Server | PVC (SQLite) | `/data/aura-power.db` | Auth store (users, sessions) |
| Controller | None (stateless) | — | All state in CRDs |

## Resource Recommendations

| Cluster Size | Server CPU/Mem | Controller CPU/Mem |
|-------------|----------------|-------------------|
| < 100 targets | 100m / 128Mi | 100m / 128Mi |
| 100-500 targets | 200m / 256Mi | 200m / 256Mi |
| 500-1000 targets | 500m / 512Mi | 500m / 512Mi |
