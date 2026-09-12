import { test, expect } from '@playwright/test';
import { openPage, requireFixtureNamespace } from './support';

test.describe('schedules', () => {
  test.beforeEach(async ({ page }) => openPage(page, 'Schedules', 'Schedules'));

  test('renders the policy table and validates required scope', async ({ page }) => {
    await expect(page.getByRole('table')).toBeVisible();
    await page.getByRole('button', { name: 'New Schedule' }).click();
    await expect(page.getByRole('heading', { name: 'New Schedule' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Create Schedule' })).toBeDisabled();
  });

  test('namespace autocomplete contains the dedicated fixture', async ({ page }) => {
    const namespace = requireFixtureNamespace();
    await page.getByRole('button', { name: 'New Schedule' }).click();
    const scope = page.getByRole('combobox', { name: 'Select Namespaces' });
    await scope.fill(namespace);
    await expect(page.getByRole('option', { name: namespace })).toBeVisible();
  });

  test('override mode requires a reason and shows expiry controls', async ({ page }) => {
    await page.getByRole('button', { name: 'New Schedule' }).click();
    await page.getByRole('checkbox', { name: /Temporary override/ }).check();
    await expect(page.getByLabel('Expires in (hours)')).toBeVisible();
    await expect(page.getByLabel(/Reason/)).toBeVisible();
    await expect(page.getByRole('button', { name: 'Create Override' })).toBeDisabled();
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
      await page.getByRole('button', { name: 'New Schedule' }).click();
      await page.getByRole('textbox', { name: /^Name/ }).fill(name);
      const scope = page.getByRole('combobox', { name: 'Select Namespaces' });
      await scope.fill(namespace);
      await page.getByRole('option', { name: namespace }).click();
      await page.getByRole('button', { name: 'Create Schedule' }).click();
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
