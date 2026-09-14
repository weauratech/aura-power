import AxeBuilder from '@axe-core/playwright';
import { test, expect, type Page } from '@playwright/test';
import { requireFixtureNamespace } from './support';

const WCAG_TAGS = ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa'];

const staticRoutes = [
  ['/', 'Cluster Overview'],
  ['/targets', 'Targets'],
  ['/schedule', 'Schedules'],
  ['/rules', 'Schedules'],
  ['/policies', 'Schedules'],
  ['/overrides', 'Overrides'],
  ['/savings', 'Savings'],
  ['/blocked', 'Blocked Targets'],
  ['/audit', 'Audit Log'],
  ['/notifications', 'Notifications'],
  ['/metrics', 'Metrics'],
  ['/pending', 'Pending Approvals'],
  ['/users', 'Users'],
  ['/site-map', 'All pages'],
] as const;

async function scan(page: Page, context: string) {
  const results = await new AxeBuilder({ page }).withTags(WCAG_TAGS).analyze();
  expect(results.violations, `${context}: ${JSON.stringify(results.violations, null, 2)}`).toEqual([]);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth), `${context}: horizontal page overflow`).toBe(true);
}

async function visitWithTheme(page: Page, route: string, heading: string, theme: 'light' | 'dark') {
  await page.addInitScript((selectedTheme) => localStorage.setItem('aura-power-theme', selectedTheme), theme);
  await page.goto(route);
  await expect(page.getByRole('heading', { level: 1, name: heading, exact: true })).toBeVisible();
  await expect(page).toHaveTitle(/ · Aura Power$/);
  await expect(page.locator('h1')).toHaveCount(1);
  await expect(page.getByRole('link', { name: 'Skip to main content' })).toHaveAttribute('href', '#main-content');
  await scan(page, `${route} (${theme})`);
}

for (const [route, heading] of staticRoutes) {
  test(`${route} passes the automated WCAG route contract in both themes`, async ({ page }) => {
    for (const theme of ['light', 'dark'] as const) await visitWithTheme(page, route, heading, theme);
  });
}

test('namespace, target and rule detail routes pass the automated WCAG contract', async ({ page }) => {
  const namespace = requireFixtureNamespace();
  await visitWithTheme(page, `/targets/${encodeURIComponent(namespace)}`, namespace, 'light');
  await visitWithTheme(page, `/targets/${encodeURIComponent(namespace)}/browser-fixture?kind=Deployment`, 'browser-fixture', 'dark');
  await visitWithTheme(page, '/rules/aura-power-accessibility-missing-fixture', 'Rule: aura-power-accessibility-missing-fixture', 'light');
});

const dialogJourneys = [
  ['/schedule', 'Create a new schedule', 'New Schedule'],
  ['/overrides', 'New Override', 'New Override'],
  ['/notifications', 'Create a new notification channel', 'New Channel'],
  ['/users', 'New User', 'New User'],
] as const;

for (const [route, triggerName, dialogName] of dialogJourneys) {
  test(`${route} dialog passes Axe, traps focus and restores it`, async ({ page }) => {
    await page.goto(route);
    const trigger = page.getByRole('button', { name: triggerName, exact: true });
    await trigger.click();
    const dialog = page.getByRole('dialog', { name: dialogName, exact: true });
    await expect(dialog).toBeVisible();
    await scan(page, `${route} ${dialogName} dialog`);
    await page.keyboard.press('Escape');
    await expect(dialog).toBeHidden();
    await expect(trigger).toBeFocused();
  });
}

test('keyboard route changes expose current navigation and move focus to the page heading', async ({ page }) => {
  await page.goto('/');
  const mobile = (page.viewportSize()?.width ?? 1280) < 900;
  if (mobile) await page.getByRole('button', { name: 'Open navigation' }).click();
  const targets = page.getByRole('link', { name: 'Targets', exact: true });
  await targets.focus();
  await page.keyboard.press('Enter');
  const heading = page.getByRole('heading', { level: 1, name: 'Targets' });
  await expect(heading).toBeFocused();
  if (mobile) await page.getByRole('button', { name: 'Open navigation' }).click();
  await expect(targets).toHaveAttribute('aria-current', 'page');
});

test('400% zoom equivalent reflows without document-level horizontal scrolling', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 720 });
  await page.goto('/targets');
  await expect(page.getByRole('heading', { level: 1, name: 'Targets' })).toBeVisible();
  await scan(page, 'targets at a 320 CSS-pixel reflow viewport');
});

test('reduced-motion preference removes non-essential transition duration', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await page.goto('/');
  const durations = await page.evaluate(() => {
    const sample = document.querySelector('main');
    if (!sample) return null;
    const style = getComputedStyle(sample);
    return { animation: style.animationDuration, transition: style.transitionDuration };
  });
  expect(durations).not.toBeNull();
  const cssToken = await page.evaluate(() => getComputedStyle(document.documentElement).getPropertyValue('--ap-duration-base').trim());
  expect(cssToken).toBe('0ms');
});

test('audit loading, empty and API failure states pass the automated WCAG contract', async ({ page }) => {
  await page.route('**/api/v1/audit*', async route => {
    await new Promise(resolve => setTimeout(resolve, 2500));
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ events: [], count: 0, total: 0 }) });
  });
  await page.goto('/audit');
  await expect(page.getByRole('status', { name: 'Loading audit events' })).toBeVisible();
  await scan(page, 'audit loading');
  await expect(page.getByText('No audit events', { exact: true })).toBeVisible();
  await scan(page, 'audit empty');

  await page.unroute('**/api/v1/audit*');
  await page.route('**/api/v1/audit*', route => route.fulfill({
    status: 503,
    contentType: 'application/json',
    body: JSON.stringify({ error: 'induced accessibility outage' }),
  }));
  await page.reload();
  await expect(page.getByRole('alert')).toContainText('induced accessibility outage');
  await scan(page, 'audit API failure');
});
