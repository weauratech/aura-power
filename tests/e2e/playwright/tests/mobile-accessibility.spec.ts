import AxeBuilder from '@axe-core/playwright';
import { test, expect } from '@playwright/test';
import { waitForMobileNavigationClosed } from './support';

test.describe('mobile navigation and drawers', () => {
  test.skip(({ isMobile }) => !isMobile, 'mobile browser contract');

  test('supports named controls, keyboard dismissal, focus return, and a viewport-width drawer', async ({ page }) => {
    await page.setViewportSize({ width: 320, height: 720 });
    await page.goto('/');

    const menuButton = page.getByRole('button', { name: 'Open navigation' });
    await menuButton.focus();
    await page.keyboard.press('Enter');
    await expect(page.getByRole('navigation', { name: 'Primary navigation' })).toBeVisible();
    await page.keyboard.press('Escape');
    await expect(menuButton).toBeFocused();

    await menuButton.click();
    await page.getByRole('link', { name: 'Schedules', exact: true }).click();
    await waitForMobileNavigationClosed(page);
    await expect(page.getByRole('heading', { name: 'Schedules', exact: true })).toBeVisible();

    const trigger = page.getByRole('button', { name: 'Create a new schedule', exact: true });
    await trigger.click();
    const drawer = page.getByRole('dialog', { name: 'New Schedule' });
    await expect(drawer).toBeVisible();
    await expect(page.getByRole('button', { name: 'Close schedule drawer' })).toBeFocused();
    const box = await drawer.boundingBox();
    expect(box).not.toBeNull();
    expect(box!.width).toBeLessThanOrEqual(page.viewportSize()!.width + 0.5);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true);

    const accessibility = await new AxeBuilder({ page }).include('[role="dialog"]').analyze();
    expect(accessibility.violations).toEqual([]);

    await page.keyboard.press('Escape');
    await expect(drawer).toBeHidden();
    await expect(trigger).toBeFocused();
  });
});
