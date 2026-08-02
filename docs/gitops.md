# GitOps Coexistence Guide

Aura Power operates safely alongside GitOps tools (ArgoCD, Flux, Helm) without creating sync conflicts — when configured correctly.

## How Aura Power Interacts with Workloads

When Aura Power powers down a workload, it:
1. Captures a snapshot of the current state (replica count)
2. Scales `spec.replicas` to 0 (Deployments/StatefulSets) or sets `spec.suspend: true` (CronJobs)
3. Annotates the workload with `aura.sh/last-action` and `aura.sh/snapshot-ref`

This means the live state of `spec.replicas` will differ from what's in Git — which triggers GitOps sync warnings.

## ArgoCD

### The Problem

ArgoCD detects that `spec.replicas` differs from the Git source and marks the Application as **OutOfSync**. If auto-sync is enabled, ArgoCD will immediately revert the scale-down.

### Solution: ignoreDifferences

Add `ignoreDifferences` to your ArgoCD Application spec for workloads managed by Aura Power:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: my-app
spec:
  # ... source, destination, etc.
  ignoreDifferences:
    - group: apps
      kind: Deployment
      jsonPointers:
        - /spec/replicas
    - group: apps
      kind: StatefulSet
      jsonPointers:
        - /spec/replicas
    - group: batch
      kind: CronJob
      jsonPointers:
        - /spec/suspend
```

This tells ArgoCD to ignore replica count changes made by Aura Power.

### Per-Resource Override (more granular)

If you only want specific workloads to be managed by Aura Power:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: my-app
spec:
  ignoreDifferences:
    - group: apps
      kind: Deployment
      name: payment-service
      namespace: staging
      jsonPointers:
        - /spec/replicas
```

### Opt-In Annotation

Aura Power blocks power-down of ArgoCD-managed workloads by default (guardrail). To opt in a workload:

```bash
kubectl annotate deployment my-app -n staging aura.sh/power-eligible="true"
```

Or in your Git manifest:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-app
  namespace: staging
  annotations:
    aura.sh/power-eligible: "true"
```

### Recommended Setup

1. Add `ignoreDifferences` for `/spec/replicas` in your ArgoCD Application
2. Add `aura.sh/power-eligible: "true"` to workloads you want Aura Power to manage
3. Keep auto-sync enabled — ArgoCD will sync everything except replica count

## Flux

### The Problem

Flux's Kustomize Controller or Helm Controller will reconcile workloads back to their Git-defined state, reverting Aura Power's scale-down.

### Solution: Suspend Reconciliation per Resource

Flux supports field-level ignore via `spec.patches` in Kustomization:

```yaml
apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: my-app
spec:
  # ... source, path, etc.
  patches:
    - target:
        kind: Deployment
        name: my-app
      patch: |
        - op: remove
          path: /spec/replicas
```

This removes `spec.replicas` from the Flux-applied manifest, letting Aura Power control it.

### Alternative: Annotation-Based Exclusion

For Flux Helm releases, you can exclude specific fields using the `driftDetection` feature:

```yaml
apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: my-app
spec:
  driftDetection:
    mode: enabled
    ignore:
      - paths: ["/spec/replicas"]
        target:
          kind: Deployment
```

### Opt-In

Same as ArgoCD — add the annotation to the workload:

```bash
kubectl annotate deployment my-app -n staging aura.sh/power-eligible="true"
```

Aura Power blocks Flux-managed workloads by default (detected via `app.kubernetes.io/managed-by: Helm` or Flux labels).

## Helm-Managed Workloads

Helm doesn't have a continuous reconciliation loop — it only applies changes on `helm upgrade`. However, workloads with `app.kubernetes.io/managed-by: Helm` are blocked by default.

### Opt-In

```bash
kubectl annotate deployment my-app -n staging aura.sh/power-eligible="true"
```

No additional Helm configuration is needed since Helm won't revert the replica count until the next `helm upgrade`.

## HPA-Managed Workloads

Workloads with an active Horizontal Pod Autoscaler are blocked by default. If Aura Power scales replicas to 0, the HPA has no effect. On restore, the HPA immediately takes over and scales to the appropriate level.

### Opt-In

```bash
kubectl annotate deployment my-app -n staging aura.sh/power-eligible="true"
```

When opted in:
- **Power-down**: Aura Power scales to 0, HPA becomes ineffective (min replicas = 0 pods)
- **Restore**: Aura Power restores the snapshot replica count, HPA adjusts from there

## Summary

| Tool | Default Behavior | Opt-In | Additional Config |
|------|-----------------|--------|-------------------|
| ArgoCD | Blocked | `aura.sh/power-eligible: "true"` | `ignoreDifferences` on `/spec/replicas` |
| Flux | Blocked | `aura.sh/power-eligible: "true"` | Remove `/spec/replicas` from patch or use driftDetection ignore |
| Helm | Blocked | `aura.sh/power-eligible: "true"` | None needed |
| HPA | Blocked | `aura.sh/power-eligible: "true"` | None needed |
| None | Eligible | Already eligible | N/A |

## Verifying Guardrail Status

Check which workloads are blocked and why:

```bash
# Via panel: Blocked page
# Via CLI:
aura-power status --server https://power.your-domain.com | grep blocked

# Via kubectl:
kubectl get powertargets -n aura-system -o jsonpath='{range .items[?(@.status.blocked==true)]}{.spec.targetRef.namespace}/{.spec.targetRef.name}: {.status.blockReasons[*].message}{"\n"}{end}'
```

## Excluding Workloads Permanently

To permanently exclude a workload from all Aura Power governance:

```bash
kubectl annotate deployment my-critical-app -n production aura.sh/power-exempt="true"
```

Exempt workloads are never discovered, never shown in targets, and never affected by policies.
