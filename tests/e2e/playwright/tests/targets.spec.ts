import { test, expect } from '@playwright/test';
import { openPage, requireFixtureNamespace } from './support';

test.describe('targets', () => {
  test.beforeEach(async ({ page }) => openPage(page, 'Targets', 'Targets'));

  test('search narrows results to the dedicated fixture namespace', async ({ page }) => {
    const namespace = requireFixtureNamespace();
    await page.getByPlaceholder(/Search by name or namespace/).fill(namespace);
    const rows = page.locator('tbody tr');
    await expect(rows.first()).toBeVisible();
    expect(await rows.count()).toBeGreaterThan(0);
    for (const row of await rows.all()) await expect(row).toContainText(namespace);
  });

  test('state filter and target details have observable outcomes', async ({ page }) => {
    await page.getByRole('button', { name: 'Running', exact: true }).click();
    await expect(page.getByText(/workloads \(filtered\)/)).toBeVisible();
    const target = page.locator('tbody tr').first().getByRole('link').last();
    await expect(target).toBeVisible();
    const targetName = (await target.textContent())?.trim();
    expect(targetName).toBeTruthy();
    await target.click();
    await expect(page).toHaveURL(/\/targets\/[^/]+\/[^/]+$/);
    await expect(page.getByText(targetName!, { exact: true }).first()).toBeVisible();
  });

  test('opens the schedule drawer with required fields incomplete', async ({ page }) => {
    await page.getByRole('main').getByRole('button', { name: 'Create Schedule', exact: true }).click();
    const drawer = page.getByRole('dialog', { name: 'New Schedule' });
    await expect(drawer).toBeVisible();
    await expect(drawer.getByRole('button', { name: 'Create Schedule', exact: true })).toBeDisabled();
  });
});
