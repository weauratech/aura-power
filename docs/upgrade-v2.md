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
```

### 2. Update CRDs

v2.0 includes CRDs in the Helm chart `crds/` directory. Helm installs CRDs on first install but does not upgrade them automatically.

Apply the new CRDs manually:

```bash
kubectl apply -f charts/aura-power/crds/
```

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
kubectl exec -n aura-system deploy/aura-power-server -- wget -qO- http://localhost:8080/readyz

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

## Rollback

If issues occur:

```bash
helm rollback aura-power -n aura-system
```

CRD changes are additive (new fields only) and backward-compatible.

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
A: No. CRD data persists in etcd. The new controller will pick up existing targets.

**Q: Do I need to recreate policies?**
A: No. Existing PowerPolicies are fully compatible with v2.0.

**Q: What happens to the old single-binary deployment?**
A: Helm will replace it with the new server StatefulSet + controller Deployment.

**Q: Is the SQLite data persisted?**
A: Yes. The server uses a PVC (1Gi by default). Auth data and audit logs persist across restarts.
