# GitOps Coexistence Guide

Aura Power operates safely alongside GitOps tools (ArgoCD, Flux, Helm) without creating sync conflicts — when configured correctly.

## How Aura Power Interacts with Workloads

When Aura Power powers down a workload, it:
1. Captures a snapshot of the current state (replica count)
2. Scales `spec.replicas` to 0 (Deployments/StatefulSets) or sets `spec.suspend: true` (CronJobs)
3. Records the snapshot and durable action state on the corresponding `PowerTarget`

This means the live state of `spec.replicas` will differ from what's in Git — which triggers GitOps sync warnings.

## ArgoCD

### The Problem

ArgoCD detects that `spec.replicas` differs from the Git source and marks the Application as **OutOfSync**. If auto-sync is enabled, ArgoCD will immediately revert the scale-down.

### Supported field-ownership contract

Aura Power supports Argo CD coexistence only when the Application declares both
parts of the field-ownership contract:

1. `ignoreDifferences` delegates `/spec/replicas` or `/spec/suspend` to Aura Power.
2. `RespectIgnoreDifferences=true` prevents sync and self-heal from applying those
   delegated fields back to the Git value.

Configure both on the Application (or on `spec.template.spec` in the
ApplicationSet that generates it):

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
  syncPolicy:
    syncOptions:
      - RespectIgnoreDifferences=true
```

`ignoreDifferences` by itself only changes diff calculation and is not a
supported configuration for self-heal. Complete Application and ApplicationSet
examples are available in [`examples/gitops/argocd`](../examples/gitops/argocd).

Argo CD applies `RespectIgnoreDifferences` only after the resource exists. Keep
the replica/suspend value in Git for initial creation, allow the first sync to
create the resource, and only then activate an Aura off policy.

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
  syncPolicy:
    syncOptions:
      - RespectIgnoreDifferences=true
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

1. Add `ignoreDifferences` for the Aura-owned field in the Application or ApplicationSet source.
2. Add `RespectIgnoreDifferences=true` to `syncPolicy.syncOptions` in the same source.
3. Sync once and confirm that the workload exists before activating an off policy.
4. Add `aura.sh/power-eligible: "true"` to workloads you want Aura Power to manage.
5. Verify an unrelated Git change, such as an image update, still syncs.

Aura Power persists an action intent before changing a workload. After one
accepted write, a repeated divergence for the same desired state is reported as
`status.action.phase: Contended`; the controller observes the resource without
issuing the same mutation repeatedly. Correct the Application/ApplicationSet
field ownership, then explicitly authorize one retry by changing the target's
retry token:

```bash
kubectl annotate powertarget -n aura-system TARGET_NAME \
  power.aura.sh/retry-action="$(date +%s)" --overwrite
```

`Applied` means the Kubernetes API accepted Aura's write; it does not yet mean
the discovery loop observed the desired state. A fast self-heal can therefore
move the operation from `Applied` to `Contended` on the next observation. A
normal transition moves from `Applied` to `Converged` after discovery observes
the delegated field.

### CronJob restoration semantics

Aura Power snapshots the effective value of `spec.suspend` before changing a
CronJob and restores that exact value. An omitted `spec.suspend` uses the
Kubernetes default `false`, so its snapshot is restored as `false`. Restoration
fails without changing the CronJob when the snapshot has no suspend value or
when the workload UID differs from the UID captured by discovery. Repeated
restoration of the same snapshot is idempotent.

Suspension only stops new schedules. It does not stop Jobs that the CronJob has
already created; their count is exposed as
`PowerTarget.status.observedState.activeJobs`. Aura Power does not delete,
cancel, or restart those Jobs.

Kubernetes counts schedules that occur while a CronJob is suspended as missed.
When the CronJob is resumed, jobs can be created immediately according to its
`startingDeadlineSeconds` and the CronJob controller's missed-schedule rules.
Set `startingDeadlineSeconds`, concurrency policy, and job history limits in the
CronJob manifest to express the intended catch-up behavior. Aura Power preserves
those fields and does not promise that a suspended window will be skipped.

For GitOps-managed CronJobs, delegate `/spec/suspend` with both
`ignoreDifferences` and `RespectIgnoreDifferences=true`. Git remains authoritative
for schedule and catch-up fields; Aura Power is temporarily authoritative only
for `spec.suspend`. The same rule applies when restoring an originally suspended
CronJob: Git must also declare the intended baseline or it can overwrite the
restored value.

### Reproducing the native compatibility gate

`scripts/quality/kind-argocd-journey.sh` runs the supported contract against a
disposable Kind cluster. The runner installs Argo CD v2.14.20 from a manifest
with a pinned SHA-256, serves a two-revision Git fixture inside the cluster and
tests manual sync, automated self-heal, `ignoreDifferences` with and without
`RespectIgnoreDifferences=true`, and an ApplicationSet-generated Application.
The workload matrix contains a Deployment, StatefulSet and CronJob. The CronJob
case retains an existing Job, observes a schedule while suspended and verifies
catch-up after resume.

```bash
export KUBECONFIG=/path/to/aura-power-quality.kubeconfig
./scripts/quality/kind-argocd-journey.sh
```

The runner refuses any context other than `kind-aura-power-quality` by default,
uses run-scoped names and labels, and verifies cleanup. `KIND_CLUSTER_NAME` may
name another disposable Kind cluster for an isolated local run.

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

Aura Power discovers `autoscaling/v2` Horizontal Pod Autoscalers by their exact
`scaleTargetRef` (`apiVersion`, `kind`, `name`, and namespace). Deployments and
StatefulSets with an active HPA are blocked by default.

Immediately before power-down, the controller repeats the namespace HPA lookup
through an uncached API-server reader. When it finds an HPA, it also reads the
exact UID-bound workload and Namespace and recomputes opt-in from their current
annotations. A newly-created HPA or removed opt-in therefore blocks that attempt.
Any HPA LIST, workload GET, Namespace GET, or UID verification failure blocks
the attempt; cached state is never treated as current safety evidence.

### Opt-In

```bash
kubectl annotate deployment my-app -n staging aura.sh/power-eligible="true"
```

When opted in:
- **Power-down**: Aura Power snapshots the current live replica count and makes one scale-to-zero transition.
- **Power-down hold**: the standard HPA algorithm disables scaling while current replicas are zero and `minReplicas` is positive, so the target remains off.
- **Restore**: Aura Power restores the snapshot replica count, after which HPA resumes evaluation and may adjust positive replicas from live metrics.
- **Contention**: if another scale writer reactivates the target during the off policy, Aura Power reports `Contended` and does not enter a write loop.

Opt-in delegates a shared field to two controllers and must be deliberate. The
validated contract covers the standard Kubernetes HPA zero-replica hold and
snapshot restore. KEDA scale-to-zero, custom scale targets, and external systems
that can activate a zero-replica target are separate integrations and are not
covered by this contract.

## Summary

| Tool | Default Behavior | Opt-In | Additional Config |
|------|-----------------|--------|-------------------|
| ArgoCD | Blocked | `aura.sh/power-eligible: "true"` | `ignoreDifferences` plus `RespectIgnoreDifferences=true` |
| Flux | Blocked | `aura.sh/power-eligible: "true"` | Remove `/spec/replicas` from patch or use driftDetection ignore |
| Helm | Blocked | `aura.sh/power-eligible: "true"` | None needed |
| HPA (`autoscaling/v2`, Deployment/StatefulSet) | Blocked | `aura.sh/power-eligible: "true"` | Standard HPA holds at zero; verify separately when another autoscaler can activate from zero |
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
