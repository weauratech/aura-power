import { test, expect } from '@playwright/test';
import { openPage } from './support';

test('savings exposes its computed summary and export action', async ({ page }) => {
  await openPage(page, 'Savings', 'Savings');
  await expect(page.getByText('CPU Hours Saved', { exact: true })).toBeVisible();
  await expect(page.getByText('Memory GiB-Hours', { exact: true })).toBeVisible();
  await expect(page.getByText('Estimated Cost Saved', { exact: true })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Export CSV' })).toBeVisible();
});

test('blocked targets exposes a guardrail result', async ({ page }) => {
  await openPage(page, 'Blocked', 'Blocked Targets');
  await expect(page.getByText('Workloads where guardrails prevent power actions.')).toBeVisible();
  await expect(page.getByText('No blocked targets. All workloads are operating normally.').or(page.getByRole('table'))).toBeVisible();
});

test('notifications exposes configuration controls and a result', async ({ page }) => {
  await openPage(page, 'Notifications', 'Notifications');
  await expect(page.getByRole('button', { name: 'Create a new notification channel', exact: true })).toBeVisible();
  await expect(page.getByText('No notification channels').or(page.getByRole('table'))).toBeVisible();
});

test('metrics exposes availability or measured capacity', async ({ page }) => {
  await openPage(page, 'Metrics', 'Metrics');
  await expect(page.getByText(/Metrics provider not available/).or(page.getByText('CPU Usage', { exact: true }))).toBeVisible();
});

test('audit log renders an induced empty state', async ({ page }) => {
  await page.route('**/api/v1/audit*', route => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ events: [], count: 0, total: 0 }) }));
  await openPage(page, 'Audit Log', 'Audit Log');
  await expect(page.getByText('No audit events', { exact: true })).toBeVisible();
  await expect(page.getByText('0 total events', { exact: true })).toBeVisible();
});

test('audit log renders an induced data state', async ({ page }) => {
  await page.route('**/api/v1/audit*', route => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify({
      events: [{ spec: { timestamp: new Date().toISOString(), action: 'workload_powered_down', actor: 'quality-agent', target: { namespace: 'fixture', name: 'api', kind: 'Deployment' }, result: 'success', reason: 'induced browser contract', ruleName: 'quality' } }],
      count: 1,
      total: 1,
    }),
  }));
  await openPage(page, 'Audit Log', 'Audit Log');
  await expect(page.getByText('Powered Down', { exact: true })).toBeVisible();
  await expect(page.getByText('fixture/api', { exact: true })).toBeVisible();
  await expect(page.getByText('1 total events', { exact: true })).toBeVisible();
});

test('audit log renders an induced API error without an empty state', async ({ page }) => {
  await page.route('**/api/v1/audit*', route => route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ error: 'induced audit outage' }) }));
  await openPage(page, 'Audit Log', 'Audit Log');
  await expect(page.getByRole('alert')).toContainText(/induced audit outage/i);
  await expect(page.getByText('No audit events', { exact: true })).not.toBeVisible();
});

test('logout clears the session and returns to login', async ({ page }) => {
  await openPage(page, 'Dashboard', 'Cluster Overview');
  if ((page.viewportSize()?.width ?? 1280) < 900) {
    await page.getByRole('button', { name: 'Open navigation' }).click();
  }
  const navigation = page.getByRole('navigation', { name: 'Primary navigation' });
  await navigation.getByRole('button', { name: 'Sign out' }).click();
  await expect(page.getByLabel('Password')).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Cluster Overview' })).not.toBeVisible();
  const me = await page.request.get('/api/v1/auth/me');
  expect(me.status()).toBe(401);
  await page.reload();
  await expect(page.getByLabel('Password')).toBeVisible();
});
