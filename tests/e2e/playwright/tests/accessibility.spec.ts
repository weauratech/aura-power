import AxeBuilder from '@axe-core/playwright';
import { test, expect, type Locator, type Page } from '@playwright/test';
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
  ['/cluster-metrics', 'Metrics'],
  ['/pending', 'Pending Approvals'],
  ['/users', 'Users'],
  ['/site-map', 'All pages'],
] as const;

async function scan(page: Page, context: string) {
  const results = await new AxeBuilder({ page }).withTags(WCAG_TAGS).analyze();
  expect(results.violations, `${context}: ${JSON.stringify(results.violations, null, 2)}`).toEqual([]);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth), `${context}: horizontal page overflow`).toBe(true);
}

async function waitForOpaque(locator: Locator) {
  await expect.poll(() => locator.evaluate(element => {
    let opacity = 1;
    for (let current: Element | null = element; current; current = current.parentElement) {
      opacity *= Number.parseFloat(getComputedStyle(current).opacity || '1');
    }
    return opacity;
  })).toBe(1);
}

async function visitWithTheme(page: Page, route: string, heading: string, theme: 'light' | 'dark') {
  await page.addInitScript((selectedTheme) => localStorage.setItem('aura-power-theme', selectedTheme), theme);
  await page.goto(route);
  await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
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

test('cluster metrics deep link remains distinct from the Prometheus endpoint', async ({ page }) => {
  const prometheus = await page.request.get('/metrics');
  expect(prometheus.ok()).toBe(true);
  expect(prometheus.headers()['content-type']).toContain('text/plain');
  expect(await prometheus.text()).not.toContain('<!doctype html>');

  await page.goto('/cluster-metrics');
  await expect(page.getByRole('heading', { level: 1, name: 'Metrics' })).toBeVisible();
});

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
    await waitForOpaque(dialog);
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
  for (const control of await page.getByRole('group', { name: 'Filter targets by state' }).getByRole('button').all()) {
    const box = await control.boundingBox();
    expect(box?.width).toBeGreaterThanOrEqual(24);
    expect(box?.height).toBeGreaterThanOrEqual(24);
  }
  const grouping = page.getByRole('button', { name: /Grouped by NS|Flat/ });
  const groupingBox = await grouping.boundingBox();
  expect(groupingBox?.width).toBeGreaterThanOrEqual(24);
  expect(groupingBox?.height).toBeGreaterThanOrEqual(24);
});

test('reduced-motion preference removes non-essential transition duration', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await page.goto('/');
  await expect(page.locator('main')).toBeVisible();
  const themeButton = page.getByRole('button', { name: /Switch to (light|dark) theme/ });
  await expect(themeButton).toBeVisible();
  const transitionDuration = await themeButton.evaluate(element => getComputedStyle(element).transitionDuration);
  expect(transitionDuration.split(',').every(value => Number.parseFloat(value) <= 0.00001)).toBe(true);
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
  await expect(page.getByRole('alert')).toContainText('Induced accessibility outage');
  await expect(page.getByRole('heading', { level: 1, name: 'Audit Log' })).toBeVisible();
  await scan(page, 'audit API failure');
});

test('API failure states preserve each route heading and document structure', async ({ page }) => {
  test.setTimeout(90_000);
  await page.route('**/api/v1/**', route => {
    if (new URL(route.request().url()).pathname === '/api/v1/auth/me') return route.fallback();
    return route.fulfill({
      status: 503,
      contentType: 'application/json',
      body: JSON.stringify({ error: 'induced route failure' }),
    });
  });

  const namespace = requireFixtureNamespace();
  const failureRoutes = [
    ['/', 'Cluster Overview'],
    ['/targets', 'Targets'],
    [`/targets/${encodeURIComponent(namespace)}`, namespace],
    [`/targets/${encodeURIComponent(namespace)}/missing?kind=Deployment`, 'missing'],
    ['/schedule', 'Schedules'],
    ['/rules', 'Schedules'],
    ['/policies', 'Schedules'],
    ['/rules/missing', 'Rule: missing'],
    ['/overrides', 'Overrides'],
    ['/savings', 'Savings'],
    ['/blocked', 'Blocked Targets'],
    ['/audit', 'Audit Log'],
    ['/notifications', 'Notifications'],
    ['/cluster-metrics', 'Metrics'],
    ['/pending', 'Pending Approvals'],
    ['/users', 'Users'],
  ] as const;

  for (const [route, heading] of failureRoutes) {
    await page.goto(route);
    await expect(page.getByRole('heading', { level: 1, name: heading, exact: true })).toBeVisible();
    await expect(page.locator('h1')).toHaveCount(1);
    await expect(page.getByRole('alert')).toBeVisible({ timeout: 15_000 });
    await scan(page, `${route} API failure`);
  }
});
