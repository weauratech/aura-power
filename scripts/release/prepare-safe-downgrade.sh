#!/usr/bin/env bash
set -Eeuo pipefail

usage() {
  cat <<'EOF'
Prepare an Aura Power downgrade without losing powered-down workload state.

Usage:
  prepare-safe-downgrade.sh \
    --expected-context CONTEXT \
    --namespace NAMESPACE \
    --release RELEASE \
    --backup-dir DIRECTORY

The command is idempotent when rerun with the same backup directory. It:
  1. stops the release server and inventories rules with resourceVersions;
  2. saves policies, overrides, targets, and original replica counts;
  3. quarantines rules with UID/resourceVersion-conditional JSON Patches;
  4. stops the controller and proves the rule set did not change;
  5. restores snapshots after matching each live workload UID;
  6. verifies both writers remain stopped and all snapshots are restored.

It does not run Helm. Keep the backup directory until the release is upgraded
again and the saved rules have been reviewed before they are reapplied.
EOF
}

expected_context=""
control_namespace=""
release_name=""
backup_dir=""

while (($#)); do
  case "$1" in
    --expected-context) expected_context="${2:-}"; shift 2 ;;
    --namespace) control_namespace="${2:-}"; shift 2 ;;
    --release) release_name="${2:-}"; shift 2 ;;
    --backup-dir) backup_dir="${2:-}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

[[ -n "$expected_context" && -n "$control_namespace" && -n "$release_name" && -n "$backup_dir" ]] || {
  usage >&2
  exit 2
}

command -v kubectl >/dev/null || { echo "kubectl is required" >&2; exit 2; }
command -v jq >/dev/null || { echo "jq is required" >&2; exit 2; }
command -v helm >/dev/null || { echo "helm is required" >&2; exit 2; }

actual_context="$(kubectl config current-context)"
[[ "$actual_context" == "$expected_context" ]] || {
  echo "refusing mutation: current context '$actual_context' does not equal '$expected_context'" >&2
  exit 3
}

kubectl get namespace "$control_namespace" >/dev/null
release_status="$(helm status "$release_name" -n "$control_namespace" -o json)"
[[ "$(jq -r '.info.status' <<<"$release_status")" == "deployed" ]] || {
  echo "release $control_namespace/$release_name is not deployed" >&2
  exit 4
}

controller_list="$(kubectl get deployment -n "$control_namespace" \
  -l "app.kubernetes.io/instance=${release_name},app.kubernetes.io/component=controller" \
  -o json)"
controller_count="$(jq '.items | length' <<<"$controller_list")"
[[ "$controller_count" == "1" ]] || {
  echo "expected exactly one controller deployment for release $release_name, found $controller_count" >&2
  exit 5
}
controller_name="$(jq -er '.items[0].metadata.name' <<<"$controller_list")"

server_list="$(kubectl get statefulset -n "$control_namespace" \
  -l "app.kubernetes.io/instance=${release_name},app.kubernetes.io/component=server" \
  -o json)"
server_count="$(jq '.items | length' <<<"$server_list")"
[[ "$server_count" == "1" ]] || {
  echo "expected exactly one server statefulset for release $release_name, found $server_count" >&2
  exit 5
}
server_name="$(jq -er '.items[0].metadata.name' <<<"$server_list")"

mkdir -p "$backup_dir" "$backup_dir/powerpolicies" "$backup_dir/poweroverrides" "$backup_dir/powertargets"
chmod 700 "$backup_dir" "$backup_dir/powerpolicies" "$backup_dir/poweroverrides" "$backup_dir/powertargets"
metadata_file="$backup_dir/metadata.json"

if [[ -f "$metadata_file" ]]; then
  quarantine_token="$(jq -er '.quarantineToken' "$metadata_file")"
  saved_context="$(jq -er '.context' "$metadata_file")"
  saved_namespace="$(jq -er '.namespace' "$metadata_file")"
  saved_release="$(jq -er '.release' "$metadata_file")"
  [[ "$saved_context" == "$expected_context" && "$saved_namespace" == "$control_namespace" && "$saved_release" == "$release_name" ]] || {
    echo "backup directory belongs to a different context, namespace, or release" >&2
    exit 6
  }
else
  original_replicas="$(kubectl get deployment "$controller_name" -n "$control_namespace" -o jsonpath='{.spec.replicas}')"
  original_server_replicas="$(kubectl get statefulset "$server_name" -n "$control_namespace" -o jsonpath='{.spec.replicas}')"
  quarantine_token="downgrade-$(date -u +%Y%m%d%H%M%S)-$$"
  jq -n \
    --arg context "$expected_context" \
    --arg namespace "$control_namespace" \
    --arg release "$release_name" \
    --arg controller "$controller_name" \
    --arg server "$server_name" \
    --arg quarantineToken "$quarantine_token" \
    --argjson controllerReplicas "${original_replicas:-0}" \
    --argjson serverReplicas "${original_server_replicas:-0}" \
    '{context:$context, namespace:$namespace, release:$release, controller:$controller, controllerReplicas:$controllerReplicas, server:$server, serverReplicas:$serverReplicas, quarantineToken:$quarantineToken}' \
    >"$metadata_file"
  chmod 600 "$metadata_file"
fi

[[ "$(jq -er '.controller' "$metadata_file")" == "$controller_name" && "$(jq -er '.server' "$metadata_file")" == "$server_name" ]] || {
  echo "release workload identity changed since backup creation" >&2
  exit 6
}

# Stop the API writer before taking the rule inventory. The admission webhook
# remains available in the controller until every baseline rule is quarantined.
kubectl scale statefulset "$server_name" -n "$control_namespace" --replicas=0 >/dev/null
kubectl rollout status statefulset "$server_name" -n "$control_namespace" --timeout=180s >/dev/null
[[ "$(kubectl get statefulset "$server_name" -n "$control_namespace" -o jsonpath='{.spec.replicas}')" == "0" ]] || {
  echo "server did not quiesce" >&2
  exit 7
}

fail_closed() {
  kubectl scale deployment "$controller_name" -n "$control_namespace" --replicas=0 >/dev/null 2>&1 || true
  kubectl rollout status deployment "$controller_name" -n "$control_namespace" --timeout=180s >/dev/null 2>&1 || true
}

rules_inventory() {
  local policies overrides
  policies="$(kubectl get powerpolicy -n "$control_namespace" -o json)"
  overrides="$(kubectl get poweroverride -n "$control_namespace" -o json)"
  jq -nc --argjson policies "$policies" --argjson overrides "$overrides" '
    ([ $policies.items[] | {kind:"PowerPolicy",name:.metadata.name,uid:.metadata.uid,resourceVersion:.metadata.resourceVersion} ] +
     [ $overrides.items[] | {kind:"PowerOverride",name:.metadata.name,uid:.metadata.uid,resourceVersion:.metadata.resourceVersion} ]) |
    sort_by(.kind,.name)'
}

inventory_file="$backup_dir/rules-inventory.json"
if [[ ! -f "$inventory_file" ]]; then
  rules_inventory >"$inventory_file"
  chmod 600 "$inventory_file"
fi

backup_targets() {
  local resource="$1"
  local directory="$2"
  local name file
  while IFS= read -r name; do
    [[ -n "$name" ]] || continue
    file="$directory/$name.json"
    if [[ ! -f "$file" ]]; then
      kubectl get "$resource" "$name" -n "$control_namespace" -o json >"$file"
      chmod 600 "$file"
    fi
  done < <(kubectl get "$resource" -n "$control_namespace" -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}')
}

backup_rules() {
  local resource="$1"
  local kind="$2"
  local directory="$3"
  local name expected_uid expected_rv file object
  while IFS=$'\t' read -r name expected_uid expected_rv; do
    [[ -n "$name" ]] || continue
    file="$directory/$name.json"
    if [[ ! -f "$file" ]]; then
      object="$(kubectl get "$resource" "$name" -n "$control_namespace" -o json)" || { fail_closed; return 1; }
      if [[ "$(jq -r '.metadata.uid' <<<"$object")" != "$expected_uid" || "$(jq -r '.metadata.resourceVersion' <<<"$object")" != "$expected_rv" ]]; then
        echo "$resource $name changed while its backup was captured" >&2
        fail_closed
        return 1
      fi
      printf '%s\n' "$object" >"$file"
      chmod 600 "$file"
    fi
  done < <(jq -r --arg kind "$kind" '.[] | select(.kind == $kind) | [.name,.uid,.resourceVersion] | @tsv' "$inventory_file")
}

backup_rules powerpolicy PowerPolicy "$backup_dir/powerpolicies"
backup_rules poweroverride PowerOverride "$backup_dir/poweroverrides"
backup_targets powertarget "$backup_dir/powertargets"

# Deterministic test-only synchronization point for race-injection journeys.
touch "$backup_dir/inventory.ready"
if [[ -n "${AURA_POWER_DOWNGRADE_TEST_PAUSE_AFTER_INVENTORY_SECONDS:-}" ]]; then
  [[ "$AURA_POWER_DOWNGRADE_TEST_PAUSE_AFTER_INVENTORY_SECONDS" =~ ^[0-9]+$ && "$AURA_POWER_DOWNGRADE_TEST_PAUSE_AFTER_INVENTORY_SECONDS" -le 30 ]] || {
    echo "AURA_POWER_DOWNGRADE_TEST_PAUSE_AFTER_INVENTORY_SECONDS must be an integer <= 30" >&2
    fail_closed
    exit 7
  }
  sleep "$AURA_POWER_DOWNGRADE_TEST_PAUSE_AFTER_INVENTORY_SECONDS"
fi

current_inventory="$(rules_inventory)"
if ! jq -en --argjson current "$current_inventory" --slurpfile baseline "$inventory_file" '
  ($current | map({kind,name,uid})) == ($baseline[0] | map({kind,name,uid}))' >/dev/null; then
  echo "rule set changed after inventory; refusing to quarantine an unbacked or replaced resource" >&2
  fail_closed
  exit 8
fi

# Workload labels are copied into PowerTarget status. Prove the generated value
# is absent before using it as a fail-closed selector in both controller versions.
if kubectl get powertarget -n "$control_namespace" -o json | jq -e \
  --arg token "$quarantine_token" \
  '.items[] | select(.status.workloadLabels["power.aura.sh/downgrade-quarantine"] == $token)' >/dev/null; then
  echo "generated quarantine selector already exists on a managed workload" >&2
  fail_closed
  exit 8
fi

quarantine_rules() {
  local resource="$1"
  local kind="$2"
  local name expected_uid expected_rv rule_json annotation_patch patch
  while IFS=$'\t' read -r name expected_uid expected_rv; do
    [[ -n "$name" ]] || continue
    rule_json="$(kubectl get "$resource" "$name" -n "$control_namespace" -o json)" || { fail_closed; return 1; }
    [[ "$(jq -r '.metadata.uid' <<<"$rule_json")" == "$expected_uid" ]] || {
      echo "$resource $name was replaced after inventory" >&2
      fail_closed
      return 1
    }
    if jq -e --arg token "$quarantine_token" \
      '((.spec.scope.targetRefs | length) == 0) and (.spec.scope.workloadLabels["power.aura.sh/downgrade-quarantine"] == $token)' \
      <<<"$rule_json" >/dev/null; then
      continue
    fi
    [[ "$(jq -r '.metadata.resourceVersion' <<<"$rule_json")" == "$expected_rv" ]] || {
      echo "$resource $name changed after backup; refusing to overwrite resourceVersion $(jq -r '.metadata.resourceVersion' <<<"$rule_json")" >&2
      fail_closed
      return 1
    }
    if jq -e '.metadata.annotations != null' <<<"$rule_json" >/dev/null; then
      annotation_patch='[{"op":"add","path":"/metadata/annotations/power.aura.sh~1downgrade-quarantined","value":"true"}]'
    else
      annotation_patch='[{"op":"add","path":"/metadata/annotations","value":{"power.aura.sh/downgrade-quarantined":"true"}}]'
    fi
    patch="$(jq -nc \
      --arg uid "$expected_uid" \
      --arg rv "$expected_rv" \
      --arg token "$quarantine_token" \
      --argjson annotation "$annotation_patch" \
      '[{"op":"test","path":"/metadata/uid","value":$uid},
        {"op":"test","path":"/metadata/resourceVersion","value":$rv},
        {"op":"add","path":"/spec/scope/targetRefs","value":[]},
        {"op":"add","path":"/spec/scope/workloadLabels","value":{"power.aura.sh/downgrade-quarantine":$token}}] + $annotation')"
    if ! kubectl patch "$resource" "$name" -n "$control_namespace" --type=json -p "$patch" >/dev/null; then
      echo "$resource $name failed its UID/resourceVersion-conditional quarantine" >&2
      fail_closed
      return 1
    fi
  done < <(jq -r --arg kind "$kind" '.[] | select(.kind == $kind) | [.name,.uid,.resourceVersion] | @tsv' "$inventory_file")
}

quarantine_rules powerpolicy PowerPolicy
quarantine_rules poweroverride PowerOverride

# Admission is served by the controller. Quarantine and verify the rules while
# it is still available, then stop reconciliation before restoring snapshots.
kubectl scale deployment "$controller_name" -n "$control_namespace" --replicas=0 >/dev/null
kubectl rollout status deployment "$controller_name" -n "$control_namespace" --timeout=180s >/dev/null
[[ "$(kubectl get deployment "$controller_name" -n "$control_namespace" -o jsonpath='{.status.replicas}')" != "1" ]] || {
  echo "controller did not quiesce" >&2
  exit 7
}

final_inventory="$(rules_inventory)"
if ! jq -en --argjson current "$final_inventory" --slurpfile baseline "$inventory_file" '
  ($current | map({kind,name,uid})) == ($baseline[0] | map({kind,name,uid}))' >/dev/null; then
  echo "rule set changed while quarantine completed; both release writers remain stopped" >&2
  exit 8
fi

restore_target() {
  local target_json="$1"
  local target_name kind namespace name expected_uid api_resource live_json live_uid desired current patch
  target_name="$(jq -r '.metadata.name' <<<"$target_json")"
  kind="$(jq -r '.spec.targetRef.kind' <<<"$target_json")"
  namespace="$(jq -r '.spec.targetRef.namespace' <<<"$target_json")"
  name="$(jq -r '.spec.targetRef.name' <<<"$target_json")"
  expected_uid="$(jq -r '.spec.targetRef.uid // ""' <<<"$target_json")"

  [[ -n "$expected_uid" ]] || {
    echo "cannot safely restore target $target_name: targetRef.uid is absent" >&2
    return 1
  }

  case "$kind" in
    Deployment) api_resource="deployment.apps" ;;
    StatefulSet) api_resource="statefulset.apps" ;;
    CronJob) api_resource="cronjob.batch" ;;
    *) echo "cannot safely restore target $target_name: unsupported kind '$kind'" >&2; return 1 ;;
  esac

  live_json="$(kubectl get "$api_resource" "$name" -n "$namespace" -o json)" || {
    echo "cannot safely restore target $target_name: live workload is absent" >&2
    return 1
  }
  live_uid="$(jq -r '.metadata.uid' <<<"$live_json")"
  [[ "$live_uid" == "$expected_uid" ]] || {
    echo "cannot safely restore target $target_name: UID changed ($expected_uid != $live_uid)" >&2
    return 1
  }

  if [[ "$kind" == "CronJob" ]]; then
    desired="$(jq -r '.status.snapshot.suspended | if . == null then "missing" else tostring end' <<<"$target_json")"
    [[ "$desired" != "missing" ]] || { echo "snapshot for $target_name has no suspended value" >&2; return 1; }
    current="$(jq -r '.spec.suspend // false | tostring' <<<"$live_json")"
    if [[ "$current" != "$desired" ]]; then
      [[ "$current" == "true" ]] || { echo "refusing to overwrite concurrent CronJob state for $target_name" >&2; return 1; }
      patch="$(jq -nc --arg uid "$expected_uid" --argjson desired "$desired" '[{"op":"test","path":"/metadata/uid","value":$uid},{"op":"test","path":"/spec/suspend","value":true},{"op":"replace","path":"/spec/suspend","value":$desired}]')"
      kubectl patch "$api_resource" "$name" -n "$namespace" --type=json -p "$patch" >/dev/null
    fi
  else
    desired="$(jq -r '.status.snapshot.replicaCount // "missing"' <<<"$target_json")"
    [[ "$desired" =~ ^[0-9]+$ ]] || { echo "snapshot for $target_name has no valid replica count" >&2; return 1; }
    current="$(jq -r '.spec.replicas // 1' <<<"$live_json")"
    if [[ "$current" != "$desired" ]]; then
      [[ "$current" == "0" ]] || { echo "refusing to overwrite concurrent scale for $target_name ($current replicas)" >&2; return 1; }
      patch="$(jq -nc --arg uid "$expected_uid" --argjson desired "$desired" '[{"op":"test","path":"/metadata/uid","value":$uid},{"op":"test","path":"/spec/replicas","value":0},{"op":"replace","path":"/spec/replicas","value":$desired}]')"
      kubectl patch "$api_resource" "$name" -n "$namespace" --type=json -p "$patch" >/dev/null
    fi
  fi

  live_json="$(kubectl get "$api_resource" "$name" -n "$namespace" -o json)"
  [[ "$(jq -r '.metadata.uid' <<<"$live_json")" == "$expected_uid" ]] || return 1
  if [[ "$kind" == "CronJob" ]]; then
    [[ "$(jq -r '.spec.suspend // false | tostring' <<<"$live_json")" == "$desired" ]] || return 1
  else
    [[ "$(jq -r '.spec.replicas // 1' <<<"$live_json")" == "$desired" ]] || return 1
  fi
  echo "restored target=$target_name kind=$kind workload=$namespace/$name uid=$expected_uid"
}

restore_failures=0
while IFS= read -r target_json; do
  [[ -n "$target_json" ]] || continue
  restore_target "$target_json" || restore_failures=$((restore_failures + 1))
done < <(kubectl get powertarget -n "$control_namespace" -o json | jq -c '.items[] | select(.status.snapshot.available == true)')
[[ "$restore_failures" == "0" ]] || {
  echo "$restore_failures snapshot restoration(s) failed; controller remains stopped" >&2
  exit 9
}

for resource in powerpolicy poweroverride; do
  if kubectl get "$resource" -n "$control_namespace" -o json | jq -e \
    --arg token "$quarantine_token" \
    '.items[] | select(.spec.scope.workloadLabels["power.aura.sh/downgrade-quarantine"] != $token or (.spec.scope.targetRefs | length) != 0)' >/dev/null; then
    echo "not every $resource is quarantined" >&2
    exit 10
  fi
done

[[ "$(kubectl get deployment "$controller_name" -n "$control_namespace" -o jsonpath='{.spec.replicas}')" == "0" ]] || {
  echo "controller replica count changed during preparation" >&2
  exit 11
}
[[ "$(kubectl get statefulset "$server_name" -n "$control_namespace" -o jsonpath='{.spec.replicas}')" == "0" ]] || {
  echo "server replica count changed during preparation" >&2
  exit 11
}

echo "safe_downgrade_prepared=true context=$expected_context namespace=$control_namespace release=$release_name server=$server_name controller=$controller_name writers_stopped=true rules_quarantined=true snapshots_restored=true backup_dir=$backup_dir"
