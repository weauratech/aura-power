import { test, expect } from '@playwright/test';
import { openPage } from './support';

test('administrator navigation keeps every primary destination reachable', async ({ page }) => {
  await openPage(page, 'Dashboard', 'Cluster Overview');
  const destinations = [
    ['Targets', 'Targets'],
    ['Schedules', 'Schedules'],
    ['Savings', 'Savings'],
    ['Blocked', 'Blocked Targets'],
    ['Audit Log', 'Audit Log'],
    ['Notifications', 'Notifications'],
    ['Metrics', 'Metrics'],
    ['Pending', 'Pending Approvals'],
    ['Users', 'Users'],
  ] as const;
  for (const [link, heading] of destinations) {
    await page.getByRole('link', { name: link, exact: true }).click();
    await expect(page.getByRole('heading', { name: heading, exact: true })).toBeVisible();
  }
});

test('@mutation member receives an authorization denial for policy creation', async ({ page, request }) => {
  const adminUser = process.env.AURA_E2E_ADMIN_USER;
  const adminPassword = process.env.AURA_E2E_ADMIN_PASSWORD;
  if (!adminUser || !adminPassword) throw new Error('administrator fixture credentials are required');
  const username = `codex-member-${Date.now()}`;
  const password = `fixture-${Date.now()}-A!`;
  const create = await request.post('/api/v1/users', { data: { username, password, role: 'member' } });
  expect(create.status()).toBe(201);
  const created = await create.json();
  try {
    await request.post('/api/v1/auth/logout');
    await page.goto('/');
    await page.getByLabel('Username').fill(username);
    await page.getByLabel('Password').fill(password);
    await page.getByRole('button', { name: 'Sign in' }).click();
    await expect(page.getByRole('heading', { name: 'Cluster Overview' })).toBeVisible();
    const denied = await page.request.post('/api/v1/policies', {
      data: { metadata: { name: `codex-denied-${Date.now()}`, namespace: 'aura-system' }, spec: { scope: { namespaces: ['forbidden-fixture'] }, schedule: { desiredState: 'off' }, priority: 1 } },
    });
    expect(denied.status()).toBe(403);
  } finally {
    await request.post('/api/v1/auth/login', { data: { username: adminUser, password: adminPassword } });
    const cleanup = await request.delete(`/api/v1/users/${created.id}`);
    expect([200, 204]).toContain(cleanup.status());
  }
});
