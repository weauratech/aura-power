# Aura Power Helm Chart

Deploys Aura Power (Server + Controller) on a Kubernetes cluster.

## Install

```bash
helm install aura-power oci://ghcr.io/weauratech/charts/aura-power \
  --namespace aura-system --create-namespace
```

## Values

### Server

| Parameter | Description | Default |
|-----------|-------------|---------|
| `server.enabled` | Deploy the server component | `true` |
| `server.image.repository` | Server image | `ghcr.io/weauratech/aura-power-server` |
| `server.image.tag` | Image tag (defaults to appVersion) | `""` |
| `server.replicas` | Server pods (`0` during controlled maintenance, otherwise `1` while SQLite is pod-local) | `1` |
| `server.port` | HTTP port | `8080` |
| `server.resources.requests.cpu` | CPU request | `100m` |
| `server.resources.requests.memory` | Memory request | `128Mi` |
| `server.resources.limits.cpu` | CPU limit | `500m` |
| `server.resources.limits.memory` | Memory limit | `256Mi` |
| `server.auth.existingSecret` | Existing Secret with `jwt-secret` and `admin-password` | `""` |
| `server.auth.keepManagedSecret` | Transitional retention annotation used before moving the managed Secret to `existingSecret` | `false` |
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
| `controller.replicas` | Controller replicas (`0` pauses reconciliation; values above `1` require leader election) | `1` |
| `controller.resources.requests.cpu` | CPU request | `100m` |
| `controller.resources.requests.memory` | Memory request | `128Mi` |
| `controller.resources.limits.cpu` | CPU limit | `500m` |
| `controller.resources.limits.memory` | Memory limit | `256Mi` |
| `controller.leaderElection.enabled` | Enable leader election | `true` |
| `controller.leaderElection.id` | Leader election lease name | `aura-power-controller-leader.power.aura.sh` |
| `controller.config.reconciliationInterval` | Target reconcile interval | `30s` |
| `controller.config.discoveryInterval` | Workload discovery interval | `60s` |
| `controller.config.auditRetentionDays` | Days to retain audit events | `7` |
| `controller.config.goMemLimit` | Go runtime soft memory limit, kept below the pod limit | `192MiB` |
| `controller.config.pprofBindAddress` | Optional loopback-only pprof listener for authorized diagnostics | `""` |
| `controller.config.systemNamespaceBlocklist` | Namespaces blocked by guardrails | `[kube-system, kube-public, kube-node-lease]` |

### Maintenance windows

The chart accepts `server.replicas=0` and `controller.replicas=0` so an upgrade
can quiesce SQLite and stop reconciliation without making out-of-band scaling
changes that the next Helm upgrade would undo. A candidate chart defaults to
its candidate `appVersion`; `--reuse-values` does not pin an image whose saved
tag is empty. Therefore, create a mode `0600` effective-values file and set the
currently running, verified image repositories and digests explicitly before
the first maintenance upgrade:

```bash
umask 077
maintenance_values="$(mktemp)"
decision_inventory="$(mktemp)"
cleanup_maintenance_values() {
  for file in "$maintenance_values" "$decision_inventory"; do
    if [ -f "$file" ]; then
      : >"$file"
      unlink "$file"
    fi
  done
}
trap cleanup_maintenance_values EXIT

# Set these from verified release metadata. They must identify the images that
# are already running, not the candidate images.
: "${CURRENT_SERVER_REPOSITORY:?set the current server repository}"
: "${CURRENT_SERVER_DIGEST:?set the current server sha256 digest}"
: "${CURRENT_CONTROLLER_REPOSITORY:?set the current controller repository}"
: "${CURRENT_CONTROLLER_DIGEST:?set the current controller sha256 digest}"
: "${CANDIDATE_CHART:?set the path to the verified candidate chart}"
export CURRENT_SERVER_REPOSITORY CURRENT_SERVER_DIGEST
export CURRENT_CONTROLLER_REPOSITORY CURRENT_CONTROLLER_DIGEST

inventory_decisions() {
  kubectl get \
    powerpolicies,poweroverrides,powerschedules,powernamespacegroups \
    --all-namespaces -o json | jq '
      [.items[] | {
        apiVersion, kind,
        namespace: .metadata.namespace,
        name: .metadata.name,
        uid: .metadata.uid,
        generation: .metadata.generation
      }] | sort_by(.apiVersion, .kind, .namespace, .name)
    '
}

helm get values aura-power --namespace aura-system --all --output yaml \
  >"$maintenance_values"
chmod 0600 "$maintenance_values"
yq -e '
  .webhook.enabled == true and .webhook.failurePolicy == "Fail"
' "$maintenance_values" >/dev/null
leader_election_id="$(yq -r '.controller.leaderElection.id' \
  "$maintenance_values")"
old_holder="$(kubectl get lease "$leader_election_id" \
  --namespace aura-system -o jsonpath='{.spec.holderIdentity}')"
[[ -n "$old_holder" ]]
yq -i '
  .server.image.repository = strenv(CURRENT_SERVER_REPOSITORY) |
  .server.image.tag = "" |
  .server.image.digest = strenv(CURRENT_SERVER_DIGEST) |
  .controller.image.repository = strenv(CURRENT_CONTROLLER_REPOSITORY) |
  .controller.image.tag = "" |
  .controller.image.digest = strenv(CURRENT_CONTROLLER_DIGEST) |
  .controller.replicas = 0 |
  .server.replicas = 1
' "$maintenance_values"

helm upgrade aura-power "$CANDIDATE_CHART" --namespace aura-system \
  --reset-values -f "$maintenance_values" --dry-run=server --hide-secret
helm upgrade aura-power "$CANDIDATE_CHART" --namespace aura-system \
  --reset-values -f "$maintenance_values" --cleanup-on-fail --wait --timeout 8m

kubectl get deployment aura-power-controller --namespace aura-system -o json \
  | jq -e '(.status.replicas // 0) == 0' >/dev/null
inventory_decisions >"$decision_inventory"

yq -i '.server.replicas = 0' "$maintenance_values"
helm upgrade aura-power "$CANDIDATE_CHART" --namespace aura-system \
  --reset-values -f "$maintenance_values" --dry-run=server --hide-secret
helm upgrade aura-power "$CANDIDATE_CHART" --namespace aura-system \
  --reset-values -f "$maintenance_values" --cleanup-on-fail --wait --timeout 8m
```

At this point no candidate binary has run: the controller stopped first and the
server then stopped while both workloads still referenced their previous
digests. Perform the consistent SQLite backup and other maintenance checks now.
Maintain an external change freeze for policies, overrides, schedules and
namespace groups. Even with the required fail-closed webhook, only `CREATE` and
`UPDATE` of `PowerPolicy` and `PowerOverride` are covered; `DELETE` and the other
decision resources are not intercepted. Abort if a fresh inventory differs
from `decision_inventory`.
Only after they succeed, replace the two digest values in the restricted file
with verified candidate digests while both replica counts remain zero. Review
and apply that quiesced revision, then restore the server before the controller:

```bash
: "${CANDIDATE_SERVER_DIGEST:?set the verified candidate server digest}"
: "${CANDIDATE_CONTROLLER_DIGEST:?set the verified candidate controller digest}"
export CANDIDATE_SERVER_DIGEST CANDIDATE_CONTROLLER_DIGEST
yq -i '
  .server.image.digest = strenv(CANDIDATE_SERVER_DIGEST) |
  .controller.image.digest = strenv(CANDIDATE_CONTROLLER_DIGEST)
' "$maintenance_values"

helm upgrade aura-power "$CANDIDATE_CHART" --namespace aura-system \
  --reset-values -f "$maintenance_values" --dry-run=server --hide-secret
helm upgrade aura-power "$CANDIDATE_CHART" --namespace aura-system \
  --reset-values -f "$maintenance_values" --cleanup-on-fail --wait --timeout 8m

yq -i '.server.replicas = 1' "$maintenance_values"
helm upgrade aura-power "$CANDIDATE_CHART" --namespace aura-system \
  --reset-values -f "$maintenance_values" --cleanup-on-fail --wait --timeout 8m
kubectl rollout status statefulset/aura-power-server \
  --namespace aura-system --timeout=5m

yq -i '.controller.replicas = 1' "$maintenance_values"
helm upgrade aura-power "$CANDIDATE_CHART" --namespace aura-system \
  --reset-values -f "$maintenance_values" --cleanup-on-fail --wait --timeout 8m
kubectl rollout status deployment/aura-power-controller \
  --namespace aura-system --timeout=5m

deadline=$((SECONDS + 120))
new_holder=""
while (( SECONDS < deadline )); do
  new_holder="$(kubectl get lease "$leader_election_id" \
    --namespace aura-system -o jsonpath='{.spec.holderIdentity}')"
  [[ -n "$new_holder" && "$new_holder" != "$old_holder" ]] && break
  sleep 2
done
[[ -n "$new_holder" && "$new_holder" != "$old_holder" ]]
cmp -s "$decision_inventory" <(inventory_decisions)

: "${POST_START_OBSERVATION_SECONDS:?set at least two times the larger configured interval}"
[[ "$POST_START_OBSERVATION_SECONDS" =~ ^[1-9][0-9]*$ ]]
sleep "$POST_START_OBSERVATION_SECONDS"
cmp -s "$decision_inventory" <(inventory_decisions)
```

`--wait` proves Pod readiness, not controller leadership or successful
reconciliation. Before lifting the change freeze, require a non-empty Lease
`holderIdentity` that differs from the pre-maintenance holder, compare a fresh
decision-resource inventory with the saved inventory, and observe at least two
complete configured discovery and reconciliation intervals. During that gate,
verify the expected PowerTarget UID set and cardinality, require no unexpected
pending, failed, or contended actions, and compare each admitted workload's UID
and replica or CronJob suspension state with the pre-maintenance baseline and
current policy decision. An unrelated workload mutation, missing target, stale
Lease, or unexplained state transition fails the maintenance acceptance.

Do not leave either component at zero after the maintenance window. The server
remains limited to one replica. Controllers above one are accepted only when
leader election is enabled.

### Admission validation

| Parameter | Description | Default |
|-----------|-------------|---------|
| `webhook.enabled` | Register fail-closed runtime validation for policies and overrides | `true` |
| `webhook.failurePolicy` | API server behavior when the webhook is unavailable | `Fail` |
| `webhook.timeoutSeconds` | Admission request timeout | `5` |
| `webhook.existingSecret` | Existing TLS Secret for deterministic GitOps rendering | `""` |
| `webhook.caBundle` | Base64 CA bundle required with `webhook.existingSecret` | `""` |
| `webhook.certManager.enabled` | Use cert-manager for certificate rotation | `false` |
| `webhook.certManager.issuerRef.name` | Existing Issuer or ClusterIssuer | `""` |
| `networkPolicy.additionalControllerEgressPorts` | Additional notification webhook ports allowed from the controller | `[]` |

Runtime admission rejects invalid IANA timezones and expired overrides at the
Kubernetes API boundary. The default chart-managed self-signed certificate can be installed with:

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
| `prometheusRule.enabled` | Create controller heap, working-set and OOM alerts | `false` |
| `prometheusRule.heapLimitRatio` | Heap/GOMEMLIMIT warning ratio | `0.85` |
| `prometheusRule.workingSetLimitRatio` | Container working-set/limit warning ratio | `0.85` |
| `prometheusRule.for` | Required sustained pressure before warning | `10m` |
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
