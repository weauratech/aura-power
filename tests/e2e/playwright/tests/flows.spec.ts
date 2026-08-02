import { test, expect } from '@playwright/test';

// These are integration flow tests (C1-C5) that test multi-step workflows.

async function login(page: any) {
  await page.goto('/');
  const loginVisible = await page.locator('input[type="password"]').isVisible().catch(() => false);
  if (loginVisible) {
    await page.fill('input[autocomplete="username"], input:first-of-type', 'admin');
    await page.fill('input[type="password"]', 'admin123');
    await page.click('button[type="submit"]');
  }
  await page.waitForSelector('text=Cluster Overview', { timeout: 15000 });
}

test.describe('Integration Flows', () => {
  test('C1: Policy lifecycle (create → verify → delete)', async ({ page }) => {
    await login(page);

    // Navigate to Schedules
    await page.click('text=Schedules');
    await page.waitForSelector('table', { timeout: 10000 });

    // Open drawer
    await page.click('button:has-text("New Schedule")');
    await page.waitForSelector('text=New Schedule');

    // Fill name
    const nameInput = page.locator('input').first();
    await nameInput.fill('smoke-test-flow-c1');

    // Select namespace (type "default" in autocomplete)
    const nsInput = page.locator('input[placeholder*="Type to search"]').first();
    await nsInput.click();
    await nsInput.fill('default');
    await page.waitForTimeout(800);
    await page.keyboard.press('ArrowDown');
    await page.keyboard.press('Enter');

    // Submit
    await page.click('button:has-text("Create Schedule")');
    await page.waitForTimeout(3000);

    // Verify in table
    await page.reload();
    await page.waitForSelector('table', { timeout: 10000 });
    const exists = await page.locator('text=smoke-test-flow-c1').isVisible().catch(() => false);

    // Cleanup via API
    const resp = await page.request.delete('/api/v1/policies/aura-system/smoke-test-flow-c1');
    expect(resp.status()).toBe(200);
  });

  test('C3: User management flow', async ({ page }) => {
    await login(page);

    // Navigate to Users
    await page.click('text=Users');
    await page.waitForSelector('text=admin', { timeout: 10000 });

    // Create user
    await page.click('button:has-text("New User")');
    await page.waitForSelector('text=Username');
    await page.fill('input[label="Username"], input:near(:text("Username"))', 'smoke-test-flow-c3');
    await page.fill('input[type="password"]', 'FlowTest123!');
    await page.click('button:has-text("Create User")');
    await page.waitForTimeout(2000);

    // Verify user exists
    await page.reload();
    await page.waitForSelector('text=smoke-test-flow-c3', { timeout: 10000 });

    // Delete via API
    const usersResp = await page.request.get('/api/v1/users');
    const users = await usersResp.json();
    const testUser = (users.users || users).find((u: any) => u.username === 'smoke-test-flow-c3');
    if (testUser) {
      await page.request.delete(`/api/v1/users/${testUser.id}`);
    }
  });

  test('C4: RBAC enforcement (member cannot create)', async ({ page }) => {
    await login(page);

    // Create member user via API
    const createResp = await page.request.post('/api/v1/users', {
      data: { username: 'smoke-test-rbac-c4', password: 'RbacC4Test!', role: 'member' },
    });
    expect(createResp.status()).toBe(201);
    const created = await createResp.json();

    // Logout
    await page.request.post('/api/v1/auth/logout');

    // Login as member
    await page.goto('/');
    await page.waitForSelector('input[type="password"]', { timeout: 10000 });
    await page.fill('input[autocomplete="username"], input:first-of-type', 'smoke-test-rbac-c4');
    await page.fill('input[type="password"]', 'RbacC4Test!');
    await page.click('button[type="submit"]');
    await page.waitForSelector('text=Cluster Overview', { timeout: 15000 });

    // Try to create policy via API (should fail)
    const policyResp = await page.request.post('/api/v1/policies', {
      data: {
        metadata: { name: 'smoke-test-rbac-c4-policy', namespace: 'aura-system' },
        spec: { scope: { namespaces: ['default'] }, schedule: { desiredState: 'off' }, priority: 1 },
      },
    });
    expect(policyResp.status()).toBe(403);

    // Cleanup: re-login as admin and delete user
    await page.request.post('/api/v1/auth/logout');
    await page.goto('/');
    await page.waitForSelector('input[type="password"]', { timeout: 10000 });
    await page.fill('input[autocomplete="username"], input:first-of-type', 'admin');
    await page.fill('input[type="password"]', 'admin123');
    await page.click('button[type="submit"]');
    await page.waitForTimeout(2000);
    await page.request.delete(`/api/v1/users/${created.id}`);
  });
});
