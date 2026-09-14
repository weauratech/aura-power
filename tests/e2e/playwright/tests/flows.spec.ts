import { test, expect } from '@playwright/test';
import { openPage, waitForMobileNavigationClosed } from './support';

test('administrator navigation keeps every primary destination reachable', async ({ page }) => {
  await openPage(page, 'Dashboard', 'Cluster Overview');
  const destinations = [
    ['Targets', 'Targets'],
    ['Schedules', 'Schedules'],
    ['Overrides', 'Overrides'],
    ['Savings', 'Savings'],
    ['Blocked', 'Blocked Targets'],
    ['Audit Log', 'Audit Log'],
    ['Notifications', 'Notifications'],
    ['Metrics', 'Metrics'],
    ['Pending', 'Pending Approvals'],
    ['Users', 'Users'],
  ] as const;
  for (const [link, heading] of destinations) {
    const mobile = (page.viewportSize()?.width ?? 1280) < 900;
    if (mobile) {
      await page.getByRole('button', { name: 'Open navigation' }).click();
    }
    await page.getByRole('link', { name: link, exact: true }).click();
    if (mobile) {
      await waitForMobileNavigationClosed(page);
    }
    await expect(page.getByRole('heading', { name: heading, exact: true })).toBeVisible();
  }
});

test('@mutation member receives an authorization denial for policy creation', async ({ page }) => {
  const adminUser = process.env.AURA_E2E_ADMIN_USER;
  const adminPassword = process.env.AURA_E2E_ADMIN_PASSWORD;
  if (!adminUser || !adminPassword) throw new Error('administrator fixture credentials are required');
  const username = `codex-member-${Date.now()}`;
  const password = `fixture-${Date.now()}-A!`;
  const create = await page.request.post('/api/v1/users', { data: { username, password, role: 'member' } });
  expect(create.status()).toBe(201);
  const created = await create.json();
  try {
    await page.request.post('/api/v1/auth/logout');
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
    await page.request.post('/api/v1/auth/login', { data: { username: adminUser, password: adminPassword } });
    const cleanup = await page.request.delete(`/api/v1/users/${created.id}`);
    expect([200, 204]).toContain(cleanup.status());
  }
});

test('@mutation authenticated member changes password and must sign in again', async ({ page, browser }, testInfo) => {
  const username = `codex-password-${Date.now()}`;
  const currentPassword = `Current-fixture-${Date.now()}!`;
  const newPassword = `Rotated-fixture-${Date.now()}!`;
  const create = await page.request.post('/api/v1/users', { data: { username, password: currentPassword, role: 'member' } });
  expect(create.status()).toBe(201);
  const created = await create.json();
  const memberContext = await browser.newContext({
    baseURL: testInfo.project.use.baseURL as string,
    storageState: { cookies: [], origins: [] },
  });
  const memberPage = await memberContext.newPage();
  try {
    await memberPage.goto('/');
    await memberPage.getByLabel('Username').fill(username);
    await memberPage.getByLabel('Password').fill(currentPassword);
    await memberPage.getByRole('button', { name: 'Sign in' }).click();
    await expect(memberPage.getByRole('heading', { name: 'Cluster Overview' })).toBeVisible();

    const openNavigation = memberPage.getByRole('button', { name: 'Open navigation' });
    if (await openNavigation.isVisible()) {
      await openNavigation.click();
    }
    await memberPage.getByRole('button', { name: 'Change password' }).click();
    const dialog = memberPage.getByRole('dialog', { name: 'Change password' });
    await dialog.getByLabel(/Current password/).fill(currentPassword);
    await dialog.getByLabel(/^New password/).fill(newPassword);
    await dialog.getByLabel(/Confirm new password/).fill(newPassword);
    await dialog.getByRole('button', { name: 'Change password', exact: true }).click();
    await expect(memberPage.getByRole('button', { name: 'Sign in' })).toBeVisible();

    await memberPage.getByLabel('Username').fill(username);
    await memberPage.getByLabel('Password').fill(newPassword);
    await memberPage.getByRole('button', { name: 'Sign in' }).click();
    await expect(memberPage.getByRole('heading', { name: 'Cluster Overview' })).toBeVisible();
  } finally {
    await memberContext.close();
    const cleanup = await page.request.delete(`/api/v1/users/${created.id}`);
    expect([200, 204]).toContain(cleanup.status());
  }
});
