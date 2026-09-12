import { test as setup, expect } from '@playwright/test';

const username = process.env.AURA_E2E_ADMIN_USER;
const password = process.env.AURA_E2E_ADMIN_PASSWORD;

setup('authenticate an isolated administrator fixture', async ({ page }) => {
  if (!username || !password) throw new Error('AURA_E2E_ADMIN_USER and AURA_E2E_ADMIN_PASSWORD are required.');
  await page.goto('/');
  await page.getByLabel('Username').fill(username);
  await page.getByLabel('Password').fill(password);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page.getByRole('heading', { name: 'Cluster Overview' })).toBeVisible();
  await page.context().storageState({ path: 'tests/.auth/admin.json' });
});
