<p align="center">
  <img src="assets/lockup.svg" alt="Aura Power" height="64">
  <br><br>
  <strong>Kubernetes workload energy governance</strong><br>
  Power down and restore workloads on schedule. Reduce cloud costs without manual intervention.
  <br><br>
  <a href="https://github.com/weauratech/aura-power/actions/workflows/ci.yaml"><img src="https://github.com/weauratech/aura-power/actions/workflows/ci.yaml/badge.svg" alt="CI"></a>
  <a href="https://github.com/weauratech/aura-power/releases"><img src="https://img.shields.io/github/v/release/weauratech/aura-power?style=flat-square" alt="Release"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache%202.0-blue?style=flat-square" alt="License"></a>
  <a href="https://goreportcard.com/report/github.com/weauratech/aura-power"><img src="https://goreportcard.com/badge/github.com/weauratech/aura-power?style=flat-square" alt="Go Report Card"></a>
  <a href="https://github.com/weauratech/aura-power/releases"><img src="https://img.shields.io/github/downloads/weauratech/aura-power/total?style=flat-square&color=green" alt="Downloads"></a>
</p>

---

## Why Aura Power?

Most development workloads run 24/7 even though teams only use them 8-10 hours a day. That is 60% wasted compute. Aura Power lets you define when workloads should be on and automatically shuts them down outside those hours.

| Problem | Solution |
|---------|----------|
| Dev/staging clusters running overnight and weekends | Schedule-based power-off with automatic restore |
| No visibility into what is off and why | Web panel with real-time status and savings tracking |
| Fear of accidentally shutting down critical workloads | Guardrails: system namespace protection, ArgoCD/Helm detection, opt-in model |
| Complex tooling requiring cluster admin access | Web panel with role-based access (no kubeconfig needed) |

## Features

- **Schedule-based governance** Define time windows when workloads should be on. Outside those windows, they power off automatically.
- **Priority-based conflict resolution** Multiple policies can overlap. The highest priority wins. Temporary overrides can supersede policies.
- **Guardrails** System namespaces are never touched. Workloads managed by ArgoCD, Helm, or Flux are blocked by default (opt-in required).
- **Web Panel** Real-time dashboard with targets, rules, schedule visualization, metrics, and savings tracking.
- **Prometheus + OpenCost integration** Real cluster metrics (CPU, memory, nodes) and cost data from OpenCost.
- **Split architecture** Server (API + Panel) and Controller (Reconciler) run as separate workloads with isolated RBAC.
- **CLI** Terminal-based operations for scripting and automation.

## Architecture

```
                          Users (Browser / CLI)
                                  |
                    ┌─────────────v──────────────┐
                    |    aura-power-server        |
                    |    (API + Panel + Auth)     |
                    |    StatefulSet, port 8080   |
                    └─────────────┬──────────────┘
                                  |
                         Kubernetes API (CRDs)
                                  |
                    ┌─────────────v──────────────┐
                    |  aura-power-controller     |
                    |  (Reconciler + Discovery)  |
                    |  Deployment, leader elect  |
                    └────────────────────────────┘
                                  |
                    ┌─────────────v──────────────┐
                    |  Your Workloads            |
                    |  (Deployments, STS, CJ)    |
                    └────────────────────────────┘
```

The server handles user interactions and serves the web panel. The controller runs reconciliation loops and executes power-down/restore actions. They communicate exclusively through CRDs.

## Quick Start

### Install

```bash
helm install aura-power oci://ghcr.io/weauratech/charts/aura-power \
  --namespace aura-system --create-namespace \
  --set server.auth.jwtSecret=$(openssl rand -hex 32) \
  --set server.auth.initialAdmin.password=changeme \
  --set server.prometheus.url=http://prometheus.monitoring.svc:9090 \
  --set server.gateway.enabled=true \
  --set server.gateway.gatewayRef.name=my-gateway \
  --set server.gateway.gatewayRef.namespace=gateway-ns \
  --set server.gateway.gatewayRef.sectionName=https \
  --set "server.gateway.hostnames[0]=power.int.example.com"
```

### Access the Panel

If you configured an Ingress or Gateway API HTTPRoute, access the panel at the hostname you defined:

```
https://power.int.example.com
```

For local testing without Ingress:

```bash
kubectl port-forward -n aura-system statefulset/aura-power-server 8080:8080
```

Login with the admin credentials you set during installation.

### Create Your First Policy

```yaml
apiVersion: power.aura.sh/v1alpha1
kind: PowerPolicy
metadata:
  name: dev-off-hours
  namespace: aura-system
spec:
  scope:
    namespaces: [dev, staging]
  schedule:
    desiredState: "on"
    windows:
      - start: "08:00"
        end: "18:00"
        days: [1, 2, 3, 4, 5]
        timezone: "America/Sao_Paulo"
  priority: 10
```

Workloads in `dev` and `staging` will power off outside 08:00-18:00 Mon-Fri.

### Opt-In Workloads

```bash
# Single workload
kubectl annotate deployment my-app aura.sh/power-eligible=true

# All workloads in a namespace
kubectl annotate namespace dev aura.sh/power-eligible=true
```

## CRDs

| Resource | Purpose |
|----------|---------|
| `PowerPolicy` | Recurring schedule for workload governance |
| `PowerTarget` | Auto-discovered workload with current state |
| `PowerOverride` | Temporary exception with automatic expiration |
| `PowerSchedule` | Named schedule definition (reusable) |
| `PowerAuditEvent` | Audit trail of all actions taken |

## Helm Chart

```bash
# OCI registry (recommended)
helm install aura-power oci://ghcr.io/weauratech/charts/aura-power

# With custom values
helm install aura-power oci://ghcr.io/weauratech/charts/aura-power -f values.yaml
```

See [charts/aura-power/README.md](charts/aura-power/README.md) for full configuration reference.

## CLI

Pre-built binaries are available on the [Releases](https://github.com/weauratech/aura-power/releases) page.

```bash
# Login to the server
aura-power login --server https://power.int.example.com --username admin

# Check status
aura-power status

# Explain a workload's state
aura-power explain dev/my-deployment

# View savings
aura-power savings
```

## Development

```bash
# Prerequisites: Go 1.25+, Node.js 20+, Docker, Helm 3

# Run tests
make test-all

# Build all binaries
make build-all

# Build Docker images
make docker-build

# Lint
make lint
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for the full development guide.

## How It Works

1. **Discovery**: The controller scans all namespaces for Deployments, StatefulSets, and CronJobs. Creates a `PowerTarget` CRD for each.

2. **Evaluation**: For each target, the engine evaluates all active policies and overrides. The highest-priority rule wins.

3. **Execution**: If the desired state is "off" and the workload is running, the controller scales it to zero (or suspends CronJobs). The original replica count is stored in a snapshot for restoration.

4. **Guardrails**: Before executing, the controller checks for blocks: system namespaces, missing opt-in annotation, active HPAs, ArgoCD/Helm/Flux management. Blocked workloads are never touched.

5. **Restoration**: When the schedule window opens again, the controller restores workloads to their original state using the stored snapshot.

## Comparison

| Feature | Aura Power | kube-green | CAST AI |
|---------|:----------:|:----------:|:-------:|
| Schedule-based power management | Yes | Yes | Yes |
| Web panel | Yes | No | Yes |
| Priority-based conflict resolution | Yes | No | No |
| Temporary overrides | Yes | No | Yes |
| Guardrails (ArgoCD, Helm detection) | Yes | No | N/A |
| Role-based access control | Yes | No | Yes |
| Open source | Yes (Apache 2.0) | Yes (MIT) | No |
| Self-hosted | Yes | Yes | No |
| CronJob support | Yes | Yes | N/A |
| Savings tracking | Yes | No | Yes |
| Prometheus integration | Yes | No | Yes |
| CLI | Yes | No | Yes |

## Community

- [Issues](https://github.com/weauratech/aura-power/issues) for bug reports and feature requests
- [Discussions](https://github.com/weauratech/aura-power/discussions) for questions and ideas
- [Contributing Guide](CONTRIBUTING.md)
- [Security Policy](SECURITY.md)
- [Code of Conduct](CODE_OF_CONDUCT.md)

## License

Apache License 2.0. See [LICENSE](LICENSE) for details.

---

<p align="center">
  Built by <a href="https://github.com/weauratech">Aura Tech</a>
</p>
