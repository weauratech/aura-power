# Architecture

Aura Power follows a split architecture with two independently deployable components communicating through Kubernetes CRDs.

## Components

```mermaid
flowchart TD
    subgraph "User Layer"
        Browser["Web Panel (Browser)"]
        CLI["aura-power CLI"]
    end

    subgraph "Server (StatefulSet)"
        API["REST API (Gin)"]
        Auth["JWT Auth + SQLite"]
        Panel["Embedded SPA"]
        PromProxy["Prometheus Proxy"]
    end

    subgraph "Controller (Deployment)"
        Reconciler["Target Reconciler"]
        Discovery["Discovery Loop"]
        Audit["Audit Recorder"]
        Engine["Decision Engine (Pure Domain)"]
    end

    subgraph "Kubernetes"
        CRDs["CRDs (etcd)"]
        Workloads["Deployments / StatefulSets / CronJobs"]
        Events["K8s Events"]
    end

    subgraph "External"
        Prometheus["Prometheus"]
        OpenCost["OpenCost"]
    end

    Browser --> Panel
    Browser --> API
    CLI --> API
    API --> CRDs
    API --> PromProxy
    PromProxy --> Prometheus
    PromProxy --> OpenCost

    Controller -- "watch + reconcile" --> CRDs
    Reconciler --> Engine
    Reconciler --> Workloads
    Reconciler --> Audit
    Discovery --> Workloads
    Discovery --> CRDs
    Audit --> CRDs
    Audit --> Events
```

## Data Flow

### Reconciliation Cycle (every 30s)

```mermaid
sequenceDiagram
    participant C as Controller
    participant K as K8s API (CRDs)
    participant E as Decision Engine
    participant W as Workloads

    C->>K: Watch PowerTarget changes
    C->>K: List PowerPolicies
    C->>K: List PowerOverrides
    C->>E: Evaluate(target, policies, overrides)
    E-->>C: Decision{state, winningRule, blocked}
    alt state=off AND observed=on
        C->>W: Scale replicas to 0
        C->>K: Create PowerAuditEvent
    end
    alt state=on AND observed=off
        C->>W: Restore from snapshot
        C->>K: Create PowerAuditEvent
    end
    C->>K: Update PowerTarget status
```

### Authentication Flow

```mermaid
sequenceDiagram
    participant B as Browser
    participant S as Server API
    participant DB as SQLite

    B->>S: POST /auth/login {user, pass}
    S->>DB: Validate credentials
    DB-->>S: User record
    S->>S: Generate JWT (access + refresh)
    S-->>B: Set-Cookie: aura_session (HttpOnly)
    B->>S: GET /api/v1/targets (Cookie sent)
    S->>S: Validate JWT from cookie
    S->>S: Check RBAC role
    S-->>B: 200 + data
```

## CRD Lifecycle

| CRD | Created By | Updated By | Read By |
|-----|-----------|------------|---------|
| PowerTarget | Controller (discovery) | Controller (reconciler) | Server API |
| PowerPolicy | Server API / kubectl | Server API / kubectl | Controller |
| PowerOverride | Server API / kubectl | Controller (phase=Expired) | Controller |
| PowerAuditEvent | Controller | — | Server API |
| PowerSchedule | Server API / kubectl | — | Controller |
| PowerNamespaceGroup | Server API / kubectl | — | Controller |

## Security Model

- **Server RBAC**: Read/write to Aura Power CRDs only. Read workload metadata.
- **Controller RBAC**: Read/write CRDs. Patch workloads (scale). Create Events.
- **User RBAC**: Three roles (member/approver/admin) enforced at API layer via JWT claims.
- **No cluster-admin required**: Both components use least-privilege ServiceAccounts.

## Deployment Topology

```
Namespace: aura-system
├── StatefulSet/aura-power-server (1 replica)
│   ├── PVC: SQLite (auth + audit metadata)
│   ├── Port 8080: API + Panel
│   └── Serves: /healthz, /readyz, /metrics
├── Deployment/aura-power-controller (1 replica, leader election)
│   ├── Stateless (all state in CRDs)
│   ├── Port 8081: health probes
│   └── Reconciles: PowerTargets, PowerPolicies, PowerOverrides
└── CRDs: 6 custom resources in power.aura.sh/v1alpha1
```

## Decision Engine (Pure Domain)

The decision engine is a pure function with no I/O dependencies:

```
Input:  Target + []Policies + []Overrides + time.Now()
Output: Decision{DesiredState, WinningRule, SuppressedRules, Blocked, BlockReasons}
```

Rules:
1. Filter policies/overrides that match the target's scope
2. Evaluate time windows against current time
3. Sort by priority (descending)
4. Highest priority wins
5. If tied priority: Override > Policy > "on" (safety-first)
6. Check guardrails (system namespace, ArgoCD, Flux, HPA, Helm)
7. If blocked: set Blocked=true with reasons
