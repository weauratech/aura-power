import { test, expect } from '@playwright/test';
import { openPage } from './support';

test.describe('dashboard', () => {
  test.beforeEach(async ({ page }) => openPage(page, 'Dashboard', 'Cluster Overview'));

  test('shows the complete operational summary', async ({ page }) => {
    const main = page.getByRole('main');
    for (const label of ['Coverage', 'Savings', 'Powered On', 'Powered Off', 'Blocked', 'Divergent']) {
      await expect(main.getByText(label, { exact: true }).first()).toBeVisible();
    }
    await expect(main.getByText('State Distribution', { exact: true })).toBeVisible();
    await expect(main.getByText('Targets by Namespace', { exact: true })).toBeVisible();
    await expect(main.getByText('Recent Activity', { exact: true })).toBeVisible();
  });
});
