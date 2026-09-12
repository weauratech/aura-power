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
| `server.replicas` | Server pods; fixed at one while auth uses local SQLite | `1` |
| `server.port` | HTTP port | `8080` |
| `server.resources.requests.cpu` | CPU request | `100m` |
| `server.resources.requests.memory` | Memory request | `128Mi` |
| `server.resources.limits.cpu` | CPU limit | `500m` |
| `server.resources.limits.memory` | Memory limit | `256Mi` |
| `server.auth.existingSecret` | Existing Secret with `jwt-secret` and `admin-password` | `""` |
| `server.auth.jwtSecret` | JWT signing key; generated once and retained when empty | `""` |
| `server.auth.initialAdmin.username` | Initial admin username | `admin` |
| `server.auth.initialAdmin.password` | Initial password; generated once and retained when empty | `""` |
| `server.auth.accessTokenTTL` | Access token lifetime | `1h` |
| `server.auth.refreshTokenTTL` | Refresh token lifetime | `168h` |
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

### Admission validation

| Parameter | Description | Default |
|-----------|-------------|---------|
| `webhook.enabled` | Register runtime validation for policies and overrides | `false` |
| `webhook.failurePolicy` | API server behavior when the webhook is unavailable | `Fail` |
| `webhook.timeoutSeconds` | Admission request timeout | `5` |
| `webhook.existingSecret` | Existing TLS Secret for deterministic GitOps rendering | `""` |
| `webhook.caBundle` | Base64 CA bundle required with `webhook.existingSecret` | `""` |
| `webhook.certManager.enabled` | Use cert-manager for certificate rotation | `false` |
| `webhook.certManager.issuerRef.name` | Existing Issuer or ClusterIssuer | `""` |

Runtime admission rejects invalid IANA timezones and expired overrides at the
Kubernetes API boundary. Enable the chart-managed self-signed certificate with:

```bash
helm upgrade --install aura-power ./charts/aura-power \
  --namespace aura-system \
  --set webhook.enabled=true
```

The chart reuses its release-scoped TLS Secret across upgrades. For automatic
certificate rotation, install cert-manager and reference an existing issuer:

```bash
helm upgrade --install aura-power ./charts/aura-power \
  --namespace aura-system \
  --set webhook.enabled=true \
  --set webhook.certManager.enabled=true \
  --set webhook.certManager.issuerRef.name=platform-ca
```

Admission uses `failurePolicy: Fail` by default. Confirm the controller is
Ready before creating or updating `PowerPolicy` and `PowerOverride` objects.
Disabling `webhook.enabled` removes the admission resources on the next Helm
upgrade.

For Argo CD, use cert-manager or set both `webhook.existingSecret` and
`webhook.caBundle`. Offline rendering cannot read the live Secret, so the
self-signed fallback produces different certificate material in a fresh render.

### Global

| Parameter | Description | Default |
|-----------|-------------|---------|
| `imagePullSecrets` | Image pull secrets | `[]` |
| `serviceMonitor.enabled` | Create Prometheus ServiceMonitor | `false` |
| `serviceMonitor.interval` | Scrape interval | `30s` |
| `serviceMonitor.namespace` | Namespace in which to create ServiceMonitors | `""` |
| `networkPolicy.enabled` | Create NetworkPolicies | `false` |
| `networkPolicy.additionalServerEgressPorts` | Extra TCP egress ports for providers/webhooks | `[]` |

When an externally managed auth Secret is rotated, restart the server so its
environment receives the new values: `kubectl rollout restart statefulset/aura-power-server -n aura-system`.

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
  powerauditevents.power.aura.sh powernamespacegroups.power.aura.sh \
  powernotificationchannels.power.aura.sh
kubectl delete pvc -n aura-system -l app.kubernetes.io/name=aura-power
```
