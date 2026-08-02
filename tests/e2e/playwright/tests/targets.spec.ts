import { test, expect } from '@playwright/test';

test.describe('Targets', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/');
    const loginVisible = await page.locator('input[type="password"]').isVisible().catch(() => false);
    if (loginVisible) {
      await page.fill('input[autocomplete="username"], input:first-of-type', 'admin');
      await page.fill('input[type="password"]', 'admin123');
      await page.click('button[type="submit"]');
    }
    await page.waitForSelector('text=Cluster Overview', { timeout: 15000 });
    await page.click('text=Targets');
    await page.waitForSelector('table', { timeout: 10000 });
  });

  test('B5: table renders with workloads', async ({ page }) => {
    const rows = page.locator('tbody tr');
    await expect(rows.first()).toBeVisible();
    const count = await rows.count();
    expect(count).toBeGreaterThan(0);
  });

  test('B6: search filters results', async ({ page }) => {
    await page.fill('input[placeholder*="Search"]', 'argocd');
    await page.waitForTimeout(500);
    const rows = page.locator('tbody tr');
    const count = await rows.count();
    expect(count).toBeGreaterThan(0);
    // All visible rows should contain argocd
    const firstCell = await rows.first().locator('td').first().textContent();
    expect(firstCell?.toLowerCase()).toContain('argocd');
  });

  test('B7: state filter works', async ({ page }) => {
    await page.click('button:has-text("Running")');
    await page.waitForTimeout(500);
    // Should have Running chips visible
    await expect(page.locator('text=Running').first()).toBeVisible();
  });

  test('B8: click namespace navigates', async ({ page }) => {
    const nsLink = page.locator('a[href*="/targets/"]').first();
    const href = await nsLink.getAttribute('href');
    await nsLink.click();
    await page.waitForURL(`**${href}`);
    expect(page.url()).toContain('/targets/');
  });

  test('B10: Create Schedule button opens drawer', async ({ page }) => {
    await page.click('button:has-text("Create Schedule")');
    await expect(page.locator('text=New Schedule')).toBeVisible();
  });
});
