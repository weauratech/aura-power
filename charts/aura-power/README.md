# Aura Power Helm Chart

Kubernetes workload energy governance. Safely power down and restore workloads on schedule.

## Installation

```bash
helm install aura-power oci://ghcr.io/weauratech/charts/aura-power \
  --namespace aura-system --create-namespace \
  --set server.auth.jwtSecret=$(openssl rand -hex 32) \
  --set server.auth.initialAdmin.password=changeme
```

## Architecture

The chart deploys two components:

- **Server** (StatefulSet): API, web panel, authentication, metrics proxy
- **Controller** (Deployment): Reconcilers, discovery loop, workload execution

They communicate exclusively through Kubernetes CRDs.

## Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `server.enabled` | Deploy the server component | `true` |
| `server.replicas` | Server replicas | `1` |
| `server.port` | HTTP port | `8080` |
| `server.auth.jwtSecret` | JWT signing secret (required) | `""` |
| `server.auth.initialAdmin.username` | Initial admin username | `admin` |
| `server.auth.initialAdmin.password` | Initial admin password (required) | `""` |
| `server.persistence.enabled` | Enable PVC for SQLite | `true` |
| `server.persistence.size` | PVC size | `1Gi` |
| `server.ingress.enabled` | Create Ingress resource | `false` |
| `server.gateway.enabled` | Create HTTPRoute (Gateway API) | `false` |
| `server.prometheus.url` | Prometheus server URL | `""` |
| `server.opencost.url` | OpenCost server URL | `""` |
| `controller.enabled` | Deploy the controller component | `true` |
| `controller.replicas` | Controller replicas (should be 1) | `1` |
| `controller.leaderElection.enabled` | Enable leader election | `true` |
| `rbac.create` | Create RBAC resources | `true` |
| `networkPolicy.enabled` | Create NetworkPolicy resources | `false` |

See [values.yaml](values.yaml) for the full list.

## RBAC

The chart creates separate ClusterRoles:

- **Server**: Can read/write CRDs + read workloads (cannot scale/patch workloads)
- **Controller**: Full CRD access + can scale workloads + leader election

## Upgrading

### From 1.x to 2.0

v2.0 splits the single Deployment into Server (StatefulSet) + Controller (Deployment).

1. Back up your SQLite database (`/data/aura-power.db`)
2. Upgrade: `helm upgrade aura-power oci://ghcr.io/weauratech/charts/aura-power`
3. The PVC will be recreated by the StatefulSet
4. Auth tokens are invalidated (users re-login once)

## Uninstalling

```bash
helm uninstall aura-power -n aura-system
```

Note: CRDs are not deleted automatically. To remove them:

```bash
kubectl delete crd powerpolicies.power.aura.sh powertargets.power.aura.sh \
  poweroverrides.power.aura.sh powerschedules.power.aura.sh \
  powerauditevents.power.aura.sh
```
