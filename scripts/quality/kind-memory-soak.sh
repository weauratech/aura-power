#!/usr/bin/env bash
set -Eeuo pipefail

: "${KUBECONFIG:?set KUBECONFIG to the disposable Kind kubeconfig}"
[[ "$(kubectl config current-context)" == "kind-aura-power-quality" ]] || { echo "refusing mutation: this runner is restricted to kind-aura-power-quality" >&2; exit 2; }

RUN_ID="${RUN_ID:-m$(date -u +%H%M%S)}"
NAMESPACE_COUNT="${NAMESPACE_COUNT:-20}"
WORKLOAD_COUNT="${WORKLOAD_COUNT:-1000}"
SOAK_SECONDS="${SOAK_SECONDS:-180}"
SAMPLE_SECONDS="${SAMPLE_SECONDS:-15}"
TIMEOUT_SECONDS="${TIMEOUT_SECONDS:-300}"
CONTROL_NAMESPACE="aura-system"
DEPLOYMENT="aura-power-controller"
LEASE="aura-power-controller-leader.power.aura.sh"
PREFIX="ap-memory-${RUN_ID}"
ARTIFACT_DIR="${ARTIFACT_DIR:-artifacts/quality/memory-${RUN_ID}}"
MANIFEST="$(mktemp -t aura-power-memory.XXXXXX.yaml)"
SAMPLES="${ARTIFACT_DIR}/samples.csv"
BASELINES="${ARTIFACT_DIR}/pod-baselines.tsv"
mkdir -p "$ARTIFACT_DIR"

for value in "$NAMESPACE_COUNT" "$WORKLOAD_COUNT" "$SOAK_SECONDS" "$SAMPLE_SECONDS" "$TIMEOUT_SECONDS"; do
  [[ "$value" =~ ^[1-9][0-9]*$ ]] || { echo "all numeric parameters must be positive integers" >&2; exit 2; }
done
(( WORKLOAD_COUNT >= NAMESPACE_COUNT )) || { echo "WORKLOAD_COUNT must be >= NAMESPACE_COUNT" >&2; exit 2; }
(( WORKLOAD_COUNT <= 5000 && NAMESPACE_COUNT <= 100 && SOAK_SECONDS >= 60 && SOAK_SECONDS <= 900 && SAMPLE_SECONDS <= SOAK_SECONDS / 3 )) || { echo "requested soak exceeds the fixture safety bounds" >&2; exit 2; }

original_replicas="$(kubectl get deployment "$DEPLOYMENT" -n "$CONTROL_NAMESPACE" -o jsonpath='{.spec.replicas}')"
original_pprof="$(kubectl get deployment "$DEPLOYMENT" -n "$CONTROL_NAMESPACE" -o json | jq -r '.spec.template.spec.containers[] | select(.name=="controller") | .env[]? | select(.name=="PPROF_BIND_ADDRESS") | .value' | head -1)"

cleanup() {
  local original_status=$? cleanup_status=0 cleanup_deadline remaining
  trap - EXIT INT TERM HUP
	# Stop discovery before removing fixtures so an in-flight cycle cannot
	# recreate targets after the final cleanup check.
	kubectl scale deployment "$DEPLOYMENT" -n "$CONTROL_NAMESPACE" --replicas=0 >/dev/null 2>&1 || cleanup_status=1
	kubectl rollout status deployment "$DEPLOYMENT" -n "$CONTROL_NAMESPACE" --timeout="${TIMEOUT_SECONDS}s" >/dev/null 2>&1 || cleanup_status=1
  while read -r target; do
    [[ -z "$target" ]] || kubectl delete powertarget "$target" -n "$CONTROL_NAMESPACE" --ignore-not-found --wait=false >/dev/null 2>&1 || cleanup_status=1
  done < <(kubectl get powertarget -n "$CONTROL_NAMESPACE" -o json 2>/dev/null | jq -r --arg prefix "${PREFIX}-" '.items[] | select((.spec.targetRef.namespace // "") | startswith($prefix)) | .metadata.name')
  for index in $(seq 0 $((NAMESPACE_COUNT - 1))); do
    namespace="$(printf '%s-%03d' "$PREFIX" "$index")"
    run_label="$(kubectl get namespace "$namespace" -o json 2>/dev/null | jq -r '.metadata.labels["aura-power-quality/run"] // empty' || true)"
    if [[ "$run_label" == "$RUN_ID" ]]; then
      kubectl delete namespace "$namespace" --wait=false >/dev/null 2>&1 || cleanup_status=1
    elif kubectl get namespace "$namespace" >/dev/null 2>&1; then
      echo "refusing cleanup: namespace $namespace is not owned by run $RUN_ID" >&2
      cleanup_status=1
    fi
  done
  cleanup_deadline=$((SECONDS + 180))
  while (( SECONDS < cleanup_deadline )); do
    remaining=0
    for index in $(seq 0 $((NAMESPACE_COUNT - 1))); do
      namespace="$(printf '%s-%03d' "$PREFIX" "$index")"
      kubectl get namespace "$namespace" >/dev/null 2>&1 && remaining=$((remaining + 1))
    done
    (( remaining == 0 )) && break
    sleep 3
  done
  (( remaining == 0 )) || cleanup_status=1
  while read -r target; do
    [[ -z "$target" ]] || kubectl delete powertarget "$target" -n "$CONTROL_NAMESPACE" --ignore-not-found --wait=true --timeout=60s >/dev/null 2>&1 || cleanup_status=1
  done < <(kubectl get powertarget -n "$CONTROL_NAMESPACE" -o json 2>/dev/null | jq -r --arg prefix "${PREFIX}-" '.items[] | select((.spec.targetRef.namespace // "") | startswith($prefix)) | .metadata.name')
  remaining="$(kubectl get powertarget -n "$CONTROL_NAMESPACE" -o json 2>/dev/null | jq -r --arg prefix "${PREFIX}-" '[.items[] | select((.spec.targetRef.namespace // "") | startswith($prefix))] | length' || echo 1)"
  [[ "$remaining" == "0" ]] || cleanup_status=1
  if [[ -n "$original_pprof" ]]; then
    kubectl set env deployment/"$DEPLOYMENT" -n "$CONTROL_NAMESPACE" PPROF_BIND_ADDRESS="$original_pprof" >/dev/null 2>&1 || cleanup_status=1
  else
    kubectl set env deployment/"$DEPLOYMENT" -n "$CONTROL_NAMESPACE" PPROF_BIND_ADDRESS- >/dev/null 2>&1 || cleanup_status=1
  fi
  kubectl scale deployment "$DEPLOYMENT" -n "$CONTROL_NAMESPACE" --replicas="$original_replicas" >/dev/null 2>&1 || cleanup_status=1
  kubectl rollout status deployment "$DEPLOYMENT" -n "$CONTROL_NAMESPACE" --timeout="${TIMEOUT_SECONDS}s" >/dev/null 2>&1 || cleanup_status=1
  rm -f "$MANIFEST"
  [[ "$cleanup_status" -eq 0 ]] || { echo "FATAL: memory soak cleanup was not verified" >&2; exit 90; }
  exit "$original_status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM HUP

for index in $(seq 0 $((NAMESPACE_COUNT - 1))); do
  namespace="$(printf '%s-%03d' "$PREFIX" "$index")"
  kubectl get namespace "$namespace" >/dev/null 2>&1 && { echo "refusing mutation: fixture namespace already exists" >&2; exit 3; }
  cat >>"$MANIFEST" <<YAML
apiVersion: v1
kind: Namespace
metadata:
  name: ${namespace}
  labels:
    aura-power-quality/run: "${RUN_ID}"
---
YAML
done

for index in $(seq 0 $((WORKLOAD_COUNT - 1))); do
  namespace="$(printf '%s-%03d' "$PREFIX" "$((index % NAMESPACE_COUNT))")"
  name="$(printf 'workload-%05d' "$index")"
  cat >>"$MANIFEST" <<YAML
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ${name}
  namespace: ${namespace}
  labels:
    aura-power-quality/run: "${RUN_ID}"
spec:
  replicas: 0
  selector:
    matchLabels: {app: ${name}}
  template:
    metadata:
      labels: {app: ${name}}
    spec:
      containers:
        - name: pause
          image: registry.k8s.io/pause:3.10
---
YAML
done

kubectl set env deployment/"$DEPLOYMENT" -n "$CONTROL_NAMESPACE" PPROF_BIND_ADDRESS=127.0.0.1:6060 >/dev/null
kubectl rollout status deployment "$DEPLOYMENT" -n "$CONTROL_NAMESPACE" --timeout="${TIMEOUT_SECONDS}s" >/dev/null
kubectl scale deployment "$DEPLOYMENT" -n "$CONTROL_NAMESPACE" --replicas=2 >/dev/null
kubectl rollout status deployment "$DEPLOYMENT" -n "$CONTROL_NAMESPACE" --timeout="${TIMEOUT_SECONDS}s" >/dev/null

leader_pod() {
  kubectl get lease "$LEASE" -n "$CONTROL_NAMESPACE" -o jsonpath='{.spec.holderIdentity}' | sed 's/_.*//'
}

controller_pods() {
  kubectl get pods -n "$CONTROL_NAMESPACE" -l app.kubernetes.io/component=controller -o json | jq -r '.items[] | select(any(.status.containerStatuses[]?; .name == "controller" and .ready == true)) | .metadata.name' | sort
}

pod_health() {
  kubectl get pod "$1" -n "$CONTROL_NAMESPACE" -o json | jq -r '[.metadata.uid, ([.status.containerStatuses[]? | select(.name == "controller") | .restartCount] | add // 0), ([.status.containerStatuses[]? | select(.name == "controller") | select(.lastState.terminated.reason == "OOMKilled")] | length)] | @tsv'
}

assert_pod_health() {
  local pod="$1" expected_uid="$2" expected_restarts="$3" uid restarts oom
  IFS=$'\t' read -r uid restarts oom <<<"$(pod_health "$pod")"
  [[ "$uid" == "$expected_uid" ]] || { echo "FAIL: controller pod $pod identity changed during soak" >&2; exit 17; }
  [[ "$restarts" == "$expected_restarts" ]] || { echo "FAIL: controller pod $pod restarted during soak ($expected_restarts -> $restarts)" >&2; exit 17; }
  [[ "$oom" == "0" ]] || { echo "FAIL: controller pod $pod was OOMKilled during soak" >&2; exit 17; }
}

initial_pods=()
while read -r pod; do
  [[ -z "$pod" ]] || initial_pods+=("$pod")
done < <(controller_pods)
[[ "${#initial_pods[@]}" -eq 2 ]] || { echo "FAIL: expected two ready controller pods, found ${#initial_pods[@]}" >&2; exit 11; }
: >"$BASELINES"
for pod in "${initial_pods[@]}"; do
  printf '%s\t%s\n' "$pod" "$(pod_health "$pod")" >>"$BASELINES"
done

# Capture both controller identities and restart/OOM counters before applying
# any high-cardinality fixture. This makes failures during creation/discovery
# observable rather than accepting them as the post-load baseline.
kubectl apply -f "$MANIFEST" >/dev/null
deadline=$((SECONDS + TIMEOUT_SECONDS)); discovered=0
while (( SECONDS < deadline )); do
  for pod in "${initial_pods[@]}"; do
    baseline="$(awk -F '\t' -v pod="$pod" '$1 == pod {print $0}' "$BASELINES")"
    assert_pod_health "$pod" "$(cut -f2 <<<"$baseline")" "$(cut -f3 <<<"$baseline")"
  done
  discovered="$(kubectl get powertarget -n "$CONTROL_NAMESPACE" -o json | jq -r --arg prefix "${PREFIX}-" '[.items[] | select((.spec.targetRef.namespace // "") | startswith($prefix))] | length')"
  (( discovered >= WORKLOAD_COUNT )) && break
  sleep 10
done
(( discovered >= WORKLOAD_COUNT )) || { echo "FAIL: discovered $discovered of $WORKLOAD_COUNT workloads" >&2; exit 10; }

echo 'epoch,pod,heap_alloc_bytes,heap_inuse_bytes,rss_bytes,memory_limit_bytes' >"$SAMPLES"
sample_pod() {
  local pod="$1" label="$2" metrics_file forward_pid heap inuse rss limit
  metrics_file="${ARTIFACT_DIR}/metrics-${label}.txt"
  kubectl port-forward -n "$CONTROL_NAMESPACE" pod/"$pod" 18082:8080 16062:6060 >"${ARTIFACT_DIR}/port-forward-${label}.log" 2>&1 &
  forward_pid=$!
  for _ in $(seq 1 30); do curl -fsS http://127.0.0.1:18082/metrics >"$metrics_file" 2>/dev/null && break; sleep 1; done
  curl -fsS http://127.0.0.1:18082/metrics >"$metrics_file"
  heap="$(awk '$1=="aura_power_process_heap_alloc_bytes" {print int($2)}' "$metrics_file")"
  inuse="$(awk '$1=="aura_power_process_heap_inuse_bytes" {print int($2)}' "$metrics_file")"
  rss="$(awk '$1=="process_resident_memory_bytes" {print int($2)}' "$metrics_file")"
  limit="$(awk '$1=="aura_power_process_memory_limit_bytes" {print int($2)}' "$metrics_file")"
  [[ -n "$heap" && -n "$inuse" && -n "$rss" && -n "$limit" ]] || { kill "$forward_pid" 2>/dev/null || true; wait "$forward_pid" 2>/dev/null || true; echo "FAIL: required memory metrics missing" >&2; exit 11; }
  echo "$(date +%s),$pod,$heap,$inuse,$rss,$limit" >>"$SAMPLES"
  if [[ "$label" == profile-* ]]; then
    curl -fsS 'http://127.0.0.1:16062/debug/pprof/heap' >"${ARTIFACT_DIR}/heap-${label}.pb.gz"
  fi
  kill "$forward_pid" 2>/dev/null || true
  wait "$forward_pid" 2>/dev/null || true
}

sample_all_pods() {
  local label="$1" index=0 pod baseline expected_uid expected_restarts
  while read -r pod; do
    [[ -n "$pod" ]] || continue
    sample_pod "$pod" "${label}-${index}-${pod}"
    baseline="$(awk -F '\t' -v pod="$pod" '$1 == pod {print $0}' "$BASELINES")"
    if [[ -n "$baseline" ]]; then
      expected_uid="$(cut -f2 <<<"$baseline")"
      expected_restarts="$(cut -f3 <<<"$baseline")"
      assert_pod_health "$pod" "$expected_uid" "$expected_restarts"
    else
      IFS=$'\t' read -r expected_uid expected_restarts _ <<<"$(pod_health "$pod")"
      [[ "$expected_restarts" == "0" ]] || { echo "FAIL: replacement controller pod $pod started with restarts=$expected_restarts" >&2; exit 17; }
      assert_pod_health "$pod" "$expected_uid" 0
    fi
    index=$((index + 1))
  done < <(controller_pods)
  [[ "$index" -eq 2 ]] || { echo "FAIL: sampled $index controller pods, expected 2" >&2; exit 11; }
}

leader="$(leader_pod)"
[[ -n "$leader" ]] || { echo "FAIL: leader lease has no holder" >&2; exit 12; }
sample_all_pods profile-before-failover

soak_started_epoch="$(date +%s)"
soak_deadline=$((SECONDS + SOAK_SECONDS)); sample_index=0
while (( SECONDS < soak_deadline )); do
  sample_all_pods "soak-${sample_index}"
  sample_index=$((sample_index + 1))
  sleep "$SAMPLE_SECONDS"
done

old_leader="$(leader_pod)"
old_uid="$(kubectl get pod "$old_leader" -n "$CONTROL_NAMESPACE" -o jsonpath='{.metadata.uid}')"
# Preserve restart/OOM evidence for both replicas before intentionally deleting
# the leader; otherwise its termination history disappears with the Pod.
for pod in "${initial_pods[@]}"; do
  baseline="$(awk -F '\t' -v pod="$pod" '$1 == pod {print $0}' "$BASELINES")"
  assert_pod_health "$pod" "$(cut -f2 <<<"$baseline")" "$(cut -f3 <<<"$baseline")"
done
kubectl delete pod "$old_leader" -n "$CONTROL_NAMESPACE" --wait=false >/dev/null
deadline=$((SECONDS + TIMEOUT_SECONDS)); new_leader=""
while (( SECONDS < deadline )); do
  new_leader="$(leader_pod 2>/dev/null || true)"
  if [[ -n "$new_leader" && "$new_leader" != "$old_leader" ]] && kubectl get pod "$new_leader" -n "$CONTROL_NAMESPACE" >/dev/null 2>&1; then break; fi
  sleep 5
done
[[ -n "$new_leader" && "$new_leader" != "$old_leader" ]] || { echo "FAIL: leader did not fail over from $old_leader" >&2; exit 13; }
kubectl wait -n "$CONTROL_NAMESPACE" --for=condition=Ready pod/"$new_leader" --timeout="${TIMEOUT_SECONDS}s" >/dev/null
kubectl rollout status deployment "$DEPLOYMENT" -n "$CONTROL_NAMESPACE" --timeout="${TIMEOUT_SECONDS}s" >/dev/null
sample_all_pods profile-after-failover

peak_heap="$(awk -F, 'NR>1 && $3>max {max=$3} END {print max+0}' "$SAMPLES")"
peak_rss="$(awk -F, 'NR>1 && $5>max {max=$5} END {print max+0}' "$SAMPLES")"
steady_cutoff=$((soak_started_epoch + SOAK_SECONDS / 2))
# Compute growth independently per original replica, then gate on the worst
# value. Interleaved leader/follower samples must never be compared as one pod.
growth="$(awk -F, -v cutoff="$steady_cutoff" 'NR>1 && $1>=cutoff {if (!($2 in first)) first[$2]=$3; last[$2]=$3} END {max=-1; for (p in last) {d=last[p]-first[p]; if (d>max) max=d} print (max<0 ? 0 : max)}' "$SAMPLES")"
memory_limit="$(awk -F, 'NR==2 {print $6}' "$SAMPLES")"
(( peak_heap < memory_limit )) || { echo "FAIL: heap peak $peak_heap reached GOMEMLIMIT $memory_limit" >&2; exit 14; }
(( peak_rss < 268435456 )) || { echo "FAIL: RSS peak $peak_rss reached the 256MiB pod limit" >&2; exit 15; }
(( growth < 33554432 )) || { echo "FAIL: steady-state heap grew by $growth bytes during bounded soak" >&2; exit 16; }
new_uid="$(kubectl get pod "$new_leader" -n "$CONTROL_NAMESPACE" -o jsonpath='{.metadata.uid}')"
[[ "$new_uid" != "$old_uid" ]] || { echo "FAIL: failover did not replace the leader identity" >&2; exit 18; }

cat >"${ARTIFACT_DIR}/summary.json" <<JSON
{"run":"${RUN_ID}","namespaces":${NAMESPACE_COUNT},"workloads":${WORKLOAD_COUNT},"soakSeconds":${SOAK_SECONDS},"peakHeapBytes":${peak_heap},"peakRSSBytes":${peak_rss},"heapGrowthBytes":${growth},"goMemoryLimitBytes":${memory_limit},"leaderFailover":true,"oomKilled":false}
JSON
echo "kind_memory_soak=passed workloads=$WORKLOAD_COUNT namespaces=$NAMESPACE_COUNT peak_heap=$peak_heap peak_rss=$peak_rss leader_failover=true artifacts=$ARTIFACT_DIR"
