import { test as setup, expect } from '@playwright/test';

const STORAGE_STATE = 'tests/.auth/user.json';

setup('authenticate', async ({ page }) => {
  await page.goto('/');

  // Should redirect to login or show login page
  await page.waitForSelector('input[name="username"], input[autocomplete="username"]', { timeout: 10000 });

  await page.fill('input[autocomplete="username"], input:first-of-type', process.env.ADMIN_USER || 'admin');
  await page.fill('input[type="password"]', process.env.ADMIN_PASS || 'admin123');
  await page.click('button[type="submit"]');

  // Wait for dashboard to load
  await page.waitForSelector('text=Cluster Overview', { timeout: 15000 });

  await page.context().storageState({ path: STORAGE_STATE });
});

export { STORAGE_STATE };
