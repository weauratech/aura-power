# Quick Start

Get Aura Power running and your first schedule active in under 10 minutes.

## Prerequisites

- Kubernetes cluster (1.28+)
- Helm 3.x
- `kubectl` configured with cluster access

## 1. Install

```bash
helm install aura-power oci://ghcr.io/weauratech/charts/aura-power \
  --namespace aura-system --create-namespace \
  --set server.auth.jwtSecret=$(openssl rand -base64 32) \
  --set server.auth.initialAdmin.password=changeme
```

Wait for pods to be ready:

```bash
kubectl get pods -n aura-system -w
```

You should see:
```
aura-power-server-0         1/1   Running
aura-power-controller-...   1/1   Running
```

## 2. Access the Panel

Port-forward to access the web panel:

```bash
kubectl port-forward -n aura-system svc/aura-power-server 8080:8080
```

Open [http://localhost:8080](http://localhost:8080) and login:
- **Username**: `admin`
- **Password**: the password you set above (`changeme`)

You'll see the **Discovery Mode** banner showing how many workloads were found.

## 3. Explore Your Cluster

Before creating any policy, browse the **Targets** page to see what Aura Power discovered:

- Total workloads per namespace
- Which ones are blocked by guardrails (ArgoCD, HPA, Helm-managed)
- Current power state of each workload

You can also use the CLI:

```bash
# Install CLI
go install github.com/weauratech/aura-power/cmd/aura-power@latest

# Or download from GitHub Releases
# https://github.com/weauratech/aura-power/releases

# View status
aura-power status --server http://localhost:8080
```

## 4. Create Your First Schedule

Navigate to **Schedules** and click **New Schedule**, or use the CLI:

### Via Panel

1. Click **New Schedule** (top right)
2. Enter a name: `dev-night-off`
3. Select scope: choose your `dev` or `staging` namespace
4. Set desired state: **Off**
5. Set window: `20:00` to `08:00`
6. Select days: Mon-Fri
7. Click **Preview Impact** to see how many targets will be affected
8. Click **Create Schedule**

### Via kubectl

```yaml
apiVersion: power.aura.sh/v1alpha1
kind: PowerPolicy
metadata:
  name: dev-night-off
  namespace: aura-system
spec:
  scope:
    namespaces:
      - dev
      - staging
  schedule:
    desiredState: "off"
    windows:
      - start: "20:00"
        end: "08:00"
        timezone: "America/Sao_Paulo"
        days: [1, 2, 3, 4, 5]
  priority: 100
  description: "Power off dev/staging outside business hours"
```

```bash
kubectl apply -f policy.yaml
```

## 5. Verify

After the next reconciliation cycle (~30 seconds):

```bash
# Check targets status
kubectl get powertargets -n aura-system

# Or via CLI
aura-power status --server http://localhost:8080
```

In the panel, the Dashboard will update with powered-off targets and savings accumulating.

## 6. Create a Temporary Override (optional)

Need to keep a workload running during scheduled off-hours? Create an override:

### Via Panel

1. Go to **Schedules** → **New Schedule**
2. Toggle **Temporary override**
3. Select the target namespace/workload
4. Set state: **On**
5. Set expiration: 4 hours
6. Enter reason: "Deployment window for release"
7. Click **Create Override**

### Via kubectl

```yaml
apiVersion: power.aura.sh/v1alpha1
kind: PowerOverride
metadata:
  name: deploy-window
  namespace: aura-system
spec:
  scope:
    namespaces:
      - staging
    workloadNames:
      - payment-service
  state: "on"
  priority: 500
  expiresAt: "2024-01-15T06:00:00Z"
  reason: "Release deployment window"
  reference: "JIRA-1234"
```

The override auto-expires — no cleanup needed.

## 7. Monitor Savings

Check the **Savings** page to see accumulated savings:
- CPU hours saved
- Memory GiB-hours saved
- Estimated cost reduction

## Next Steps

- [CRD Reference](crds.md) — full specification of all custom resources
- [API Reference](api-reference.md) — REST API documentation
- [GitOps Guide](gitops.md) — running alongside ArgoCD or Flux
- [Troubleshooting](troubleshooting.md) — common issues and solutions
- [Upgrade Guide](upgrade-v2.md) — migrating from v1.x

## Uninstall

```bash
helm uninstall aura-power -n aura-system
# CRDs are NOT removed automatically (data safety)
# To remove CRDs and all data:
kubectl delete crd powertargets.power.aura.sh powerpolicies.power.aura.sh \
  poweroverrides.power.aura.sh powerschedules.power.aura.sh \
  powerauditevents.power.aura.sh powernamespacegroups.power.aura.sh
```
