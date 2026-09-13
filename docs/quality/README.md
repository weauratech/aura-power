# Aura Power quality campaign

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

The remote browser smoke is read-only. Mutating Kubernetes acceptance uses the
disposable Kind journey or the EKS runner with UID ownership checks and a
detached watchdog. Load contracts run explicitly through `make quality-load`.

The EKS runner compares the kubeconfig endpoint with `aws eks describe-cluster`,
uses a fixed context name, refuses preexisting names, and verifies UID plus
campaign labels before cleanup. A detached watchdog starts before the policy is
created and restores/removes the fixture if the parent ends or the hard deadline
is reached. Cleanup failure changes the run result to failure.
