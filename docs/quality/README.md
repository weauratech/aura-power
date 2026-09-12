# Aura Power quality campaign

This directory records the permanent quality campaign introduced against commit
`4727bc56f144`. The product contract is: select exactly the intended workloads,
change them at the intended time, restore their prior state, and explain the
outcome truthfully.

## Test classes

- Normal suites must pass and protect behavior already supported.
- Tests tagged `acceptance` encode violated product contracts. They intentionally
  fail until the corresponding product bug is fixed; CI runs both Go and web,
  publishes both reports, and keeps the acceptance job failing.
- Kind runs exercise native Kubernetes controllers, CRDs, status writes, workload
  readiness, recovery, and Helm.
- EKS runs are manual. They require an explicit kubeconfig and production-cluster
  acknowledgement, create a unique dedicated namespace, mutate one journey at a
  time, and restore/delete only campaign-labelled fixtures.
- Browser mutation tests require `AURA_E2E_ALLOW_MUTATION=true`, an explicit base URL,
  and credentials supplied at runtime. There is no production default.

Run the non-mutating layer:

```bash
make quality
```

Run violated contracts for triage:

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
installed version, empty fixture name, no real notification channel affected,
and the required permissions:

```bash
KUBECONFIG=/absolute/path/eks-operations.kubeconfig \
  AURA_POWER_AWS_PROFILE=hub-eks-aura-prd-operations \
  AURA_POWER_EKS_MUTATION_ACK=eks-aura-prd \
  scripts/quality/eks-core-journey.sh
```

Raw kubeconfigs, credentials, logs and cluster exports belong in restricted
evidence storage and must never be committed.

The legacy Go E2E, load, and shell smoke suites contain fixed resource scopes.
They are restricted in code to loopback servers and are excluded from the live
smoke workflow until they use unique, fixture-scoped resources throughout. The
workflow runs only the fixture-scoped browser journey and compares its target
and namespace with the protected environment secrets
`AURA_POWER_SMOKE_ALLOWED_ORIGIN` and `AURA_POWER_SMOKE_ALLOWED_NAMESPACE`
before exposing credentials.

The EKS runner compares the kubeconfig endpoint with `aws eks describe-cluster`,
uses a fixed context name, refuses preexisting names, and verifies UID plus
campaign labels before cleanup. A detached watchdog starts before the policy is
created and restores/removes the fixture if the parent ends or the hard deadline
is reached. Cleanup failure changes the run result to failure.
