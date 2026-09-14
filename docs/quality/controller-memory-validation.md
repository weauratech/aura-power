# Controller memory validation

Issue #12 is gated by two complementary checks:

- `make quality-load` runs the deterministic 5,000-workload discovery allocation guard three times. It prevents a return to per-namespace API lists and bounds memory retained by one discovery result.
- `scripts/quality/kind-memory-soak.sh` creates 1,000 zero-replica Deployments across 20 owned namespaces in disposable Kind, samples both controller replicas for three minutes under the 256 MiB container limit, and forces leader replacement.

The native gate records each Pod UID, restart count, and OOM state before the soak. It fails if discovery misses a fixture, either replica lacks pprof or memory metrics, either heap reaches `GOMEMLIMIT`, either RSS reaches 256 MiB, per-Pod second-half heap growth exceeds 32 MiB, a restart or OOM termination is observed before leader deletion, both replacement replicas are not healthy, leadership does not move to a different Pod, or cleanup cannot prove that its namespaces and targets are gone. Its bounds reject more than 5,000 workloads, 100 namespaces, or 15 minutes.

The production baseline was refreshed read-only on 2026-09-13 through the
`hub-eks-aura-prd-readonly` profile. `eks-aura-prd` contained 48 supported
workloads: 41 Deployments, five StatefulSets, and two CronJobs. The installed
controller was `v2.1.7`, with one replica, a 128 MiB request, a 256 MiB limit,
eight restarts, and `OOMKilled` as its latest termination reason. All Aura Power
policy, target, schedule, override, audit, and notification-channel collections
were empty. The default native gate deliberately exercises 1,000 workloads,
more than 20 times the observed production cardinality, while keeping the same
256 MiB pod limit.

Each run writes `samples.csv`, metrics snapshots, heap profiles before and after failover, port-forward logs, and `summary.json` below `artifacts/quality/memory-<run>`. CI uploads that directory even when the gate fails. Raw profiles remain CI artifacts and are not committed because they may contain process data.

The profiler is disabled by default. Set `controller.config.pprofBindAddress=127.0.0.1:6060` only for an authorized diagnostic run and access it with `kubectl port-forward`. The process rejects wildcard and non-loopback listeners.

Enable `prometheusRule.enabled` in clusters with Prometheus Operator and kube-state-metrics. The chart then installs warnings for Go heap and container working-set pressure plus a critical alert for OOM restarts. Tune ratios only from recorded peak and steady-state evidence.

## Local validation evidence

The release-default run `finalissues3` was exercised against the final campaign Kind
cluster with 1,000 workloads in 20 namespaces for 180 seconds. It recorded a
15,034,880-byte peak heap, 67,780,608-byte peak RSS, 4,234,736 bytes of
second-half heap growth, and a 201,326,592-byte `GOMEMLIMIT`. No OOM occurred;
heap profiles were captured from both leaders and failover succeeded. Cleanup
returned the controller to one ready replica, disabled pprof, and removed every
fixture namespace and target. A prior 100-workload smoke recorded a 7,266,144-byte
peak heap and 43,311,104-byte peak RSS.
