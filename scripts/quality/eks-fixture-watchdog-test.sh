#!/usr/bin/env bash
set -Eeuo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
watchdog="${repo_root}/scripts/quality/eks-fixture-watchdog.sh"
test_root="$(mktemp -d)"
trap 'rm -rf "$test_root"' EXIT

fake_bin="${test_root}/bin"
mkdir -p "$fake_bin"
cat >"${fake_bin}/kubectl" <<'FAKE_KUBECTL'
#!/usr/bin/env bash
set -euo pipefail

state_dir="${WATCHDOG_TEST_STATE:?}"
log="${state_dir}/commands.log"
printf '%s\n' "$*" >>"$log"

has_arg() {
  local expected="$1" arg
  shift
  for arg in "$@"; do
    [[ "$arg" == "$expected" ]] && return 0
  done
  return 1
}

case "${1:-} ${2:-}" in
  "get powerpolicy")
    exit 1
    ;;
  "get namespace")
    [[ -f "${state_dir}/namespace.exists" ]] || exit 1
    if has_arg "jsonpath={.metadata.uid}" "$@"; then
      cat "${state_dir}/namespace.uid"
    elif has_arg "json" "$@"; then
      jq -n --arg run "$(cat "${state_dir}/namespace.run")" --arg uid "$(cat "${state_dir}/namespace.uid")" \
        '{metadata:{labels:{"aura-power-quality/run":$run},uid:$uid}}'
    fi
    ;;
  "get deployment")
    [[ -f "${state_dir}/workload.exists" ]] || exit 1
    if has_arg "jsonpath={.metadata.uid}" "$@"; then
      cat "${state_dir}/workload.uid"
    elif has_arg "jsonpath={.spec.replicas}" "$@"; then
      cat "${state_dir}/workload.replicas"
    elif has_arg "json" "$@"; then
      jq -n --arg run "$(cat "${state_dir}/workload.run")" --arg uid "$(cat "${state_dir}/workload.uid")" \
        '{metadata:{labels:{"aura-power-quality/run":$run},uid:$uid}}'
    fi
    ;;
  "scale deployment")
    [[ -f "${state_dir}/workload.exists" ]] || exit 1
    printf '2' >"${state_dir}/workload.replicas"
    ;;
  "delete namespace")
    rm -f "${state_dir}/namespace.exists" "${state_dir}/workload.exists"
    ;;
  *)
    echo "unexpected fake kubectl invocation: $*" >&2
    exit 97
    ;;
esac
FAKE_KUBECTL
chmod +x "${fake_bin}/kubectl"

run_case() {
  local case_name="$1" state_workload_uid="$2" actual_workload_uid="$3" expected_status="$4"
  local state_dir="${test_root}/${case_name}"
  mkdir -p "$state_dir"
  : >"${state_dir}/commands.log"
  : >"${state_dir}/namespace.exists"
  printf 'quality-run' >"${state_dir}/namespace.run"
  printf 'namespace-uid' >"${state_dir}/namespace.uid"
  if [[ -n "$actual_workload_uid" ]]; then
    : >"${state_dir}/workload.exists"
    printf 'quality-run' >"${state_dir}/workload.run"
    printf '%s' "$actual_workload_uid" >"${state_dir}/workload.uid"
    printf '0' >"${state_dir}/workload.replicas"
  fi

  local kubeconfig="${state_dir}/kubeconfig" recovery_state="${state_dir}/recovery.json"
  : >"$kubeconfig"
  chmod 600 "$kubeconfig"
  jq -n \
    --arg runID quality-run \
    --arg fixtureNamespace quality-namespace \
    --arg namespaceUID namespace-uid \
    --arg workloadName restore-two \
    --arg workloadUID "$state_workload_uid" \
    --arg policyName quality-policy \
    '{runID:$runID,fixtureNamespace:$fixtureNamespace,namespaceUID:$namespaceUID,workloadName:$workloadName,workloadUID:$workloadUID,policyName:$policyName}' \
    >"$recovery_state"
  chmod 600 "$recovery_state"

  local status=0
  PATH="${fake_bin}:$PATH" \
    WATCHDOG_TEST_STATE="$state_dir" \
    KUBECONFIG="$kubeconfig" \
    AURA_POWER_RUNTIME_KUBECONFIG="$kubeconfig" \
    EXPECTED_CLUSTER=eks-aura-prd \
    AWS_REGION=us-east-2 \
    RUN_ID=quality-run \
    FIXTURE_NAMESPACE=quality-namespace \
    NAMESPACE_UID=namespace-uid \
    WORKLOAD_NAME=restore-two \
    WORKLOAD_UID="$state_workload_uid" \
    POLICY_NAME=quality-policy \
    RECOVERY_STATE="$recovery_state" \
    PARENT_PID=99999999 \
    HARD_DEADLINE_EPOCH=0 \
    "$watchdog" watch >/dev/null 2>"${state_dir}/stderr.log" || status=$?

  [[ "$status" -eq "$expected_status" ]] || {
    echo "${case_name}: expected status ${expected_status}, got ${status}" >&2
    cat "${state_dir}/stderr.log" >&2
    return 1
  }
  if [[ "$expected_status" -eq 0 ]]; then
    [[ ! -f "${state_dir}/namespace.exists" ]] || { echo "${case_name}: namespace was not deleted" >&2; return 1; }
    [[ ! -f "$kubeconfig" && ! -f "$recovery_state" ]] || { echo "${case_name}: private recovery artifacts were not removed" >&2; return 1; }
    grep -Fq 'delete namespace quality-namespace' "${state_dir}/commands.log"
    if [[ -n "$actual_workload_uid" ]]; then
      grep -Fq 'scale deployment restore-two -n quality-namespace --replicas=2' "${state_dir}/commands.log"
    else
      if grep -Fq 'scale deployment' "${state_dir}/commands.log"; then
        echo "${case_name}: namespace-only recovery unexpectedly scaled a workload" >&2
        return 1
      fi
    fi
  else
    [[ -f "${state_dir}/namespace.exists" ]] || { echo "${case_name}: mismatched namespace was deleted" >&2; return 1; }
    [[ -f "$kubeconfig" && -f "$recovery_state" ]] || { echo "${case_name}: failed recovery discarded private recovery artifacts" >&2; return 1; }
    if grep -Fq 'delete namespace quality-namespace' "${state_dir}/commands.log"; then
      echo "${case_name}: mismatched workload allowed namespace deletion" >&2
      return 1
    fi
  fi
}

# Deterministic equivalents of termination immediately after each identity
# boundary in the parent journey.
run_case interrupted-after-namespace "" "" 0
run_case interrupted-after-workload-before-state "" workload-uid 0
run_case interrupted-after-workload-state workload-uid workload-uid 0
run_case workload-uid-mismatch workload-uid replacement-uid 1

echo "eks_fixture_watchdog_contract=passed partial_namespace=true late_workload_uid=true exact_uid=true fail_closed=true"
