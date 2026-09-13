import { test, expect } from '@playwright/test';
import { execFileSync } from 'node:child_process';
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

  test('@mutation creates one exact workload reference when homonyms exist', async ({ page }) => {
    test.setTimeout(180_000);
    const namespace = requireFixtureNamespace();
    const homonymNamespace = process.env.AURA_E2E_HOMONYM_NAMESPACE ?? `${namespace}-homonym`;
    const name = `codex-exact-${Date.now()}`;
    const buttonName = `Create schedule for ${namespace}/Deployment/browser-fixture`;
    const kubeconfig = process.env.AURA_E2E_KUBECONFIG;
    if (!kubeconfig) throw new Error('AURA_E2E_KUBECONFIG is required for this mutating journey.');
    const kubectl = (...args: string[]) =>
      execFileSync('kubectl', ['--kubeconfig', kubeconfig, ...args], { encoding: 'utf8' }).trim();
    const fixtureUID = kubectl('get', 'deployment', 'browser-fixture', '--namespace', namespace, '--output', 'jsonpath={.metadata.uid}');
    const homonymUID = kubectl('get', 'deployment', 'browser-fixture', '--namespace', homonymNamespace, '--output', 'jsonpath={.metadata.uid}');
    const originalReplicas = Number(kubectl('get', 'deployment', 'browser-fixture', '--namespace', namespace, '--output', 'jsonpath={.spec.replicas}'));
    const originalHomonymReplicas = Number(kubectl('get', 'deployment', 'browser-fixture', '--namespace', homonymNamespace, '--output', 'jsonpath={.spec.replicas}'));
    let submittedPolicy:
      | {
          metadata: Record<string, string>;
          spec: {
            scope: { targetRefs: Array<Record<string, string>> };
            schedule: { desiredState: string };
          };
        }
      | undefined;
    const replicas = async (targetNamespace: string) => {
      const response = await page.request.get(`/api/v1/targets?namespace=${encodeURIComponent(targetNamespace)}`);
      expect(response.status()).toBe(200);
      const body = (await response.json()) as {
        targets?: Array<{
          spec: { targetRef: { name: string; kind: string } };
          status: { observedState: { replicas: number } };
        }>;
      };
      return body.targets?.find((target) => target.spec.targetRef.name === 'browser-fixture' && target.spec.targetRef.kind === 'Deployment')?.status.observedState.replicas;
    };
    const kubernetesReplicas = (targetNamespace: string) => {
      return Number(kubectl('get', 'deployment', 'browser-fixture', '--namespace', targetNamespace, '--output', 'jsonpath={.spec.replicas}'));
    };
    // Establish the independent Kubernetes baseline before observing any UI
    // request. A stale PowerTarget status cannot satisfy these assertions.
    expect(kubernetesReplicas(namespace)).toBe(1);
    expect(kubernetesReplicas(homonymNamespace)).toBe(1);
    const responsePromise = page.waitForResponse((response) => response.request().method() === 'POST' && response.url().endsWith('/api/v1/policies'));
    try {
      await page.getByPlaceholder(/Search by name or namespace/).fill('browser-fixture');
      const trigger = page.getByRole('button', {
        name: buttonName,
        exact: true,
      });
      await expect(trigger).toHaveCount(1);
      await trigger.click();
      const drawer = page.getByRole('dialog', { name: 'New Schedule' });
      await drawer.getByRole('textbox', { name: /^Name/ }).fill(name);
      await drawer.getByRole('textbox', { name: 'Start' }).fill('00:00');
      await drawer.getByRole('textbox', { name: 'End' }).fill('23:59');
      await drawer.getByText('Sun', { exact: true }).click();
      await drawer.getByText('Sat', { exact: true }).click();
      await drawer.getByRole('button', { name: 'Create Schedule', exact: true }).click();
      const response = await responsePromise;
      expect(response.status()).toBe(201);
      submittedPolicy = response.request().postDataJSON() as typeof submittedPolicy;
      expect(submittedPolicy?.spec.scope.targetRefs).toHaveLength(1);
      expect(submittedPolicy?.spec.scope.targetRefs[0]).toMatchObject({
        namespace,
        name: 'browser-fixture',
        kind: 'Deployment',
        apiVersion: 'apps/v1',
      });
      expect(submittedPolicy?.spec.scope.targetRefs[0].uid).toBeTruthy();
      await expect.poll(() => replicas(namespace), { timeout: 90_000 }).toBe(0);
      await expect.poll(() => replicas(homonymNamespace), { timeout: 90_000 }).toBe(1);
      await expect.poll(() => kubernetesReplicas(namespace), { timeout: 90_000 }).toBe(0);
      await expect.poll(() => kubernetesReplicas(homonymNamespace), { timeout: 90_000 }).toBe(1);
    } finally {
      let recoveryError: unknown;
      if (submittedPolicy) {
        try {
          submittedPolicy.spec.schedule.desiredState = 'on';
          await page.request.put(`/api/v1/policies/aura-system/${name}`, { data: submittedPolicy });
        } catch (error) {
          recoveryError = error;
        }
      }
      try {
        await page.request.delete(`/api/v1/policies/aura-system/${name}`);
      } catch (error) {
        recoveryError ??= error;
      }

      // Recovery is independent of the server/API under test. Retry rule
      // removal, verify both identities, and still attempt both replica
      // restorations if one cleanup operation has a transient failure.
      const cleanupErrors: unknown[] = [];
      let policyDeleted = false;
      for (let attempt = 0; attempt < 3 && !policyDeleted; attempt += 1) {
        try {
          kubectl('delete', 'powerpolicy', name, '--namespace', 'aura-system', '--ignore-not-found', '--wait=true', '--timeout=60s');
          policyDeleted = true;
        } catch (error) {
          if (attempt === 2) cleanupErrors.push(error);
        }
      }
      const restore = (targetNamespace: string, expectedUID: string, replicas: number) => {
        try {
          const actualUID = kubectl('get', 'deployment', 'browser-fixture', '--namespace', targetNamespace, '--output', 'jsonpath={.metadata.uid}');
          if (actualUID !== expectedUID) throw new Error(`refusing recovery for ${targetNamespace}: fixture UID changed`);
          kubectl('scale', 'deployment/browser-fixture', '--namespace', targetNamespace, `--replicas=${replicas}`);
        } catch (error) {
          cleanupErrors.push(error);
        }
      };
      restore(namespace, fixtureUID, originalReplicas);
      restore(homonymNamespace, homonymUID, originalHomonymReplicas);
      if (cleanupErrors.length === 0) {
        await expect.poll(() => kubernetesReplicas(namespace), { timeout: 90_000 }).toBe(originalReplicas);
        await expect.poll(() => kubernetesReplicas(homonymNamespace), { timeout: 90_000 }).toBe(originalHomonymReplicas);
      }
      if (cleanupErrors.length > 0) throw new AggregateError(cleanupErrors, 'independent Kubernetes recovery failed');
      if (recoveryError) throw recoveryError;
    }
  });
});
