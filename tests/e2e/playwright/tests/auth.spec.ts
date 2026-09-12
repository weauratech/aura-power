import { test, expect } from '@playwright/test';

test('invalid credentials stay unauthenticated with a useful error', async ({ page }) => {
  await page.goto('/');
  await page.getByLabel('Username').fill(`invalid-${Date.now()}`);
  await page.getByLabel('Password').fill('invalid-fixture-password');
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page.getByRole('alert')).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Cluster Overview' })).not.toBeVisible();
});
