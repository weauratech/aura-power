import { expect, type Page } from '@playwright/test';

export async function openPage(page: Page, navigationName: string, heading: string) {
  await page.goto('/');
  if (navigationName !== 'Dashboard') await page.getByRole('link', { name: navigationName, exact: true }).click();
  await expect(page.getByRole('heading', { name: heading, exact: true })).toBeVisible();
}

export function requireFixtureNamespace(): string {
  const namespace = process.env.AURA_E2E_FIXTURE_NAMESPACE;
  if (!namespace) throw new Error('AURA_E2E_FIXTURE_NAMESPACE is required for fixture-specific tests.');
  return namespace;
}
