import { test, expect } from '@playwright/test';
import { openPage, requireFixtureNamespace } from './support';

test.describe('schedules', () => {
  test.beforeEach(async ({ page }) => openPage(page, 'Schedules', 'Schedules'));

  const openScheduleDrawer = async (page: import('@playwright/test').Page) => {
    await page.getByRole('button', { name: 'Create a new schedule', exact: true }).click();
    return page.getByRole('dialog', { name: 'New Schedule' });
  };

  test('renders the policy table and validates required scope', async ({ page }) => {
    await expect(page.getByRole('table')).toBeVisible();
    const drawer = await openScheduleDrawer(page);
    await expect(drawer).toBeVisible();
    await expect(drawer.getByRole('button', { name: 'Create Schedule', exact: true })).toBeDisabled();
  });

  test('namespace autocomplete contains the dedicated fixture', async ({ page }) => {
    const namespace = requireFixtureNamespace();
    const drawer = await openScheduleDrawer(page);
    const scope = drawer.getByRole('combobox', { name: 'Select Namespaces' });
    await scope.fill(namespace);
    await expect(page.getByRole('option', { name: namespace, exact: true })).toBeVisible();
  });

  test('override mode requires a reason and shows expiry controls', async ({ page }) => {
    const drawer = await openScheduleDrawer(page);
    await drawer.getByRole('checkbox', { name: /Temporary override/ }).check();
    await expect(drawer.getByLabel('Expires in (hours)')).toBeVisible();
    await expect(drawer.getByLabel(/Reason/)).toBeVisible();
    await expect(drawer.getByRole('button', { name: 'Create Override', exact: true })).toBeDisabled();
  });

  test('@mutation creates, observes, and deletes a fixture policy', async ({ page }) => {
    const namespace = requireFixtureNamespace();
    const name = `codex-pw-${Date.now()}`;
    const resourceURL = `/api/v1/policies/aura-system/${name}`;
    const policyExists = async () => {
      const response = await page.request.get('/api/v1/policies');
      expect(response.status()).toBe(200);
      const body = await response.json() as { items?: Array<{ metadata?: { name?: string; namespace?: string } }> };
      return Boolean(body.items?.some(item => item.metadata?.name === name && item.metadata?.namespace === 'aura-system'));
    };
    expect(await policyExists()).toBe(false);
    try {
      const drawer = await openScheduleDrawer(page);
      await drawer.getByRole('textbox', { name: /^Name/ }).fill(name);
      const scope = drawer.getByRole('combobox', { name: 'Select Namespaces' });
      await scope.fill(namespace);
      await page.getByRole('option', { name: namespace, exact: true }).click();
      await drawer.getByRole('button', { name: 'Create Schedule', exact: true }).click();
      await expect(page.getByText(name, { exact: true })).toBeVisible();
    } finally {
      if (await policyExists()) {
        const deleted = await page.request.delete(resourceURL);
        expect([200, 204]).toContain(deleted.status());
      }
      expect(await policyExists()).toBe(false);
    }
  });
});
