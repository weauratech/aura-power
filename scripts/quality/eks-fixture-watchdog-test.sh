#!/usr/bin/env bash
set -Eeuo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
watchdog="${repo_root}/scripts/quality/eks-fixture-watchdog.sh"
# shellcheck disable=SC1091
source "${repo_root}/scripts/quality/process-guard.sh"
test_root="$(mktemp -d)"
trap 'rm -rf "$test_root"' EXIT

fake_bin="${test_root}/bin"
mkdir -p "$fake_bin"
cat >"${fake_bin}/kubectl" <<'FAKE_KUBECTL'
#!/usr/bin/env bash
set -euo pipefail
state_dir="${WATCHDOG_TEST_STATE:?}"
printf '%s\n' "$*" >>"${state_dir}/commands.log"
[[ "${1:-}" != --request-timeout=* ]] || shift

has_arg() {
  local expected="$1" arg
  shift
  for arg in "$@"; do [[ "$arg" == "$expected" ]] && return 0; done
  return 1
}

emit_object() {
  local prefix="$1"
  jq -n \
    --arg run "$(cat "${state_dir}/${prefix}.run")" \
    --arg nonce "$(cat "${state_dir}/${prefix}.nonce")" \
    --arg uid "$(cat "${state_dir}/${prefix}.uid")" \
    '{metadata:{labels:{"aura-power-quality/run":$run,"aura-power-quality/nonce":$nonce},uid:$uid}}'
}

case "${1:-} ${2:-}" in
  "get namespace") prefix=namespace ;;
  "get deployment") prefix=workload ;;
  "get powerpolicy") prefix=policy ;;
  "patch deployment")
    [[ -f "${state_dir}/workload.exists" ]] || exit 1
    patch="${!#}"
    [[ "$(jq -er '.[0].value' <<<"$patch")" == "$(cat "${state_dir}/workload.uid")" ]] || exit 1
    printf '2' >"${state_dir}/workload.replicas"
    exit 0
    ;;
  "delete --raw")
    body="$(cat)"
    uid="$(jq -er '.preconditions.uid' <<<"$body")"
    path="$3"
    case "$path" in
      /api/v1/namespaces/*) prefix=namespace ;;
      /apis/apps/v1/namespaces/*/deployments/*) prefix=workload ;;
      /apis/power.aura.sh/v1alpha1/namespaces/*/powerpolicies/*) prefix=policy ;;
      *) echo "unexpected raw delete path: $path" >&2; exit 97 ;;
    esac
    if [[ -f "${state_dir}/${prefix}.delete-error" ]]; then
      echo 'simulated delete transport failure' >&2
      exit 1
    fi
    if [[ -f "${state_dir}/${prefix}.replace-on-delete" ]]; then
      cat "${state_dir}/${prefix}.replacement-uid" >"${state_dir}/${prefix}.uid"
      rm -f "${state_dir}/${prefix}.replace-on-delete"
    fi
    actual_uid="$(cat "${state_dir}/${prefix}.uid")"
    printf 'raw-delete %s precondition=%s actual=%s\n' "$path" "$uid" "$actual_uid" >>"${state_dir}/commands.log"
    [[ "$uid" == "$actual_uid" ]] || { echo 'Conflict: UID precondition failed' >&2; exit 1; }
    if [[ -f "${state_dir}/raw-delete-delay" ]]; then sleep 0.5; fi
    rm -f "${state_dir}/${prefix}.exists"
    if [[ -f "${state_dir}/${prefix}.error-after-delete" ]]; then
      : >"${state_dir}/${prefix}.post-delete-error"
    fi
    printf '{}\n'
    exit 0
    ;;
  *) echo "unexpected fake kubectl invocation: $*" >&2; exit 97 ;;
 esac

if [[ -f "${state_dir}/${prefix}.get-error" || -f "${state_dir}/${prefix}.post-delete-error" ]]; then
  echo 'simulated API unavailable' >&2
  exit 1
fi
if [[ -f "${state_dir}/${prefix}.hang-get" ]]; then
  sleep 30
fi
if [[ ! -f "${state_dir}/${prefix}.exists" ]]; then
  has_arg "--ignore-not-found" "$@" && exit 0
  exit 1
fi
if has_arg "jsonpath={.spec.replicas}" "$@"; then
  cat "${state_dir}/workload.replicas"
elif has_arg "json" "$@"; then
  emit_object "$prefix"
fi
FAKE_KUBECTL
chmod +x "${fake_bin}/kubectl"

prepare_case() {
  local case_name="$1" namespace="$2" workload="$3" policy="$4"
  CASE_DIR="${test_root}/${case_name}"
  mkdir -p "$CASE_DIR"
  : >"${CASE_DIR}/commands.log"
  for prefix in namespace workload policy; do
    printf 'quality-run' >"${CASE_DIR}/${prefix}.run"
    printf '0123456789abcdef0123456789abcdef' >"${CASE_DIR}/${prefix}.nonce"
    printf '%s-uid' "$prefix" >"${CASE_DIR}/${prefix}.uid"
  done
  [[ "$namespace" != true ]] || : >"${CASE_DIR}/namespace.exists"
  if [[ "$workload" == true ]]; then
    : >"${CASE_DIR}/workload.exists"
    printf '0' >"${CASE_DIR}/workload.replicas"
  fi
  [[ "$policy" != true ]] || : >"${CASE_DIR}/policy.exists"
  KUBECONFIG_PATH="${CASE_DIR}/kubeconfig"
  RECOVERY_DIR_PATH="${CASE_DIR}/recovery"
  : >"$KUBECONFIG_PATH"
  chmod 600 "$KUBECONFIG_PATH"
  mkdir "$RECOVERY_DIR_PATH"
  chmod 700 "$RECOVERY_DIR_PATH"
}

write_state() {
  local namespace_uid="$1" workload_uid="$2" policy_uid="$3"
  jq -n \
    --arg runID quality-run \
    --arg recoveryNonce 0123456789abcdef0123456789abcdef \
    --arg fixtureNamespace quality-namespace \
    --arg namespaceUID "$namespace_uid" \
    --arg workloadName restore-two \
    --arg workloadUID "$workload_uid" \
    --arg policyName quality-policy \
    --arg policyUID "$policy_uid" \
    '{runID:$runID,recoveryNonce:$recoveryNonce,fixtureNamespace:$fixtureNamespace,namespaceUID:$namespaceUID,workloadName:$workloadName,workloadUID:$workloadUID,policyName:$policyName,policyUID:$policyUID}' \
    >"${RECOVERY_DIR_PATH}/state.json"
  chmod 600 "${RECOVERY_DIR_PATH}/state.json"
}

invoke_watchdog() {
  local mode="$1" stderr_path="$2"
  PATH="${fake_bin}:$PATH" \
    WATCHDOG_TEST_STATE="$CASE_DIR" \
    KUBECONFIG="$KUBECONFIG_PATH" \
    AURA_POWER_RUNTIME_KUBECONFIG="$KUBECONFIG_PATH" \
    EXPECTED_CLUSTER=eks-aura-prd AWS_REGION=us-east-2 \
    RUN_ID=quality-run RECOVERY_NONCE=0123456789abcdef0123456789abcdef \
    FIXTURE_NAMESPACE=quality-namespace WORKLOAD_NAME=restore-two POLICY_NAME=quality-policy \
    RECOVERY_DIR="$RECOVERY_DIR_PATH" \
    PARENT_PID="${INVOKE_PARENT_PID:-99999999}" PARENT_IDENTITY="${INVOKE_PARENT_IDENTITY:-missing-process}" \
    HARD_DEADLINE_EPOCH="${INVOKE_DEADLINE_EPOCH:-0}" WATCHDOG_COMMAND_TIMEOUT_SECONDS="${INVOKE_COMMAND_TIMEOUT_SECONDS:-5}" \
    "$watchdog" "$mode" >/dev/null 2>"$stderr_path"
}

assert_success_cleanup() {
  local case_name="$1"
  [[ ! -f "${CASE_DIR}/namespace.exists" && ! -f "${CASE_DIR}/workload.exists" && ! -f "${CASE_DIR}/policy.exists" ]] || {
    echo "${case_name}: owned fixture resources remain" >&2; return 1;
  }
  [[ ! -f "$KUBECONFIG_PATH" && ! -f "${RECOVERY_DIR_PATH}/state.json" && -d "${RECOVERY_DIR_PATH}/supervisor-ready" && -d "${RECOVERY_DIR_PATH}/cleanup-complete" ]] || {
    echo "${case_name}: successful recovery artifacts are inconsistent" >&2; return 1;
  }
}

prepare_case interrupted-before-first-mutation false false false
write_state "" "" ""
invoke_watchdog watch "${CASE_DIR}/stderr.log"
assert_success_cleanup interrupted-before-first-mutation
if grep -Fq 'raw-delete' "${CASE_DIR}/commands.log"; then echo 'pre-mutation recovery attempted a delete' >&2; exit 1; fi

prepare_case interrupted-after-namespace true false false
write_state "" "" ""
invoke_watchdog watch "${CASE_DIR}/stderr.log"
assert_success_cleanup interrupted-after-namespace
grep -Fq 'raw-delete /api/v1/namespaces/quality-namespace precondition=namespace-uid' "${CASE_DIR}/commands.log"

prepare_case interrupted-after-workload-before-state true true false
write_state namespace-uid "" ""
invoke_watchdog watch "${CASE_DIR}/stderr.log"
assert_success_cleanup interrupted-after-workload-before-state
grep -Fq 'raw-delete /apis/apps/v1/namespaces/quality-namespace/deployments/restore-two precondition=workload-uid' "${CASE_DIR}/commands.log"

prepare_case interrupted-after-policy-state true true true
write_state namespace-uid workload-uid policy-uid
invoke_watchdog watch "${CASE_DIR}/stderr.log"
assert_success_cleanup interrupted-after-policy-state
for precondition in policy-uid workload-uid namespace-uid; do
  grep -Fq "precondition=${precondition}" "${CASE_DIR}/commands.log"
done

prepare_case abandoned-mutation-marker-after-parent-death true true false
write_state namespace-uid workload-uid ""
mkdir "${RECOVERY_DIR_PATH}/mutation-in-progress"
invoke_watchdog watch "${CASE_DIR}/stderr.log"
assert_success_cleanup abandoned-mutation-marker-after-parent-death

prepare_case namespace-replacement-race true false false
write_state namespace-uid "" ""
: >"${CASE_DIR}/namespace.replace-on-delete"
printf 'replacement-uid' >"${CASE_DIR}/namespace.replacement-uid"
replacement_status=0
invoke_watchdog cleanup "${CASE_DIR}/stderr.log" || replacement_status=$?
[[ "$replacement_status" -ne 0 && -f "${CASE_DIR}/namespace.exists" && -f "$KUBECONFIG_PATH" && -f "${RECOVERY_DIR_PATH}/state.json" ]] || {
  echo 'replacement race did not fail closed with recovery artifacts retained' >&2; exit 1;
}
grep -Fq 'precondition=namespace-uid actual=replacement-uid' "${CASE_DIR}/commands.log"

prepare_case invalid-state false false false
printf '{invalid' >"${RECOVERY_DIR_PATH}/state.json"
chmod 600 "${RECOVERY_DIR_PATH}/state.json"
invalid_status=0
invoke_watchdog cleanup "${CASE_DIR}/stderr.log" || invalid_status=$?
[[ "$invalid_status" -ne 0 && -f "$KUBECONFIG_PATH" && -f "${RECOVERY_DIR_PATH}/state.json" ]] || {
  echo 'invalid state did not fail closed' >&2; exit 1;
}

prepare_case concurrent-deadline-and-trap true true true
write_state namespace-uid workload-uid policy-uid
: >"${CASE_DIR}/raw-delete-delay"
INVOKE_PARENT_PID="$$"
INVOKE_PARENT_IDENTITY="$(process_identity "$$")"
INVOKE_DEADLINE_EPOCH="$(( $(date +%s) + 1 ))"
invoke_watchdog watch "${CASE_DIR}/watch.stderr" &
watch_pid=$!
ready_attempts=0
while [[ ! -d "${RECOVERY_DIR_PATH}/supervisor-ready" ]]; do
  ready_attempts=$((ready_attempts + 1))
  (( ready_attempts < 40 )) || { echo 'supervisor did not become ready' >&2; exit 1; }
  sleep 0.05
done
sleep 0.8
mkdir "${RECOVERY_DIR_PATH}/cleanup-requested" 2>/dev/null || true
watch_status=0
wait "$watch_pid" || watch_status=$?
[[ "$watch_status" -eq 0 ]] || {
  echo "concurrent deadline/trap recovery failed: watch=${watch_status}" >&2; exit 1;
}
assert_success_cleanup concurrent-deadline-and-trap
[[ "$(grep -c '^raw-delete ' "${CASE_DIR}/commands.log")" -eq 3 ]] || {
  echo 'concurrent recovery issued duplicate deletes' >&2; exit 1;
}

unset INVOKE_PARENT_PID INVOKE_PARENT_IDENTITY INVOKE_DEADLINE_EPOCH

prepare_case signal-trap-releases-mutation-marker true true false
write_state namespace-uid workload-uid ""
INVOKE_PARENT_PID="$$"
INVOKE_PARENT_IDENTITY="$(process_identity "$$")"
INVOKE_DEADLINE_EPOCH="$(( $(date +%s) + 30 ))"
invoke_watchdog watch "${CASE_DIR}/watch.stderr" &
watch_pid=$!
ready_attempts=0
while [[ ! -d "${RECOVERY_DIR_PATH}/supervisor-ready" ]]; do
  ready_attempts=$((ready_attempts + 1)); (( ready_attempts < 40 )) || exit 1; sleep 0.05
done
mkdir "${RECOVERY_DIR_PATH}/mutation-in-progress"
# This is the cleanup trap protocol: relinquish the mutation marker first,
# then request the already-ready supervisor.
rmdir "${RECOVERY_DIR_PATH}/mutation-in-progress"
mkdir "${RECOVERY_DIR_PATH}/cleanup-requested"
watch_status=0
wait "$watch_pid" || watch_status=$?
[[ "$watch_status" -eq 0 ]] || { echo "signal trap recovery failed: ${watch_status}" >&2; exit 1; }
assert_success_cleanup signal-trap-releases-mutation-marker
unset INVOKE_PARENT_PID INVOKE_PARENT_IDENTITY INVOKE_DEADLINE_EPOCH

prepare_case reused-parent-pid-identity true false false
write_state namespace-uid "" ""
INVOKE_PARENT_PID="$$"
INVOKE_PARENT_IDENTITY="identity-from-an-earlier-process"
INVOKE_DEADLINE_EPOCH="$(( $(date +%s) + 30 ))"
invoke_watchdog watch "${CASE_DIR}/stderr.log"
assert_success_cleanup reused-parent-pid-identity
unset INVOKE_PARENT_PID INVOKE_PARENT_IDENTITY INVOKE_DEADLINE_EPOCH

prepare_case api-unavailable-before-delete true false false
write_state namespace-uid "" ""
: >"${CASE_DIR}/namespace.get-error"
api_status=0
invoke_watchdog cleanup "${CASE_DIR}/stderr.log" || api_status=$?
[[ "$api_status" -ne 0 && -f "${CASE_DIR}/namespace.exists" && -f "$KUBECONFIG_PATH" && -f "${RECOVERY_DIR_PATH}/state.json" ]] || { echo 'API failure before delete did not fail closed' >&2; exit 1; }

prepare_case api-unavailable-during-delete true false false
write_state namespace-uid "" ""
: >"${CASE_DIR}/namespace.delete-error"
api_status=0
invoke_watchdog cleanup "${CASE_DIR}/stderr.log" || api_status=$?
[[ "$api_status" -ne 0 && -f "${CASE_DIR}/namespace.exists" && -f "$KUBECONFIG_PATH" && -f "${RECOVERY_DIR_PATH}/state.json" ]] || { echo 'API failure during delete did not fail closed' >&2; exit 1; }

prepare_case api-unavailable-after-delete true false false
write_state namespace-uid "" ""
: >"${CASE_DIR}/namespace.error-after-delete"
api_status=0
invoke_watchdog cleanup "${CASE_DIR}/stderr.log" || api_status=$?
[[ "$api_status" -ne 0 && ! -f "${CASE_DIR}/namespace.exists" && -f "$KUBECONFIG_PATH" && -f "${RECOVERY_DIR_PATH}/state.json" ]] || { echo 'inconclusive post-delete verification discarded recovery artifacts' >&2; exit 1; }

prepare_case api-timeout-before-delete true false false
write_state namespace-uid "" ""
: >"${CASE_DIR}/namespace.hang-get"
INVOKE_COMMAND_TIMEOUT_SECONDS=1
started="$(date +%s)"
api_status=0
invoke_watchdog cleanup "${CASE_DIR}/stderr.log" || api_status=$?
elapsed=$(( $(date +%s) - started ))
[[ "$api_status" -ne 0 && "$elapsed" -lt 8 && -f "${CASE_DIR}/namespace.exists" && -f "$KUBECONFIG_PATH" && -f "${RECOVERY_DIR_PATH}/state.json" ]] || { echo "API timeout did not fail closed promptly: status=${api_status} elapsed=${elapsed}" >&2; exit 1; }
unset INVOKE_COMMAND_TIMEOUT_SECONDS

echo "eks_fixture_watchdog_contract=passed pre_mutation=true nonce_capture=true preconditions=true replacement_race=true deadline_trap=true signal_trap=true abandoned_marker=true parent_identity=true api_fail_closed=true external_timeout=true invalid_state=true"
