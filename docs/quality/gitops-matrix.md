# GitOps compatibility matrix

The authority question is evaluated separately for diff detection, sync,
runtime health, and ownership of `spec.replicas` or `spec.suspend`.

| Tracking and sync mode | Expected contract | Evidence | Status |
|---|---|---|---|
| Argo annotation tracking, self-heal off | Aura may change an opted-in fixture; Argo reports drift without undoing it | dedicated Kind fixture | pending execution |
| Argo annotation tracking, self-heal on | Without an agreed field owner, Argo and Aura must not oscillate | Kind observed 33 power-down audit events and 10 execution errors in about 80 seconds while Argo held replicas at 2 | failed |
| `ignoreDifferences` only | Difference is hidden from comparison, but sync can still write replicas | official Argo contract and fixture | pending execution |
| `ignoreDifferences` plus `RespectIgnoreDifferences=true` | Existing resource leaves ignored replicas to Aura while other Git changes sync | Kind held replicas at 0 and Application `Synced` across five consecutive observations | pass-real for replica ownership; unrelated Git update pending |
| Standard tracking label | Target ownership must be detected and require explicit opt-in | domain tests | partial |
| Custom tracking label | Configured label must be consumed by discovery | no configurable implementation found | unsupported |
| ApplicationSet-generated Application | Same field-authority rules apply to generated apps | no end-to-end evidence yet | inconclusive |
| HPA/KEDA | Controller ownership and scale subresource must be explicit | discovery supports neither as a first-class target | unsupported/inconclusive |
| Flux | Ownership detection exists but no full reconciliation contract | unit signal only | pass-simulated only |

`ignoreDifferences` affects comparison. Argo CD requires
`RespectIgnoreDifferences=true` to apply the same rule during sync, and this
pre-patch behavior applies only once the live resource exists. The acceptance
gate therefore requires observing both the Aura transition and a later,
unrelated Git change on the same existing resource.

The baseline ownership experiment preceded the snapshot remediation. The
candidate now preserves and restores exact snapshots in native Kind and stops
repeated writes after sustained external contention. The complete Argo mode
matrix still needs to be rerun before claiming broad Argo compatibility.
