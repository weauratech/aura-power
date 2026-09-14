import { expect, type Page } from '@playwright/test';

export async function waitForMobileNavigationClosed(page: Page) {
  await expect(page.locator('.MuiDrawer-modal')).toHaveCount(0);
}

export async function openPage(page: Page, navigationName: string, heading: string) {
  await page.goto('/');
  if (navigationName !== 'Dashboard') {
    const mobile = (page.viewportSize()?.width ?? 1280) < 900;
    if (mobile) {
      await page.getByRole('button', { name: 'Open navigation' }).click();
    }
    await page.getByRole('link', { name: navigationName, exact: true }).click();
    if (mobile) {
      await waitForMobileNavigationClosed(page);
    }
  }
  await expect(page.getByRole('heading', { name: heading, exact: true })).toBeVisible();
}

export function requireFixtureNamespace(): string {
  const namespace = process.env.AURA_E2E_FIXTURE_NAMESPACE;
  if (!namespace) throw new Error('AURA_E2E_FIXTURE_NAMESPACE is required for fixture-specific tests.');
  return namespace;
}
