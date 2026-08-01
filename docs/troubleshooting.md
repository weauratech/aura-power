# Troubleshooting

Common issues and resolution steps for Aura Power operators.

## Workload not powering down

**Symptom**: Policy is active but the target remains `on`.

1. Check if the target is blocked:
   ```bash
   kubectl get pt -n aura-system -o wide
   ```
   Look for `Blocked=true` in the output.

2. Inspect block reasons:
   ```bash
   kubectl get pt <name> -n <namespace> -o jsonpath='{.status.blockReasons}'
   ```
   Common reasons:
   - **HPA detected**: Workload has an active HPA. Remove it or opt-in via annotation.
   - **ArgoCD managed**: ArgoCD will revert replica changes. Add the annotation `power.aura.sh/opt-in: "true"` to the workload.
   - **Flux managed**: Similar to ArgoCD. Opt-in required.

3. Check divergent state:
   ```bash
   kubectl get pt <name> -n <namespace> -o jsonpath='{.status.divergent}'
   ```
   If `true`, the controller is waiting for the action to take effect (pods terminating).

4. Check consecutive failures:
   ```bash
   kubectl get pt <name> -n <namespace> -o jsonpath='{.status.consecutiveFailures}'
   ```
   Non-zero means the controller is failing to execute the action. Check controller logs.

## Workload not restoring

**Symptom**: Override or schedule says `on` but workload stays at 0 replicas.

1. Verify snapshot exists:
   ```bash
   kubectl get pt <name> -n <namespace> -o jsonpath='{.status.snapshot}'
   ```
   If `available: false`, the snapshot was not captured before power-down. Manually scale the workload.

2. Check controller logs:
   ```bash
   kubectl logs -n aura-system -l app.kubernetes.io/component=controller --tail=50
   ```

## Server readyz failing

**Symptom**: Server pod is not ready, `/readyz` returns 503.

1. Check which component failed:
   ```bash
   kubectl exec -n aura-system <server-pod> -- wget -qO- http://localhost:8080/readyz
   ```
   Response includes `component` field (`kubernetes` or `database`).

2. **kubernetes**: Server cannot reach the K8s API. Check RBAC and network policies.
3. **database**: SQLite file is corrupted or inaccessible. Check the PVC mount:
   ```bash
   kubectl describe pvc -n aura-system -l app.kubernetes.io/component=server
   ```

## Policy not matching targets

**Symptom**: Policy exists but `status.affectedTargets` is 0.

1. Verify scope selectors:
   ```bash
   kubectl get pp <name> -n aura-system -o yaml
   ```
   Check that `spec.scope.namespaces` or `spec.scope.workloadLabels` match existing workloads.

2. Verify targets are discovered:
   ```bash
   kubectl get pt --all-namespaces
   ```
   If no targets exist in the expected namespace, the controller may not be watching it. Check controller RBAC.

3. Check namespace exclusions. System namespaces (`kube-system`, `kube-public`, `aura-system`) are excluded by default.

## Override not taking effect

**Symptom**: Override created but workload state unchanged.

1. Check if the override has expired:
   ```bash
   kubectl get po <name> -n aura-system
   ```
   Look at the `Phase` column. If `Expired`, the override is no longer active.

2. Verify priority. The override must have higher priority than conflicting policies:
   ```bash
   kubectl get pt <target-name> -n <namespace> -o jsonpath='{.status.winningRule}'
   ```

## High memory usage on controller

**Symptom**: Controller OOMKilled or high memory consumption.

1. Check number of targets:
   ```bash
   kubectl get pt --all-namespaces --no-headers | wc -l
   ```
   Each target adds ~2KB to controller memory. For 1000+ targets, increase memory limits.

2. Review reconcile interval. Default is 30s. If too aggressive for your cluster size, this can be tuned via `--reconcile-interval` flag.

## Metrics not appearing in Prometheus

**Symptom**: `/metrics` returns data but Prometheus doesn't scrape.

1. Verify ServiceMonitor exists (if using Prometheus Operator):
   ```bash
   kubectl get servicemonitor -n aura-system
   ```
   Enable in Helm: `--set serviceMonitor.enabled=true`

2. Check that Prometheus can reach the server service:
   ```bash
   kubectl get svc -n aura-system -l app.kubernetes.io/component=server
   ```

3. Verify NetworkPolicy allows Prometheus ingress to port 8080.

## CLI authentication issues

**Symptom**: `aura-power` CLI returns 401 Unauthorized.

1. Re-authenticate:
   ```bash
   aura-power login --server https://power.your-domain.com
   ```

2. Check token expiration. Access tokens expire after 1h by default. The CLI auto-refreshes, but if the refresh token (7d) is also expired, re-login is required.

3. Verify the server URL matches the configured gateway/ingress hostname.

## Savings not accumulating

**Symptom**: `status.savings` shows zero values.

1. Savings accumulate only when a workload transitions to `off`. Check if the target has been powered down at least once.

2. Verify snapshot has resource data:
   ```bash
   kubectl get pt <name> -n <namespace> -o jsonpath='{.status.snapshot.resources}'
   ```
   If empty, the workload had no resource requests/limits set when captured.

3. Savings are calculated per reconcile cycle while the workload is off. If the controller was restarted, accumulated savings persist in the CRD status and are not lost.

## Controller not starting

**Symptom**: Controller pod in CrashLoopBackOff.

1. Check logs:
   ```bash
   kubectl logs -n aura-system -l app.kubernetes.io/component=controller
   ```

2. Common causes:
   - **CRDs not installed**: Ensure CRDs are present (`kubectl get crd | grep power.aura.sh`)
   - **RBAC insufficient**: Check ClusterRole bindings
   - **Leader election failure**: If running multiple replicas, ensure lease objects are accessible
