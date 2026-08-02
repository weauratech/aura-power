# Aura Power Helm Chart

Deploys Aura Power (Server + Controller) on a Kubernetes cluster.

## Install

```bash
helm install aura-power oci://ghcr.io/weauratech/charts/aura-power \
  --namespace aura-system --create-namespace \
  --set server.auth.jwtSecret=$(openssl rand -base64 32) \
  --set server.auth.initialAdmin.password=changeme
```

## Values

### Server

| Parameter | Description | Default |
|-----------|-------------|---------|
| `server.enabled` | Deploy the server component | `true` |
| `server.image.repository` | Server image | `ghcr.io/weauratech/aura-power-server` |
| `server.image.tag` | Image tag (defaults to appVersion) | `""` |
| `server.replicas` | Number of server pods (StatefulSet) | `1` |
| `server.port` | HTTP port | `8080` |
| `server.resources.requests.cpu` | CPU request | `100m` |
| `server.resources.requests.memory` | Memory request | `128Mi` |
| `server.resources.limits.cpu` | CPU limit | `500m` |
| `server.resources.limits.memory` | Memory limit | `256Mi` |
| `server.auth.jwtSecret` | **Required.** Secret key for JWT signing | `""` |
| `server.auth.initialAdmin.username` | Initial admin username | `admin` |
| `server.auth.initialAdmin.password` | **Required on first install.** Admin password | `""` |
| `server.auth.accessTokenTTL` | Access token lifetime | `1h` |
| `server.auth.refreshTokenTTL` | Refresh token lifetime | `7d` |
| `server.persistence.enabled` | Enable SQLite PVC | `true` |
| `server.persistence.storageClass` | Storage class (empty = cluster default) | `""` |
| `server.persistence.size` | PVC size | `1Gi` |
| `server.ingress.enabled` | Create Ingress resource | `false` |
| `server.ingress.className` | Ingress class | `""` |
| `server.ingress.host` | Ingress hostname | `""` |
| `server.ingress.tls` | Enable TLS | `false` |
| `server.gateway.enabled` | Create Gateway API HTTPRoute | `false` |
| `server.gateway.gatewayRef.name` | Gateway name | `""` |
| `server.gateway.gatewayRef.namespace` | Gateway namespace | `""` |
| `server.gateway.gatewayRef.sectionName` | Gateway listener section | `""` |
| `server.gateway.hostnames` | HTTPRoute hostnames | `[]` |
| `server.prometheus.url` | Prometheus server URL for metrics | `""` |
| `server.opencost.url` | OpenCost API URL for cost data | `""` |

### Controller

| Parameter | Description | Default |
|-----------|-------------|---------|
| `controller.enabled` | Deploy the controller component | `true` |
| `controller.image.repository` | Controller image | `ghcr.io/weauratech/aura-power-controller` |
| `controller.image.tag` | Image tag (defaults to appVersion) | `""` |
| `controller.replicas` | Controller replicas (leader election) | `1` |
| `controller.resources.requests.cpu` | CPU request | `100m` |
| `controller.resources.requests.memory` | Memory request | `128Mi` |
| `controller.resources.limits.cpu` | CPU limit | `500m` |
| `controller.resources.limits.memory` | Memory limit | `256Mi` |
| `controller.leaderElection.enabled` | Enable leader election | `true` |
| `controller.leaderElection.id` | Leader election lease name | `aura-power-controller-leader.power.aura.sh` |
| `controller.config.reconciliationInterval` | Target reconcile interval | `30s` |
| `controller.config.discoveryInterval` | Workload discovery interval | `60s` |
| `controller.config.auditRetentionDays` | Days to retain audit events | `7` |
| `controller.config.systemNamespaceBlocklist` | Namespaces blocked by guardrails | `[kube-system, kube-public, kube-node-lease]` |

### Global

| Parameter | Description | Default |
|-----------|-------------|---------|
| `imagePullSecrets` | Image pull secrets | `[]` |
| `serviceMonitor.enabled` | Create Prometheus ServiceMonitor | `false` |
| `serviceMonitor.interval` | Scrape interval | `30s` |
| `networkPolicy.enabled` | Create NetworkPolicies | `false` |

## CRDs

CRDs are included in `crds/` and installed automatically on first `helm install`. Helm does not upgrade CRDs on subsequent `helm upgrade` — apply them manually:

```bash
kubectl apply -f charts/aura-power/crds/
```

## Uninstall

```bash
helm uninstall aura-power -n aura-system
```

CRDs and PVCs are preserved by default. To remove everything:

```bash
kubectl delete crd powertargets.power.aura.sh powerpolicies.power.aura.sh \
  poweroverrides.power.aura.sh powerschedules.power.aura.sh \
  powerauditevents.power.aura.sh powernamespacegroups.power.aura.sh
kubectl delete pvc -n aura-system -l app.kubernetes.io/name=aura-power
```
