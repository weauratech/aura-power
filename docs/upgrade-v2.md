# Upgrade Guide: v1.x to v2.0

This guide covers upgrading from Aura Power v1.x (single binary) to v2.0 (server + controller split architecture).

## Breaking Changes

### Architecture Split

v2.0 separates the single binary into two components:

| Component | Workload | Purpose |
|-----------|----------|---------|
| **Server** | StatefulSet | REST API, web panel, authentication, SQLite storage |
| **Controller** | Deployment | Reconciliation loop, workload power management |

### Authentication Required

v2.0 introduces mandatory JWT authentication. All API access requires login. The web panel uses HttpOnly cookies.

### CRD Changes

New fields added to `PowerTargetStatus`:
- `snapshot` — captured state for restoration
- `savings` — accumulated cost savings
- `consecutiveFailures` — retry failure tracking
- `conditions` — standard Kubernetes conditions

New CRD: `PowerNamespaceGroup` — groups namespaces for policy reuse.

### Helm Chart Restructured

The chart now deploys two workloads. Values structure changed:

```yaml
# v1.x (single deployment)
replicaCount: 1
image:
  repository: ...

# v2.0 (split)
server:
  enabled: true
  image:
    repository: ghcr.io/weauratech/aura-power-server
controller:
  enabled: true
  image:
    repository: ghcr.io/weauratech/aura-power-controller
```

---

## Prerequisites

- Helm 3.x
- kubectl access to the target cluster
- Existing v1.x installation

## Upgrade Steps

### 1. Back up existing CRDs

```bash
kubectl get powertargets --all-namespaces -o yaml > backup-powertargets.yaml
kubectl get powerpolicies --all-namespaces -o yaml > backup-powerpolicies.yaml
kubectl get poweroverrides --all-namespaces -o yaml > backup-poweroverrides.yaml
kubectl get powerschedules --all-namespaces -o yaml > backup-powerschedules.yaml
kubectl get powernamespacegroups --all-namespaces -o yaml > backup-powernamespacegroups.yaml
kubectl get powernotificationchannels --all-namespaces -o yaml > backup-powernotificationchannels.yaml
kubectl get powerauditevents --all-namespaces -o yaml > backup-powerauditevents.yaml
kubectl get powernotificationdeliveries --all-namespaces -o yaml > backup-powernotificationdeliveries.yaml

# Quiesce the server and snapshot its PVC with your storage provider or CSI
# VolumeSnapshot workflow. SQLite uses WAL mode, so copying only the live .db
# file is not a consistent backup.
kubectl scale -n aura-system statefulset/aura-power-server --replicas=0
kubectl get pvc -n aura-system data-aura-power-server-0
# Create and verify the storage snapshot here, then restore the server:
kubectl scale -n aura-system statefulset/aura-power-server --replicas=1
```

### 2. Update CRDs

v2.0 includes CRDs in the Helm chart `crds/` directory. Helm installs CRDs on first install but does not upgrade them automatically.

Install the `PowerNotificationDelivery` CRD before starting a controller version
that uses the durable outbox. Helm does not add or update `crds/` content during
an upgrade.

Apply the new CRDs manually:

```bash
kubectl apply -f charts/aura-power/crds/
```

For v2.2.0 and later this ordering is a safety requirement: apply both
`PowerTarget` and `PowerAuditEvent` CRDs before updating the controller. The
controller readiness endpoint validates the installed suppression fields and
stays unready when the API schema is incomplete. Do not bypass readiness or
start a rollout with the older CRDs.

### 3. Prepare authentication values

v2.0 requires auth configuration. Generate a JWT secret:

```bash
JWT_SECRET=$(openssl rand -base64 32)
```

### 4. Upgrade the Helm release

```bash
helm upgrade aura-power ./charts/aura-power \
  -n aura-system \
  --set server.image.tag=v2.0.0 \
  --set controller.image.tag=v2.0.0 \
  --set server.auth.jwtSecret="$JWT_SECRET" \
  --set server.auth.initialAdmin.password="your-secure-password"
```

### 5. Verify deployment

```bash
# Check pods
kubectl get pods -n aura-system

# Verify server readiness
kubectl exec -n aura-system statefulset/aura-power-server -- wget -qO- http://localhost:8080/readyz

# Verify controller logs
kubectl logs -n aura-system -l app.kubernetes.io/component=controller --tail=20

# Check CRDs are functional
kubectl get pt --all-namespaces
```

### 6. Authenticate with CLI

```bash
aura-power login --server https://power.your-domain.com
# Username: admin
# Password: (the password you set above)
```

---

## Downgrade and rollback

Do not run a direct `helm rollback` from v2.2 or later to v2.1.7 or older.
Those controllers do not understand `scope.targetRefs` or workload UIDs. A rule
that contains only `targetRefs` is decoded by v2.1.7 as an empty scope, which
matches every discovered workload. The newer controller also replaces legacy
PowerTarget names with UID-bound names, so starting the old controller before
restoration can delete the only persisted snapshots.

Use the fail-closed preparation command while the newer release is still
healthy. The backup directory must be on durable, access-controlled storage and
must be reused if the command is repeated:

```bash
mkdir -p ./aura-power-downgrade-backup
chmod 700 ./aura-power-downgrade-backup
EXPECTED_CONTEXT=aura-power-quality-eks-operations
test "$(kubectl config current-context)" = "$EXPECTED_CONTEXT"

./scripts/release/prepare-safe-downgrade.sh \
  --expected-context "$EXPECTED_CONTEXT" \
  --namespace aura-system \
  --release aura-power \
  --backup-dir ./aura-power-downgrade-backup
```

Set `EXPECTED_CONTEXT` to the independently known context for the intended
cluster. Do not derive it from `kubectl config current-context`; that would make
the wrong-cluster guard compare the active context with itself.

The preparation first stops the Aura Power server so API clients cannot change
rules through the product. It captures the complete rule set and each
`resourceVersion`, saves the original policies, overrides, and targets, then
uses UID/resourceVersion-conditional JSON Patches through the live admission
webhook to quarantine every inventoried rule. It stops the controller,
re-inventories the rule set, and restores each available snapshot. Restoration
also uses UID and powered-down-state preconditions. The command aborts instead
of overwriting a concurrent rule edit, a rule created after inventory, a
recreated workload, or a concurrent scale change. The final
`safe_downgrade_prepared=true` line is required before continuing.

Keep the upgraded CRDs installed. They are backward-readable, while downgrading
the CRDs themselves could prune the UID and snapshot fields needed for recovery.
Render and inspect the old chart, then deploy it with its controller held at
zero. Prefer the immutable chart artifact for the destination version; the path
below is illustrative:

```bash
helm template aura-power ./aura-power-v2.1.7/charts/aura-power \
  --namespace aura-system \
  --set controller.replicas=0 > rendered-v2.1.7.yaml

helm upgrade aura-power ./aura-power-v2.1.7/charts/aura-power \
  --namespace aura-system \
  --reuse-values \
  --set controller.replicas=0 \
  --wait --timeout 8m
```

Confirm that every workload recorded with an available snapshot in
`powertargets/*.json` still has the same UID and restored replica or suspension
state. Then start the legacy controller while all rules remain quarantined:

```bash
kubectl scale deployment aura-power-controller \
  --namespace aura-system --replicas=1
kubectl rollout status deployment aura-power-controller \
  --namespace aura-system --timeout=5m
```

Observe at least one discovery and reconciliation interval and verify that no
workload changed. Review rules one at a time before restoring them. A saved rule
with `spec.scope.targetRefs` cannot be enabled on v2.1.7; translate it to a
legacy selector and verify the preview first. For a rule that is already
legacy-compatible, restore only its original spec and remove the quarantine
annotation:

```bash
rule=example-policy
backup=./aura-power-downgrade-backup/powerpolicies/${rule}.json
kubectl patch powerpolicy "$rule" --namespace aura-system --type=merge \
  --patch "$(jq '{spec:.spec,metadata:{annotations:{"power.aura.sh/downgrade-quarantined":null}}}' "$backup")"
```

If preparation detects an inventory or resourceVersion conflict, it leaves both
the server and controller at zero and keeps the original backup unchanged. Do
not delete the backup. Inspect the reported resource and decide whether the
concurrent version or the saved version is authoritative. After removing an
unwanted newly-created rule (DELETE remains available while the webhook is
stopped), start the controller temporarily to restore admission, resolve any
intentional update, then start the server and retry with a new backup directory.
For other failures, keep both writers stopped and resolve the reported missing
workload, UID mismatch, incomplete snapshot, or concurrent state change before
retrying.

---

## Post-Upgrade

### Enable metrics collection

```bash
helm upgrade aura-power ./charts/aura-power \
  -n aura-system \
  --reuse-values \
  --set serviceMonitor.enabled=true
```

### Configure Prometheus/OpenCost integration

```bash
helm upgrade aura-power ./charts/aura-power \
  -n aura-system \
  --reuse-values \
  --set server.prometheus.url=http://prometheus.monitoring.svc:9090 \
  --set server.opencost.url=http://opencost.opencost.svc:9003
```

### Set up Gateway API (optional)

```bash
helm upgrade aura-power ./charts/aura-power \
  -n aura-system \
  --reuse-values \
  --set server.gateway.enabled=true \
  --set server.gateway.gatewayRef.name=your-gateway \
  --set server.gateway.gatewayRef.namespace=gateway-ns \
  --set "server.gateway.hostnames[0]=power.your-domain.com"
```

---

## FAQ

**Q: Will my existing PowerTargets be lost?**
A: Upgrades preserve the data in etcd and migrate targets to UID-bound names.
Downgrades require the procedure above because v2.1.7 recreates legacy target
names and cannot preserve the newer identity contract.

**Q: Do I need to recreate policies?**
A: Existing v1/v2.1 selector-based policies remain compatible when upgrading.
Policies that use v2.2 `targetRefs` must stay quarantined during a downgrade to
v2.1.7 until they are translated and reviewed.

**Q: What happens to the old single-binary deployment?**
A: Helm will replace it with the new server StatefulSet + controller Deployment.

**Q: Is the SQLite data persisted?**
A: Yes. The server uses a PVC (1Gi by default). Auth data and audit logs persist across restarts.
