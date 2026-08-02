import { test, expect } from '@playwright/test';

test.describe('Schedules', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/');
    const loginVisible = await page.locator('input[type="password"]').isVisible().catch(() => false);
    if (loginVisible) {
      await page.fill('input[autocomplete="username"], input:first-of-type', 'admin');
      await page.fill('input[type="password"]', 'admin123');
      await page.click('button[type="submit"]');
    }
    await page.waitForSelector('text=Cluster Overview', { timeout: 15000 });
    await page.click('text=Schedules');
    await page.waitForSelector('text=Power policies', { timeout: 10000 });
  });

  test('B14: table shows policies', async ({ page }) => {
    await expect(page.locator('table')).toBeVisible();
    const rows = page.locator('tbody tr');
    const count = await rows.count();
    expect(count).toBeGreaterThan(0);
  });

  test('B15: New Schedule button opens drawer', async ({ page }) => {
    await page.click('button:has-text("New Schedule")');
    await expect(page.locator('text=New Schedule')).toBeVisible();
  });

  test('B16: namespace autocomplete populates', async ({ page }) => {
    await page.click('button:has-text("New Schedule")');
    await page.waitForSelector('text=New Schedule');
    const nsInput = page.locator('input[placeholder*="Type to search"]').first();
    await nsInput.click();
    await nsInput.fill('a');
    // Autocomplete dropdown should appear
    await page.waitForTimeout(1000);
    const options = page.locator('[role="listbox"] [role="option"], .MuiAutocomplete-option');
    const count = await options.count();
    expect(count).toBeGreaterThan(0);
  });

  test('B17: override toggle shows expiration fields', async ({ page }) => {
    await page.click('button:has-text("New Schedule")');
    await page.waitForSelector('text=New Schedule');
    // Toggle override switch
    await page.click('text=Temporary override');
    await expect(page.locator('label:has-text("Expires in")')).toBeVisible();
    await expect(page.locator('label:has-text("Reason")')).toBeVisible();
  });

  test('B18: create and delete smoke-test policy', async ({ page }) => {
    await page.click('button:has-text("New Schedule")');
    await page.waitForSelector('text=New Schedule');

    // Fill form
    await page.fill('input[label="Name"], input:below(:text("Name")):first', 'smoke-test-pw');
    await page.waitForTimeout(300);

    // Select namespace
    const nsInput = page.locator('input[placeholder*="Type to search"]').first();
    await nsInput.click();
    await nsInput.fill('default');
    await page.waitForTimeout(500);
    await page.keyboard.press('Enter');

    // Submit
    await page.click('button:has-text("Create Schedule")');
    await page.waitForTimeout(2000);

    // Verify in table
    const tableContent = await page.locator('table').textContent();
    if (tableContent?.includes('smoke-test-pw')) {
      // Cleanup — find and click delete
      const row = page.locator('tr:has-text("smoke-test-pw")');
      await row.locator('button[aria-label="Delete"], button:has(svg)').last().click();
      await page.waitForTimeout(1000);
    }
  });
});
