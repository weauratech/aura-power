# Custom Resource Definitions Reference

Aura Power uses five Custom Resource Definitions (CRDs) in the `power.aura.sh/v1alpha1` API group.

## PowerTarget

Represents a workload under Aura Power management. Created automatically by the controller during discovery.

**Short name**: `pt`

### Spec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `spec.targetRef.namespace` | string | yes | Namespace of the workload |
| `spec.targetRef.name` | string | yes | Name of the workload |
| `spec.targetRef.kind` | string | yes | Kind: `Deployment`, `StatefulSet`, or `CronJob` |

### Status (computed by controller)

| Field | Type | Description |
|-------|------|-------------|
| `status.observedState.replicas` | int32 | Current replica count |
| `status.observedState.powerState` | string | `on` or `off` |
| `status.desiredState` | string | Effective desired state (`on`, `off`, or empty) |
| `status.managed` | bool | At least one policy/override governs this target |
| `status.divergent` | bool | Observed state differs from desired |
| `status.blocked` | bool | Guardrail is preventing action |
| `status.winningRule` | object | Rule that determined the desired state |
| `status.snapshot` | object | Captured state for restoration |
| `status.savings` | object | Accumulated savings (CPU hours, memory GiB-hours, estimated cost) |
| `status.consecutiveFailures` | int | Consecutive reconcile failures |
| `status.lastReconciliation` | timestamp | Last reconciliation time |

### Example

```yaml
apiVersion: power.aura.sh/v1alpha1
kind: PowerTarget
metadata:
  name: my-app
  namespace: staging
spec:
  targetRef:
    namespace: staging
    name: my-app
    kind: Deployment
```

---

## PowerPolicy

Defines a recurring power schedule for workloads. Policies use scope selectors and time windows to determine when workloads should be on or off.

**Short name**: `pp`

### Spec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `spec.scope` | object | yes | Workload targeting criteria |
| `spec.scope.namespaces` | []string | no | Target specific namespaces |
| `spec.scope.namespaceGroups` | []string | no | Reference PowerNamespaceGroup names |
| `spec.scope.namespaceLabels` | map | no | Select namespaces by labels |
| `spec.scope.workloadNames` | []string | no | Target specific workload names |
| `spec.scope.workloadLabels` | map | no | Select workloads by labels |
| `spec.schedule` | object | yes | Time-based behavior |
| `spec.schedule.desiredState` | string | yes | `on` or `off` during windows |
| `spec.schedule.windows` | []object | no | Time windows (empty = always active) |
| `spec.priority` | int32 | no | Priority for conflict resolution (higher wins, 0-1000) |
| `spec.description` | string | no | Human-readable explanation |

### Time Window

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `start` | string | yes | Start time in `HH:MM` format |
| `end` | string | yes | End time in `HH:MM` format (wraps past midnight if start > end) |
| `days` | []int | no | Days of week (0=Sunday, 6=Saturday). Empty = every day |
| `timezone` | string | yes | IANA timezone (e.g., `America/Sao_Paulo`) |

### Example: Power off staging at night

```yaml
apiVersion: power.aura.sh/v1alpha1
kind: PowerPolicy
metadata:
  name: staging-night-off
  namespace: aura-system
spec:
  scope:
    namespaces:
      - staging
      - dev
  schedule:
    desiredState: "off"
    windows:
      - start: "20:00"
        end: "08:00"
        timezone: "America/Sao_Paulo"
        days: [1, 2, 3, 4, 5]  # Mon-Fri
  priority: 100
  description: "Power off non-production at night"
```

### Example: Weekend shutdown

```yaml
apiVersion: power.aura.sh/v1alpha1
kind: PowerPolicy
metadata:
  name: weekend-off
  namespace: aura-system
spec:
  scope:
    workloadLabels:
      environment: non-production
  schedule:
    desiredState: "off"
    windows:
      - start: "00:00"
        end: "23:59"
        timezone: "UTC"
        days: [0, 6]  # Saturday and Sunday
  priority: 50
  description: "Shut down non-production on weekends"
```

---

## PowerOverride

Defines a temporary exception to power policies. Overrides have mandatory expiration and justification.

**Short name**: `po`

### Spec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `spec.scope` | object | yes | Same scope model as PowerPolicy |
| `spec.state` | string | yes | `on` or `off` |
| `spec.priority` | int32 | yes | Priority (higher wins against policies) |
| `spec.expiresAt` | timestamp | yes | Mandatory expiration time |
| `spec.reason` | string | yes | Justification (min 3 characters) |
| `spec.reference` | string | no | External ticket/incident link |

### Status

| Field | Type | Description |
|-------|------|-------------|
| `status.phase` | string | `Active` or `Expired` |
| `status.affectedTargets` | int32 | Number of targets affected |
| `status.expiresIn` | string | Human-readable duration until expiration |

### Example: Keep workload on for deployment

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
  expiresAt: "2026-08-01T06:00:00Z"
  reason: "Deployment window for v3.2 release"
  reference: "JIRA-1234"
```

---

## PowerSchedule

A named, reusable schedule that can be referenced via namespace annotations. Provides a DRY approach for common schedules.

**Short name**: `ps`

### Spec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `spec.desiredState` | string | yes | `on` or `off` during windows |
| `spec.windows` | []object | no | Same TimeWindow model as PowerPolicy |
| `spec.description` | string | no | Human-readable explanation |

### Example

```yaml
apiVersion: power.aura.sh/v1alpha1
kind: PowerSchedule
metadata:
  name: business-hours
  namespace: aura-system
spec:
  desiredState: "on"
  windows:
    - start: "08:00"
      end: "18:00"
      timezone: "America/Sao_Paulo"
      days: [1, 2, 3, 4, 5]
  description: "Keep workloads on during business hours only"
```

---

## PowerNamespaceGroup

Groups namespaces under a single name for reuse across multiple policies.

**Short name**: `png`

### Spec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `spec.namespaces` | []string | yes | List of namespace names |

### Example

```yaml
apiVersion: power.aura.sh/v1alpha1
kind: PowerNamespaceGroup
metadata:
  name: non-production
  namespace: aura-system
spec:
  namespaces:
    - dev
    - staging
    - qa
    - sandbox
```

Then reference it in a policy:

```yaml
spec:
  scope:
    namespaceGroups:
      - non-production
```

---

## Priority Resolution

When multiple rules match a target:

1. Higher `priority` value wins
2. If tied, `PowerOverride` wins over `PowerPolicy`
3. If still tied, the most recently created rule wins

The winning rule is recorded in `status.winningRule` and all others in `status.suppressedRules`.

## Guardrails

The controller detects external management (ArgoCD, Flux, HPA) and blocks actions that would conflict. Blocked targets show `status.blocked: true` with details in `status.blockReasons`.
