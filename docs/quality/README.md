# Aura Power quality campaign

Controller cardinality, heap profiling, memory budgets and leader failover are documented in [controller-memory-validation.md](controller-memory-validation.md).

This directory records the permanent quality campaign introduced against commit
`4727bc56f144`. The product contract is: select exactly the intended workloads,
change them at the intended time, restore their prior state, and explain the
outcome truthfully.

## Test classes

- Normal suites must pass and protect behavior already supported.
- Tests tagged `acceptance` encode product contracts discovered by the campaign.
  They are release gates and must remain green; a reproduced bug is fixed rather
  than skipped, retried, or weakened.
- Kind runs exercise native Kubernetes controllers, CRDs, status writes, workload
  readiness, recovery, and Helm. `scripts/quality/kind-hpa-journey.sh` proves
  exact HPA ownership discovery, default blocking, opted-in recovery, and stable
  scale-field contention. `scripts/quality/kind-argocd-journey.sh` also
  installs integrity-pinned Argo CD v2.14.20 and validates the supported field
  ownership contract against real Application and ApplicationSet controllers.
- `scripts/quality/kind-release-upgrade-journey.sh` creates its own disposable
  Kind cluster, installs the v2.1.7 chart and binaries, and upgrades the complete
  release to the candidate. It verifies CRD migration, UID identity, server PVC
  and refresh-session continuity, auth and webhook TLS Secret stability, durable
  off/on reconciliation, leader-elected discovery, audit events, and fixture plus
  cluster cleanup. Power-down is conditional on the workload `resourceVersion`
  captured with the snapshot, so a concurrent scale is recaptured instead of lost.
- `scripts/quality/kind-cli-journey.sh` runs the CLI root, login, logout, whoami,
  discover, status, explain, YAML preview, override creation, and savings commands
  against the real Kind server. It checks failure exit codes and removes its
  UID-checked namespace, override, and targets.
- `scripts/quality/kind-notification-journey.sh` sends real transition events to
  a controlled in-cluster HTTP receiver through a Secret-backed URL. It proves
  sanitized HTTP failure status, recovery on a later transition, durable
  counters, receiver-to-audit correlation, and UID/label-guarded cleanup. Central
  off/on events retry the audit-to-queue handoff by deterministic audit ID and
  suppress replay once an attempt is persisted. Ambiguous provider outcomes use
  durable delivery semantics. The native receiver verifies stable idempotency
  keys across restart/replay; this is not an exactly-once delivery claim.
- `make quality-envtest` starts a real local Kubernetes API server and proves that
  both registered validators accept valid resources and reject invalid resources
  through admission.
- EKS runs are manual. They require an explicit kubeconfig and production-cluster
  acknowledgement, create a unique dedicated namespace, mutate one journey at a
  time, and restore/delete only campaign-labelled fixtures.
- Browser mutation tests require `AURA_E2E_ALLOW_MUTATION=true`, an explicit base URL,
  `AURA_E2E_KUBECONFIG` for independent workload assertions, and credentials supplied
  at runtime. There is no production default.

Run the non-mutating layer:

```bash
make quality
```

Run the acceptance contracts:

```bash
make quality-acceptance
```

Run the core Kubernetes journey after installing Aura Power in the disposable
Kind cluster:

```bash
KUBECONFIG=/absolute/path/kind.kubeconfig \
  scripts/quality/kind-core-journey.sh
```

Run the admitted EKS journey only after read-only inventory confirms the
installed version, empty fixture name, verified notification-suppression
capability and image digests, and the required permissions:

```bash
export KUBECONFIG=/absolute/path/eks-operations.kubeconfig
controller_node="$(kubectl get pods -n aura-system \
  -l app.kubernetes.io/instance=aura-power,app.kubernetes.io/component=controller \
  -o jsonpath='{.items[0].spec.nodeName}')"
controller_arch="$(kubectl get node "$controller_node" \
  -o jsonpath='{.metadata.labels.kubernetes\.io/arch}')"
case "$controller_arch" in
  amd64) runtime_field=linuxAmd64 ;;
  arm64) runtime_field=linuxArm64 ;;
  *) echo "unsupported controller node architecture: $controller_arch" >&2; exit 2 ;;
esac
controller_index_digest="$(jq -er '.controllerDigests.index' release-manifest.json)"
controller_runtime_digest="$(jq -er --arg field "$runtime_field" \
  '.controllerDigests[$field]' release-manifest.json)"

AURA_POWER_AWS_PROFILE=hub-eks-aura-prd-operations \
  AURA_POWER_EKS_MUTATION_ACK=eks-aura-prd \
  AURA_POWER_EXPECTED_CONTROLLER_DIGEST="$controller_index_digest" \
  AURA_POWER_EXPECTED_CONTROLLER_RUNTIME_DIGEST="$controller_runtime_digest" \
  scripts/quality/eks-core-journey.sh
```

Raw kubeconfigs, credentials, logs and cluster exports belong in restricted
evidence storage and must never be committed.

The EKS runner prints its private recovery directory on any unverified cleanup.
That directory contains the mode-0600 kubeconfig, watchdog/channel logs, atomic
resource state and the active-command PID plus boot/start identity. Keep it
private. The watchdog will not remove `mutation-in-progress` until the recorded
command tree has terminated, and it preserves every artifact when identity or
API absence is inconclusive. `cleanup-started` is deliberately fail-closed and
has no automatically reclaimed PID owner. If the cleanup executor itself is
forcibly killed, use the printed recovery path to prove that no core/watchdog
process still has that directory open and that every exact identity recorded in
`active-command` has ended. Only then remove the empty `cleanup-started`
directory, reconstruct the required environment from the private kubeconfig,
`state.json` and campaign invocation, and rerun
`scripts/quality/eks-fixture-watchdog.sh cleanup`. Never delete the marker merely
because a remembered PID is not visible, and never delete the recovery
directory until UID-preconditioned cleanup and final NotFound checks succeed.

The remote browser smoke is read-only. Mutating Kubernetes acceptance uses the
disposable Kind journey or the EKS runner with UID ownership checks and a
detached watchdog. Load contracts run explicitly through `make quality-load`.

The EKS runner compares the kubeconfig endpoint with `aws eks describe-cluster`,
uses a fixed context name, refuses preexisting names, and verifies UID plus
campaign labels before cleanup. A detached watchdog starts before the policy is
created and restores/removes the fixture if the parent ends or the hard deadline
is reached. Cleanup failure changes the run result to failure. The fixture
namespace sets `power.aura.sh/notification-policy=disabled`. The workload label
is not an authority for suppressing operational notifications. Before mutation,
the controller reads the workload and Namespace directly, verifies both live
UIDs, and persists the decision in `PowerTarget.status.action`. Transition
audits remain durable and expose the same immutable decision through
`PowerAuditEvent.spec.notificationSuppressed`, the HTTP API, CSV export, and
the panel. The runner requires the named controller readiness capability and
two independently verified digests before it creates a policy:
`AURA_POWER_EXPECTED_CONTROLLER_DIGEST` is the release index digest pinned in
the Deployment, while `AURA_POWER_EXPECTED_CONTROLLER_RUNTIME_DIGEST` is the
signed platform child digest selected from `controllerDigests` for the actual
controller node architecture and expected in the controller container's CRI
`imageID`. Verify the adjacent `release-manifest.json.bundle` as described in
`docs/release-security.md` before trusting either value. A sanitized watch of notification attempt audit references starts
before policy creation and remains active through a bounded quiescence period;
it is complementary evidence, while the synchronous persisted-Spec gate is the
delivery safety guarantee.
