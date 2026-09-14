import { test, expect } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

test('invalid credentials stay accessible and unauthenticated in both themes', async ({ page }) => {
  for (const theme of ['light', 'dark']) {
    await page.addInitScript(selected => localStorage.setItem('aura-power-theme', selected), theme);
    await page.goto('/');
    await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
    await page.getByLabel('Username').fill(`invalid-${theme}-${Date.now()}`);
    await page.getByLabel('Password').fill('invalid-fixture-password');
    await page.getByRole('button', { name: 'Sign in' }).click();
    await expect(page.getByRole('alert')).toBeVisible();
    await expect(page.getByRole('heading', { level: 1, name: 'Aura Power' })).toBeVisible();
    await expect(page.getByRole('heading', { name: 'Cluster Overview' })).not.toBeVisible();
    await expect(page).toHaveTitle('Sign in · Aura Power');
    const accessibility = await new AxeBuilder({ page })
      .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa'])
      .analyze();
    expect(accessibility.violations, `${theme}: ${JSON.stringify(accessibility.violations, null, 2)}`).toEqual([]);
  }
});
