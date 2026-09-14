import { defineConfig, devices } from '@playwright/test';

const baseURL = process.env.AURA_E2E_BASE_URL;
if (!baseURL) throw new Error('AURA_E2E_BASE_URL is required; the suite has no production default.');
if (!process.env.AURA_E2E_FIXTURE_NAMESPACE) {
  throw new Error('AURA_E2E_FIXTURE_NAMESPACE is required; browser tests must target dedicated fixtures.');
}

const mutationEnabled = process.env.AURA_E2E_ALLOW_MUTATION === 'true';
if (mutationEnabled && !process.env.AURA_E2E_KUBECONFIG) {
  throw new Error('AURA_E2E_KUBECONFIG is required for mutating journeys so workload effects can be verified independently.');
}
const expandedBrowsers = process.env.AURA_E2E_BROWSERS === 'all';
const authenticatedProjects = expandedBrowsers
  ? [
      { name: 'chromium', use: { ...devices['Desktop Chrome'] } },
      { name: 'firefox', use: { ...devices['Desktop Firefox'] } },
      { name: 'webkit', use: { ...devices['Desktop Safari'] } },
      { name: 'mobile-chromium', use: { ...devices['Pixel 7'] } },
    ]
  : [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }];

export default defineConfig({
  testDir: './tests',
  outputDir: 'test-results',
  timeout: 30_000,
  expect: { timeout: 10_000 },
  fullyParallel: false,
  forbidOnly: Boolean(process.env.CI),
  retries: 0,
  workers: process.env.CI ? 1 : undefined,
  grepInvert: mutationEnabled ? undefined : /@mutation/,
  reporter: [['html', { open: 'never' }], ['junit', { outputFile: 'test-results/junit.xml' }], ['list']],
  use: {
    baseURL,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    video: 'retain-on-failure',
  },
  projects: [
    { name: 'setup', testMatch: /auth\.setup\.ts/ },
    {
      name: 'unauthenticated',
      testMatch: /auth\.spec\.ts/,
      use: { ...devices['Desktop Chrome'] },
    },
    ...authenticatedProjects.map((project) => ({
      ...project,
      dependencies: ['setup'],
      testIgnore: [/auth\.setup\.ts/, /auth\.spec\.ts/],
      use: { ...project.use, storageState: 'tests/.auth/admin.json' },
    })),
  ],
});
