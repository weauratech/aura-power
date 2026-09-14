import { test, expect } from '@playwright/test';

interface ScriptMetric {
  name: string;
  encodedBodySize: number;
  transferSize: number;
  duration: number;
}

async function scriptMetrics(page: import('@playwright/test').Page): Promise<ScriptMetric[]> {
  return page.evaluate(() => performance.getEntriesByType('resource')
    .filter((entry): entry is PerformanceResourceTiming =>
      entry instanceof PerformanceResourceTiming && entry.initiatorType === 'script')
    .map(entry => ({
      name: new URL(entry.name).pathname.split('/').pop() ?? entry.name,
      encodedBodySize: entry.encodedBodySize,
      transferSize: entry.transferSize,
      duration: entry.duration,
    })));
}

async function attach(testInfo: import('@playwright/test').TestInfo, route: string, metrics: ScriptMetric[]) {
  await testInfo.attach(`route-performance-${route.replace(/\W+/g, '-')}`, {
    body: Buffer.from(JSON.stringify({ route, scripts: metrics }, null, 2)),
    contentType: 'application/json',
  });
}

test('a cold non-chart route does not download the chart bundle', async ({ page }, testInfo) => {
  await page.goto('/targets');
  await expect(page.getByRole('heading', { level: 1, name: 'Targets' })).toBeVisible();
  const metrics = await scriptMetrics(page);
  await attach(testInfo, '/targets', metrics);
  expect(metrics.length).toBeGreaterThan(0);
  expect(metrics.some(({ name }) => name.startsWith('charts-vendor-'))).toBe(false);
  expect(metrics.some(({ name }) => name.startsWith('Dashboard-'))).toBe(false);
});

test('the dashboard loads its chart bundle on demand and records route timings', async ({ page }, testInfo) => {
  await page.goto('/');
  await expect(page.getByRole('heading', { level: 1, name: 'Cluster Overview' })).toBeVisible();
  const metrics = await scriptMetrics(page);
  await attach(testInfo, '/', metrics);
  expect(metrics.some(({ name }) => name.startsWith('charts-vendor-'))).toBe(true);
  expect(metrics.every(({ duration }) => Number.isFinite(duration) && duration >= 0)).toBe(true);
});
