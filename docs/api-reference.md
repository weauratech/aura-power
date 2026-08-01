# API Reference

The Aura Power server exposes a REST API on port 8080. All endpoints under `/api/v1` require JWT authentication (except login/refresh).

## Authentication

### POST /api/v1/auth/login

Authenticate and receive access/refresh tokens as HttpOnly cookies.

**Request**:
```json
{
  "username": "admin",
  "password": "your-password"
}
```

**Response** (200):
```json
{
  "user": {
    "id": "uuid",
    "username": "admin",
    "role": "admin"
  }
}
```

Cookies set: `access_token` (1h TTL), `refresh_token` (7d TTL).

### POST /api/v1/auth/refresh

Refresh the access token using the refresh cookie.

**Response** (200): New `access_token` cookie set.

### POST /api/v1/auth/logout

Clear authentication cookies.

### GET /api/v1/auth/me

Get current authenticated user info.

**Response** (200):
```json
{
  "id": "uuid",
  "username": "admin",
  "role": "admin"
}
```

---

## Targets

### GET /api/v1/targets

List all PowerTargets with their computed status.

**Response** (200):
```json
[
  {
    "namespace": "staging",
    "name": "my-app",
    "kind": "Deployment",
    "desiredState": "off",
    "observedState": "on",
    "managed": true,
    "divergent": true,
    "blocked": false,
    "winningRule": { "kind": "PowerPolicy", "name": "night-off", "priority": 100 }
  }
]
```

### GET /api/v1/targets/:namespace/:name/explain

Get a detailed explanation of why a target is in its current state, including all matching rules and priority resolution.

**Response** (200):
```json
{
  "target": { "namespace": "staging", "name": "my-app", "kind": "Deployment" },
  "desiredState": "off",
  "winningRule": { ... },
  "suppressedRules": [ ... ],
  "blockReasons": [],
  "explanation": "PowerPolicy 'night-off' (priority 100) determines state=off"
}
```

---

## Status

### GET /api/v1/status

Get cluster-wide power status summary.

**Response** (200):
```json
{
  "totalTargets": 42,
  "managed": 38,
  "poweredOff": 15,
  "poweredOn": 23,
  "divergent": 2,
  "blocked": 1
}
```

---

## Discovery

### GET /api/v1/discover

Trigger workload discovery and return discovered workloads.

---

## Policies

### GET /api/v1/policies

List all PowerPolicies.

### POST /api/v1/policies

Create a new PowerPolicy. Requires `approver` or `admin` role.

**Request**:
```json
{
  "name": "night-off",
  "namespace": "aura-system",
  "spec": {
    "scope": { "namespaces": ["staging"] },
    "schedule": {
      "desiredState": "off",
      "windows": [{ "start": "20:00", "end": "08:00", "timezone": "UTC" }]
    },
    "priority": 100
  }
}
```

### PUT /api/v1/policies/:namespace/:name

Update an existing PowerPolicy. Requires `approver` or `admin` role.

### DELETE /api/v1/policies/:namespace/:name

Delete a PowerPolicy. Requires `admin` role.

---

## Overrides

### GET /api/v1/overrides

List all PowerOverrides.

### POST /api/v1/overrides

Create a new PowerOverride. Requires `approver` or `admin` role.

**Request**:
```json
{
  "name": "hotfix-deploy",
  "namespace": "aura-system",
  "spec": {
    "scope": { "namespaces": ["staging"], "workloadNames": ["payment-svc"] },
    "state": "on",
    "priority": 500,
    "expiresAt": "2026-08-01T06:00:00Z",
    "reason": "Hotfix deployment window"
  }
}
```

### DELETE /api/v1/overrides/:namespace/:name

Delete a PowerOverride. Requires `admin` role.

---

## Savings

### GET /api/v1/savings

Get total accumulated savings across all targets.

**Response** (200):
```json
{
  "totalCPUHoursSaved": 1250.5,
  "totalMemoryGiBHoursSaved": 890.2,
  "totalEstimatedCost": 45.80
}
```

### GET /api/v1/savings/breakdown

Get per-target savings breakdown.

---

## Namespace Groups

### GET /api/v1/namespace-groups

List all PowerNamespaceGroups.

### POST /api/v1/namespace-groups

Create a new namespace group. Requires `approver` or `admin` role.

### DELETE /api/v1/namespace-groups/:namespace/:name

Delete a namespace group. Requires `admin` role.

---

## Namespaces

### GET /api/v1/namespaces

List all discoverable namespaces in the cluster.

---

## Audit

### GET /api/v1/audit

Get the audit log of power actions.

**Query parameters**:
- `limit` (int): Max entries to return (default 100)
- `offset` (int): Pagination offset

---

## Preview

### POST /api/v1/preview/policy

Preview which targets a policy would affect without creating it.

### POST /api/v1/preview/override

Preview which targets an override would affect without creating it.

---

## Health

### GET /healthz

Liveness probe. Always returns 200 if the process is running.

### GET /readyz

Readiness probe. Returns 200 if both K8s API and SQLite are reachable. Returns 503 with `component` field on failure.

### GET /metrics

Prometheus metrics endpoint. Exposes:
- `aura_power_server_http_requests_total{method, path, status}` — request counter
- `aura_power_server_http_request_duration_seconds{method, path}` — request latency histogram

---

## User Management

### GET /api/v1/auth/users

List all users. Requires `admin` role.

### GET /api/v1/auth/pending

List pending user registrations. Requires `admin` role.

---

## Roles

| Role | Permissions |
|------|-------------|
| `viewer` | Read-only access to all GET endpoints |
| `approver` | Create/update policies and overrides |
| `admin` | Full access including user management and deletions |
