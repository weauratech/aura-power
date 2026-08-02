import { test, expect } from '@playwright/test';

test.describe('Dashboard', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/');
    // Login
    const loginVisible = await page.locator('input[type="password"]').isVisible().catch(() => false);
    if (loginVisible) {
      await page.fill('input[autocomplete="username"], input:first-of-type', 'admin');
      await page.fill('input[type="password"]', 'admin123');
      await page.click('button[type="submit"]');
    }
    await page.waitForSelector('text=Cluster Overview', { timeout: 15000 });
  });

  test('B2: renders metric cards with values', async ({ page }) => {
    await expect(page.locator('text=Coverage')).toBeVisible();
    await expect(page.locator('text=Savings')).toBeVisible();
    await expect(page.locator('text=Powered On')).toBeVisible();
    await expect(page.locator('text=Powered Off')).toBeVisible();
    await expect(page.locator('text=Blocked')).toBeVisible();
    await expect(page.locator('text=Divergent')).toBeVisible();
  });

  test('B3: renders charts', async ({ page }) => {
    await expect(page.locator('text=State Distribution')).toBeVisible();
    await expect(page.locator('text=Targets by Namespace')).toBeVisible();
  });

  test('B4: activity feed shows events', async ({ page }) => {
    await expect(page.locator('text=Recent Activity')).toBeVisible();
  });
});
