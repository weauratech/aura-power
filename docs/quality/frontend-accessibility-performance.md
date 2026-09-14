# Frontend accessibility and performance assessment

## Scope and evidence

The target is WCAG 2.2 Level AA for every authored panel route. The permanent
browser inventory covers login; dashboard; targets, namespace and workload
details; schedule aliases (`/schedule`, `/rules`, `/policies`); rule detail;
overrides; savings; blocked targets; audit; notifications; cluster metrics at
`/cluster-metrics`; pending
approvals; users; the all-pages locator; and the schedule, notification,
override and user dialogs.

`tests/e2e/playwright/tests/accessibility.spec.ts` runs Axe in light and dark
themes, verifies one `h1`, page titles, skip navigation and document reflow,
and exercises keyboard focus, a 320 CSS-pixel reflow viewport and reduced
motion. An induced 503 journey also verifies that every data-backed route,
including collection and detail views, retains one visible `h1` and a valid
document structure while reporting failure. With `AURA_E2E_BROWSERS=all`, the
authenticated and login inventories
run on Chromium, Firefox and WebKit at desktop and mobile viewports against the
real server and dedicated Kind fixtures.

The implementation audit reproduced invalid list children in primary
navigation, unnamed progress bars and contrast ratios from 2.45:1 to 4.34:1.
It also found missing route headings, persistent search labels, table names,
skip navigation, SPA page titles, current-page state and focus transfer. A
rendered Chromium rerun on dashboard, targets, schedules, overrides, audit and
users in both themes reported zero Axe violations for the selected WCAG A/AA
tags. A later serial run against the embedded production build, a real server
and a dedicated Kind cluster passed all 26 accessibility tests. The complete
desktop Chromium run passed 45 tests and skipped only the mobile-only spec. It
also proved that `/metrics` remains Prometheus text while a reload or deep link to
`/cluster-metrics` returns the panel. Sanitized screenshots from the local-data
audit are kept outside the repository in the campaign evidence directory.

Automation does not establish conformance. The release gate proves only Axe's
rules and the explicit browser assertions. Keyboard/focus, zoom-equivalent
reflow and reduced motion have automated coverage. A human still needs to
sample VoiceOver, NVDA or another supported screen reader with real content and
verify reading order, announcements, speech clarity and task completion. Chart
contrast and tenant-supplied content also require visual sampling.

## Route and state inventory

| Surface | Loading | Empty | Populated | Failure | Validation/dialog | Authorization |
|---|---|---|---|---|---|---|
| Login | auth check | n/a | form | invalid login | required fields | unauthenticated |
| Dashboard | skeleton | zero summary | charts/events | provider/API | n/a | all signed-in roles |
| Targets/details | skeleton | empty/not found | list/detail | API alert | schedule drawer | all signed-in roles |
| Schedules/rules | skeleton | no schedules | policies/defaults | API alert | create/edit/delete | member denial journey |
| Overrides | skeleton | no overrides | active/expired | API alert | create/delete | role contract |
| Savings | skeleton | zero/no rows | summary/table | API alert | export | all signed-in roles |
| Blocked | skeleton | success notice | reasons | API alert | disclosure | all signed-in roles |
| Audit | skeleton | induced empty | induced event | induced 503 | search/export | all signed-in roles |
| Notifications | named status | empty action | table | induced failure/recovery | create/edit/delete | role contract |
| Metrics | skeleton | provider absent | charts | split provider failure | time range | all signed-in roles |
| Pending | skeleton | no requests | table | API alert | approve/reject | approver/admin |
| Users | skeleton | no secondary users | table | API alert | create/delete | admin |
| All pages | n/a | n/a | role-aware link list | n/a | navigation | all signed-in roles |

Functional specs own data-state assertions. The accessibility spec scans every
stable route and overlay. Kind fixtures provide detail-route data; functional
tests induce audit empty, populated and failure states without conditional
assertions or retries.

## WCAG 2.2 A and AA register

`Pass` means the reviewed source invariant or browser assertion passes and the
criterion does not require an unperformed human check. `Not tested` means the
automated checks pass where applicable but the criterion still needs the stated
human assessment. `Fail` records a known gap. `N/A` means no reviewed route
contains that content or interaction.

| Success criterion | Status | Evidence or remaining work |
|---|---|---|
| 1.1.1 Non-text Content | Not tested | Decorative icons are hidden and controls/charts have names; sample chart alternatives with AT. |
| 1.2.1–1.2.5 Time-based Media | N/A | No authored audio or video. |
| 1.3.1 Info and Relationships | Not tested | Headings, labels, tables, groups and dialogs are scanned; confirm reading order with AT. |
| 1.3.2 Meaningful Sequence | Not tested | DOM and visual order align in inspected routes; AT sampling remains. |
| 1.3.3 Sensory Characteristics | Pass | Instructions use names and state text rather than position alone. |
| 1.3.4 Orientation | Pass | No orientation lock; desktop/mobile projects exercise portrait widths. |
| 1.3.5 Identify Input Purpose | Pass | Login uses username and current-password autocomplete. |
| 1.4.1 Use of Color | Not tested | Status retains text/marks; visually sample charts. |
| 1.4.2 Audio Control | N/A | No audio. |
| 1.4.3 Contrast (Minimum) | Not tested | Axe in both themes after token/palette corrections; sample tenant content. |
| 1.4.4 Resize Text | Not tested | 320 CSS px represents 400% zoom at 1280 px and rejects overflow; final native-browser zoom remains. |
| 1.4.5 Images of Text | Pass | Logos have alternatives; no informational rasterized text. |
| 1.4.10 Reflow | Pass | Narrow scans reject document overflow; named table containers may scroll internally. |
| 1.4.11 Non-text Contrast | Not tested | Axe checks controls/focus; charts need visual sampling. |
| 1.4.12 Text Spacing | Not tested | No clipping in responsive evidence; custom spacing sampling remains. |
| 1.4.13 Content on Hover or Focus | Not tested | Tooltips are keyboard reachable; human dismissal/persistence sampling remains. |
| 2.1.1 Keyboard | Not tested | Navigation, disclosures, rows and dialogs have keyboard journeys; full tasks remain. |
| 2.1.2 No Keyboard Trap | Pass | Escape closes overlays and focus returns to the trigger. |
| 2.1.4 Character Key Shortcuts | N/A | No single-character shortcuts. |
| 2.2.1 Timing Adjustable | N/A | No authored UI timeout; authentication expiry is a security boundary. |
| 2.2.2 Pause, Stop, Hide | N/A | Polling does not move or blink content. |
| 2.3.1 Three Flashes | Pass | No flashing animation; reduced-motion tokens collapse duration. |
| 2.4.1 Bypass Blocks | Pass | Focus-visible skip link targets the main landmark. |
| 2.4.2 Page Titled | Pass | Route changes set a descriptive Aura Power title. |
| 2.4.3 Focus Order | Not tested | SPA navigation focuses the new `h1`; full manual order remains. |
| 2.4.4 Link Purpose | Pass | Links have visible contextual names. |
| 2.4.5 Multiple Ways | Pass | Primary navigation and the role-aware all-pages locator provide distinct ways to find every user-facing area. |
| 2.4.6 Headings and Labels | Pass | One `h1`, ordered sections, persistent labels and named controls. |
| 2.4.7 Focus Visible | Not tested | Shared focus-visible style is present; visual sampling remains. |
| 2.4.11 Focus Not Obscured | Not tested | Route/dialog focus is visible; sticky/zoom combinations need sampling. |
| 2.5.1 Pointer Gestures | N/A | No multipoint/path gesture. |
| 2.5.2 Pointer Cancellation | Pass | Actions occur on click; destructive actions confirm. |
| 2.5.3 Label in Name | Pass | Axe and role/name locators cover controls. |
| 2.5.4 Motion Actuation | N/A | No motion input. |
| 2.5.7 Dragging Movements | N/A | No drag-only interaction. |
| 2.5.8 Target Size | Not tested | The 320 px Targets filter asserts at least 24 by 24 CSS px; remaining dense controls need visual sampling. |
| 3.1.1 Language of Page | Pass | Root document declares English. |
| 3.1.2 Language of Parts | N/A | Authored UI is consistently English. |
| 3.2.1 On Focus | Pass | Focus does not navigate or mutate. |
| 3.2.2 On Input | Pass | Input does not unexpectedly change context. |
| 3.2.3 Consistent Navigation | Pass | Shared layout persists across routes. |
| 3.2.4 Consistent Identification | Pass | Shared controls retain names/icons. |
| 3.2.6 Consistent Help | N/A | No help mechanism is offered. |
| 3.3.1 Error Identification | Not tested | Login/API errors use alerts; form announcement sampling remains. |
| 3.3.2 Labels or Instructions | Pass | Labels/helper text cover inputs and required fields. |
| 3.3.3 Error Suggestion | Not tested | Known errors are actionable; validate server variants with users. |
| 3.3.4 Error Prevention | Not tested | Destructive actions confirm; operational consequences need human review. |
| 3.3.7 Redundant Entry | Pass | Editing retains values; repeated data is not required in one process. |
| 3.3.8 Accessible Authentication | Not tested | Password managers/paste are allowed; sample real managers and AT. |
| 4.1.2 Name, Role, Value | Pass | Axe plus role/name/state assertions cover routes/overlays. |
| 4.1.3 Status Messages | Not tested | Alerts and notifications are exposed; speech timing needs AT sampling. |

## Performance measurement and guard

The original production build emitted a 504.07 kB application entry (160.68
kB gzip). Lazy routes already existed, so that warning alone did not prove a
slow interaction. Analysis showed shared React/MUI, query and chart code grouped
with the application entry.

Vite now emits a manifest and stable UI, query and chart boundaries. On the
same source and machine, the final entry is 43,445 bytes (13,119 bytes gzip), a
91.4% raw reduction; the imported design-token stylesheet is 15,527 bytes
(2,748 bytes gzip) and the largest JavaScript chunk is 338,856 bytes. Every
build runs
`scripts/check-bundle-budget.mjs` and fails above 100 KiB raw/35 KiB gzip for
the application entry or 400 KiB raw for any JavaScript chunk.

`tests/e2e/playwright/tests/performance.spec.ts` captures route-level script
sizes and durations as Playwright attachments. It proves a cold Targets route
does not download the dashboard or chart chunks while Dashboard loads the chart
chunk on demand. Timing values remain evidence rather than a flaky wall-clock
gate; deterministic byte budgets protect the artifact in CI.

These are artifact budgets, not Core Web Vitals claims. Network, cache, device
CPU, LCP, INP and CLS need measurement on the deployed release with
representative users before user-experience SLOs are set.

## Commands

```bash
cd web
npm ci
npm test
npm run lint
npm run build

cd ../tests/e2e/playwright
npm ci
AURA_E2E_BASE_URL=... \
AURA_E2E_FIXTURE_NAMESPACE=... \
AURA_E2E_ADMIN_USER=... \
AURA_E2E_ADMIN_PASSWORD=... \
AURA_E2E_BROWSERS=all \
npx playwright test
```
