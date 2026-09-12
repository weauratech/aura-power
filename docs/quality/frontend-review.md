# Frontend quality review and acceptance contract

## Scope and method

This review covers every authored file under `web/src`, the Vite and TypeScript configuration, the frontend package manifest, and every Playwright test under `tests/e2e/playwright`. Generated dependencies and `web/dist` are excluded. The review followed user journeys across authentication, navigation, dashboard, targets, schedules, overrides, savings, blocked targets, audit, notifications, metrics, approvals, and users.

The evidence in this document comes from source inspection and deterministic Vitest/MSW tests. A rendered browser audit requires a reachable Aura Power server and dedicated credentials; the Playwright layer now requires those values explicitly and produces traces, videos, screenshots, HTML, and JUnit evidence on failure. No screenshot-only accessibility or WCAG conformance claim is made here.

## Test architecture

The default frontend suite uses Vitest, jsdom, Testing Library, user-event, and MSW. Tests exercise real hooks and components while replacing only the HTTP boundary. Unhandled HTTP calls fail the test. React Query retries and focus refetching are disabled in the harness so each assertion maps to one intentional request.

Run the passing regression suite and build with:

```bash
cd web
npm test
npm run build
```

Known product contracts are kept in a separate acceptance command. These are normal failing tests, without `skip`, `todo`, retries, conditional assertions, or `test.fails`:

```bash
cd web
npm run test:acceptance
```

The browser suite has no default URL or credentials. Read-only scenarios run unless mutation is explicitly enabled:

```bash
cd tests/e2e/playwright
AURA_E2E_BASE_URL=http://127.0.0.1:8080 \
AURA_E2E_ADMIN_USER=fixture-admin \
AURA_E2E_ADMIN_PASSWORD='<fixture-secret>' \
AURA_E2E_FIXTURE_NAMESPACE=aura-power-e2e-run-id \
npm test
```

Set `AURA_E2E_ALLOW_MUTATION=true` only for a dedicated fixture environment. Set `AURA_E2E_BROWSERS=all` for Chromium, Firefox, WebKit, and mobile Chromium. Mutating tests create unique names and clean up in `finally` blocks. A failed cleanup fails the test rather than disappearing from the report.

## Verified behavior

| Contract | Evidence | Result |
|---|---|---|
| Existing member, approver, and admin sessions are loaded from `/auth/me` | `web/tests/unit/auth.test.tsx` | Pass |
| A 401 produces an unauthenticated state | `web/tests/unit/auth.test.tsx` | Pass |
| Login requires both fields, submits once, and preserves input after failure | `web/tests/unit/auth.test.tsx` | Pass |
| Target filters are URL encoded and do not disappear at the HTTP boundary | `web/tests/unit/api.test.tsx` | Pass |
| HTML proxy errors and 403 responses cannot become successful results | `web/tests/unit/api.test.tsx` | Pass |
| PUT sends JSON with same-origin credentials; DELETE accepts 204 | `web/tests/unit/api.test.tsx` | Pass |
| Metrics and cost provider availability remain independent | `web/tests/unit/api.test.tsx` | Pass |
| Schedule preview and creation send the explicit namespace scope | `web/tests/unit/schedule-drawer.test.tsx` | Pass |
| Schedule API failure remains visible and keeps the form open | `web/tests/unit/schedule-drawer.test.tsx` | Pass |
| Target search produces only matching visible rows | `web/tests/unit/pages.test.tsx` | Pass |
| Audit empty state is distinguishable from an API failure | `web/tests/unit/pages.test.tsx` | Pass |

At commit `4727bc5`, 25 default tests pass. The production frontend build succeeds, while Vite reports a 1.07 MB minified entry chunk (316 KB gzip). `npm run lint` cannot run because the repository declares the script but has no ESLint dependency or configuration.

## Validated product failures

### Restricted security review

Security findings and reproductions are intentionally excluded from this public
artifact. They are retained in restricted evidence storage and must follow the
coordinated disclosure process in `SECURITY.md`.

### FE-02: notification gateway timeout is presented as an empty configuration (P2)

The notifications query converts HTTP 504 into `{items: [], count: 0}`. Operators are invited to create a channel even when existing channels could not be loaded. This is a false empty state and can cause duplicate or conflicting configuration.

Reproduction: `web/tests/acceptance/notification-errors.test.tsx`. Expected: visible load failure and no empty-state call to action. Observed: “No notification channels”.

### FE-03: Pending Approvals is a static placeholder (P1 functional gap)

Approver and administrator navigation exposes `/pending`, but `PendingApprovals.tsx` performs no query and always states that there are no requests. There are no approve, reject, refresh, error, or detail states. The navigation promises an operational approval flow that the page cannot perform.

### FE-04: workload selection loses pair identity (P1 with backend/controller scope validation)

The drawer displays targets as `namespace/name`, then serializes separate `namespaces[]` and `workloadNames[]` arrays. Selecting `alpha/api` and `beta/worker` yields namespaces `[alpha,beta]` and names `[api,worker]`, which cannot represent the two intended pairs and can become a Cartesian selection. Kind and controller acceptance tests must confirm the resulting mutation set before this is published as a standalone frontend issue.

### FE-05: portable installation namespace is hard-coded (P2)

Schedule, override, and notification creation always submit `metadata.namespace: aura-system`. The UI cannot honor a Helm release installed into another control namespace. This combines with the controller/API namespace assumptions and should be triaged as one cross-layer issue.

### FE-06: destructive icon actions can also open edit flows (P2)

Schedule and notification tables attach edit behavior to the entire row while delete buttons do not stop event propagation. Clicking Delete can therefore open the edit drawer underneath the confirmation dialog. The action is confusing for keyboard and pointer users and makes post-delete state harder to understand.

### FE-07: icon-only controls lack explicit accessible names (P2 accessibility)

Theme toggles, the mobile menu, several close buttons, target schedule icons, and disclosure buttons have no explicit accessible name. Some are wrapped in MUI Tooltip, but several are not; assistive technology and role/name based automation cannot identify their purpose reliably. Add `aria-label` or visible text and verify keyboard focus and announcement in the rendered app.

### FE-08: error behavior is inconsistent between pages (P2)

Shared API hooks normalize authorization and response errors, while Dashboard, Users, Notifications, metrics hooks, and provider checks implement separate fetch/error rules. Some errors become “unavailable”, some become empty data, and some use browser `alert()`/`confirm()`. Centralizing the transport contract would make session expiry, retries, cancellation, and error language consistent.

### FE-09: displayed version and package version diverge (P3)

The layout displays “Aura Power v2.0” while `web/package.json` declares `0.1.0`. Build metadata should supply one version, commit, and release identity to both server and panel.

## Semantic and architectural review

The public model currently blurs accepted intent, computed preview, controller decision, and completed execution. Schedules show `affectedTargets`; Targets show observed and desired states; Audit shows actions. There is no shared correlation identifier or lifecycle state spanning these pages. A state-of-the-art operational UI should distinguish at least request accepted, decision computed, action started, action completed, action failed, and restoration verified.

Target routes and keys use namespace/name in several places while Kubernetes identity also requires kind and UID for collision and recreation safety. The frontend types should consume a canonical target identity generated by the backend. Display labels can stay concise while links and mutations preserve all identity fields.

Schedule and override creation share one drawer and mode toggle. This hides important differences: recurring time-window intent versus temporary exception, approval requirements, expiry validation, and audit impact. Shared form primitives are useful, but each operation needs a distinct submit contract, validation schema, confirmation language, and completion evidence.

The current frontend fetches and renders whole collections, then filters locally. That is acceptable for small clusters but lacks pagination, stable sorting, cancellation, and virtualization. Audit already exposes count/total but does not paginate. Targets and schedules should define server pagination and filtering before scaling claims are made.

The bundle contains the full application in one entry chunk. Route-level lazy loading and deliberate vendor chunking should be measured using a bundle analyzer and real browser timings before changing boundaries. The present build warning is sufficient to create a performance improvement issue, but it is not proof of poor user-perceived performance.

## File review register

| Area | Files reviewed | Main conclusion |
|---|---|---|
| Entry and routing | `main.tsx`, `App.tsx`, `ThemeContext.tsx` | Simple composition; authentication/error routing needs an explicit unavailable state and route contract |
| Shared components | `ConfirmDialog.tsx`, `EmptyState.tsx`, `ErrorBoundary.tsx`, `Layout.tsx`, `Notifications.tsx`, `ScheduleDrawer.tsx` | Strong reusable primitives; accessibility names, event propagation, scope identity, and failure semantics need work |
| HTTP and state | `useApi.ts`, `useAuth.ts`, `useMetrics.ts`, `useProviderStatus.ts` | React Query is a sound base; duplicate transports and inconsistent error handling create inconsistent guarantees |
| Operational pages | `Dashboard.tsx`, `Targets.tsx`, `NamespaceDetail.tsx`, `TargetDetail.tsx`, `Schedule.tsx`, `Policies.tsx`, `RuleDetail.tsx`, `Overrides.tsx` | Core surfaces exist; exact target identity and execution lifecycle are not preserved end to end |
| Evidence and integrations | `AuditLog.tsx`, `Notifications.tsx`, `Metrics.tsx`, `Savings.tsx`, `Blocked.tsx` | Useful views; partial dependency failures are often collapsed and audit is not correlated to completion |
| Administration | `Login.tsx`, `PendingApprovals.tsx`, `Users.tsx` | Login and user CRUD exist; approval UI is not implemented and native dialogs fragment UX |
| Design system | all files under `design-system/` | Tokens and MUI theme provide a coherent base; interactive components need rendered accessibility validation |
| Types and errors | `types/index.ts`, `utils/errors.ts` | Central types help, but public contracts need canonical schemas and exhaustive error/status enums |
| Test/build config | `package.json`, Vite/Vitest/TypeScript files, Playwright package/config/specs | New deterministic layers are usable; lint dependency/config and browser execution against a real server remain required |

## Recommended delivery sequence

1. Fix FE-02 with the failing acceptance test as a merge gate; handle restricted security findings through coordinated disclosure.
2. Define canonical target identity and exact scope semantics across CRDs, API, CLI, and UI; migrate the drawer and routes together.
3. Either implement approval APIs and UI completely or remove the navigation and claimed capability until ready.
4. Introduce one typed transport client with explicit unauthenticated, unavailable, validation, forbidden, conflict, and timeout states.
5. Add accessible names and keyboard tests, then run axe plus manual keyboard, focus, zoom, contrast, and screen-reader checks on the rendered product.
6. Correlate preview, accepted operation, controller decision, workload effect, audit event, and recovery in the UI.
7. Add ESLint with React, hooks, accessibility, and TypeScript rules; then measure route chunks and browser performance before optimizing.

The frontend should not be declared functionally complete while FE-02 through FE-04 remain unresolved or while the browser suite has not passed against the same product build validated by the controller and API layers.
