#!/usr/bin/env bash
set -Eeuo pipefail

: "${KUBECONFIG:?set KUBECONFIG to the disposable Kind kubeconfig}"
readonly KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-aura-power-quality}"
readonly KIND_BIN="${KIND_BIN:-kind}"
[[ "$(kubectl config current-context)" == "kind-${KIND_CLUSTER_NAME}" ]] || { echo "refusing mutation: context must match kind-${KIND_CLUSTER_NAME}" >&2; exit 2; }
command -v docker >/dev/null || { echo "docker is required" >&2; exit 2; }
command -v "$KIND_BIN" >/dev/null || { echo "kind binary not found: $KIND_BIN" >&2; exit 2; }

readonly ARGO_CD_VERSION="${ARGO_CD_VERSION:-v2.14.20}"
readonly ARGO_INSTALL_SHA256="747db8bb6d1591b49bb6266412e6dcaccf3c7bd3bd81ef7d63b0ab5bfe6af951"
readonly ARGO_NAMESPACE="argocd"
readonly RUN_ID="${RUN_ID:-gitops$(date -u +%Y%m%d%H%M%S)}"
readonly FIXTURE_NAMESPACE="aura-power-gitops-${RUN_ID}"
readonly GIT_IMAGE="aura-power-git-fixture:${RUN_ID}"
readonly REPO_URL="git://git-fixture.${FIXTURE_NAMESPACE}.svc.cluster.local/repo.git"
readonly TIMEOUT_SECONDS="${TIMEOUT_SECONDS:-300}"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
ARGO_MANIFEST="$(mktemp -t aura-power-argocd.XXXXXX.yaml)"
BUILD_CONTEXT="$(mktemp -d -t aura-power-git-fixture.XXXXXX)"
readonly REPO_ROOT ARGO_MANIFEST BUILD_CONTEXT

[[ "$TIMEOUT_SECONDS" =~ ^[0-9]+$ && "$TIMEOUT_SECONDS" -le 600 ]] || { echo "TIMEOUT_SECONDS must be an integer <= 600" >&2; exit 2; }
[[ "$ARGO_CD_VERSION" == "v2.14.20" ]] || { echo "ARGO_CD_VERSION is integrity-pinned to v2.14.20" >&2; exit 2; }

namespace_uid=""
cleanup() {
  local original_status=$? cleanup_status=0 actual_uid actual_run
  trap - EXIT INT TERM HUP
  set +e
  kubectl delete powerpolicy -n aura-system -l "aura-power-quality/run=${RUN_ID}" --ignore-not-found --wait=true --timeout=90s >/dev/null 2>&1 || true
  for resource in applications.argoproj.io applicationsets.argoproj.io; do
    kubectl get "$resource" -n "$ARGO_NAMESPACE" -l "aura-power-quality/run=${RUN_ID}" -o name 2>/dev/null | while read -r item; do
      [[ -z "$item" ]] && continue
      kubectl patch "$item" -n "$ARGO_NAMESPACE" --type=merge -p '{"metadata":{"finalizers":[]}}' >/dev/null 2>&1 || true
      kubectl delete "$item" -n "$ARGO_NAMESPACE" --wait=false >/dev/null 2>&1 || true
    done
  done
  if [[ -n "$namespace_uid" ]] && kubectl get namespace "$FIXTURE_NAMESPACE" >/dev/null 2>&1; then
    actual_uid="$(kubectl get namespace "$FIXTURE_NAMESPACE" -o jsonpath='{.metadata.uid}')"
    actual_run="$(kubectl get namespace "$FIXTURE_NAMESPACE" -o json | jq -r '.metadata.labels["aura-power-quality/run"] // empty')"
    if [[ "$actual_uid" == "$namespace_uid" && "$actual_run" == "$RUN_ID" ]]; then
      kubectl delete namespace "$FIXTURE_NAMESPACE" --wait=true --timeout=180s >/dev/null 2>&1 || true
    else
      echo "refusing cleanup: fixture namespace ownership or UID mismatch" >&2
      cleanup_status=1
    fi
  fi
  if [[ -s "$ARGO_MANIFEST" ]]; then
    kubectl delete -f "$ARGO_MANIFEST" --ignore-not-found --wait=false >/dev/null 2>&1 || true
  fi
  kubectl delete namespace "$ARGO_NAMESPACE" --ignore-not-found --wait=true --timeout=180s >/dev/null 2>&1 || true
  kubectl delete powertarget -n aura-system -l "power.aura.sh/target-namespace=${FIXTURE_NAMESPACE}" --ignore-not-found --wait=true --timeout=90s >/dev/null 2>&1 || true
  rm -rf "$BUILD_CONTEXT" "$ARGO_MANIFEST"
  if kubectl get namespace "$FIXTURE_NAMESPACE" >/dev/null 2>&1 ||
    kubectl get namespace "$ARGO_NAMESPACE" >/dev/null 2>&1 ||
    kubectl get powerpolicy -n aura-system -l "aura-power-quality/run=${RUN_ID}" -o name 2>/dev/null | grep -q . ||
    kubectl get powertarget -n aura-system -l "power.aura.sh/target-namespace=${FIXTURE_NAMESPACE}" -o name 2>/dev/null | grep -q . ||
    kubectl get crd applications.argoproj.io >/dev/null 2>&1 ||
    kubectl get clusterrole -l app.kubernetes.io/part-of=argocd -o name 2>/dev/null | grep -q .; then
    cleanup_status=1
  fi
  if [[ "$cleanup_status" -ne 0 ]]; then
    echo "FATAL: Argo CD fixture cleanup was not verified" >&2
    exit 90
  fi
  exit "$original_status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM HUP

wait_for() {
  local description="$1"; shift
  local deadline=$((SECONDS + TIMEOUT_SECONDS))
  until "$@"; do
    (( SECONDS < deadline )) || { echo "FAIL: timed out waiting for ${description}" >&2; return 1; }
    sleep 3
  done
}

app_is_synced() {
  [[ "$(kubectl get application "$1" -n "$ARGO_NAMESPACE" -o jsonpath='{.status.sync.status}' 2>/dev/null || true)" == "Synced" ]]
}

app_exists() {
  kubectl get application "$1" -n "$ARGO_NAMESPACE" >/dev/null 2>&1
}

sync_app() {
  kubectl patch application "$1" -n "$ARGO_NAMESPACE" --type=merge -p '{"operation":{"initiatedBy":{"username":"quality-campaign"},"sync":{"revision":"HEAD","prune":true,"syncOptions":["CreateNamespace=true"]}}}' >/dev/null
  wait_for "Application $1 sync" app_is_synced "$1"
}

replicas_are() {
  [[ "$(kubectl get "$1" "$2" -n "$FIXTURE_NAMESPACE" -o jsonpath='{.spec.replicas}' 2>/dev/null || true)" == "$3" ]]
}

cron_suspend_is() {
  [[ "$(kubectl get cronjob "$1" -n "$FIXTURE_NAMESPACE" -o jsonpath='{.spec.suspend}' 2>/dev/null || true)" == "$2" ]]
}

label_is_v2() {
  [[ "$(kubectl get "$1" "$2" -n "$FIXTURE_NAMESPACE" -o jsonpath='{.metadata.labels.aura-power-quality/revision}' 2>/dev/null || true)" == "v2" ]]
}

target_name() {
  kubectl get powertarget -n aura-system -l "power.aura.sh/target-namespace=${FIXTURE_NAMESPACE},power.aura.sh/target-name=$2,power.aura.sh/target-kind=$1" -o jsonpath='{.items[0].metadata.name}'
}

job_count() {
  kubectl get jobs -n "$FIXTURE_NAMESPACE" -o json | jq --arg owner "$1" '[.items[] | select(any(.metadata.ownerReferences[]?; .kind == "CronJob" and .name == $owner))] | length'
}

jobs_at_least() {
  [[ "$(job_count "$1")" -ge "$2" ]]
}

jobs_greater_than() {
  [[ "$(job_count "$1")" -gt "$2" ]]
}

job_identity_exists() {
  [[ "$(kubectl get job "$2" -n "$FIXTURE_NAMESPACE" -o jsonpath='{.metadata.uid}' 2>/dev/null || true)" == "$3" ]]
}

missed_job_exists() {
  local owner="$1" cutoff="$2" previous_uids="$3"
  kubectl get jobs -n "$FIXTURE_NAMESPACE" -o json | jq -e \
    --arg owner "$owner" \
    --arg cutoff "$cutoff" \
    --argjson previous_uids "$previous_uids" '
      any(.items[];
        any(.metadata.ownerReferences[]?; .kind == "CronJob" and .name == $owner) and
        (.metadata.uid as $uid | ($previous_uids | index($uid) | not)) and
        ((.metadata.annotations["batch.kubernetes.io/cronjob-scheduled-timestamp"] // "") < $cutoff) and
        ((.metadata.annotations["batch.kubernetes.io/cronjob-scheduled-timestamp"] // "") != ""))' >/dev/null
}

target_action_phase_is() {
  [[ "$(kubectl get powertarget "$1" -n aura-system -o jsonpath='{.status.action.phase}' 2>/dev/null || true)" == "$2" ]]
}

successful_audit_count() {
  kubectl get powerauditevent -n aura-system \
    -l "power.aura.sh/target-namespace=${FIXTURE_NAMESPACE},power.aura.sh/target-name=$1,power.aura.sh/target-kind=$2,power.aura.sh/action=workload.powered_down" \
    -o json | jq '[.items[] | select(.spec.result == "success")] | length'
}

single_successful_audit_exists() {
  [[ "$(successful_audit_count "$1" "$2")" == "1" ]]
}

create_policy() {
  local policy="$1" state="$2"; shift 2
  {
    cat <<YAML
apiVersion: power.aura.sh/v1alpha1
kind: PowerPolicy
metadata:
  name: ${policy}
  namespace: aura-system
  labels:
    aura-power-quality/run: "${RUN_ID}"
spec:
  scope:
    targetRefs:
YAML
    local pair kind name uid api_version
    for pair in "$@"; do
      IFS=: read -r kind name <<<"$pair"
      api_version="apps/v1"; [[ "$kind" == "CronJob" ]] && api_version="batch/v1"
      uid="$(kubectl get "$(tr '[:upper:]' '[:lower:]' <<<"$kind")" "$name" -n "$FIXTURE_NAMESPACE" -o jsonpath='{.metadata.uid}')"
      cat <<YAML
      - apiVersion: ${api_version}
        kind: ${kind}
        namespace: ${FIXTURE_NAMESPACE}
        name: ${name}
        uid: ${uid}
YAML
    done
    cat <<YAML
  schedule:
    desiredState: "${state}"
    windows: []
  priority: 1000
YAML
  } | kubectl create -f - >/dev/null
}

set_policy_state() {
  kubectl patch powerpolicy "$1" -n aura-system --type=merge -p "{\"spec\":{\"schedule\":{\"desiredState\":\"$2\",\"windows\":[]}}}" >/dev/null
}

git_revision_v2() {
  local application="$1"
  kubectl patch application "$application" -n "$ARGO_NAMESPACE" --type=merge -p '{"spec":{"source":{"targetRevision":"revision-v2"}}}' >/dev/null
  kubectl annotate application "$application" -n "$ARGO_NAMESPACE" argocd.argoproj.io/refresh=hard --overwrite >/dev/null
}

appset_git_revision_v2() {
  kubectl patch applicationset "quality-${RUN_ID}" -n "$ARGO_NAMESPACE" --type=merge -p '{"spec":{"template":{"spec":{"source":{"targetRevision":"revision-v2"}}}}}' >/dev/null
  wait_for "ApplicationSet propagates Git revision" bash -c "[[ \"\$(kubectl get application 'appset-${RUN_ID}' -n '$ARGO_NAMESPACE' -o jsonpath='{.spec.source.targetRevision}')\" == revision-v2 ]]"
  kubectl annotate application "appset-${RUN_ID}" -n "$ARGO_NAMESPACE" argocd.argoproj.io/refresh=hard --overwrite >/dev/null
}

[[ ! -e "$ARGO_MANIFEST" || ! -s "$ARGO_MANIFEST" ]] || { echo "temporary manifest unexpectedly populated" >&2; exit 2; }
if kubectl get namespace "$ARGO_NAMESPACE" >/dev/null 2>&1 || kubectl get namespace "$FIXTURE_NAMESPACE" >/dev/null 2>&1; then
  echo "refusing mutation: Argo CD or fixture namespace already exists" >&2
  exit 4
fi
if kubectl get crd -o name | grep -q 'argoproj.io' ||
  kubectl get clusterrole,clusterrolebinding -o name | grep -q '/argocd-' ||
  kubectl get validatingwebhookconfiguration,mutatingwebhookconfiguration -o name | grep -q '/argocd-'; then
  echo "refusing mutation: cluster-scoped Argo CD resources already exist" >&2
  exit 4
fi
kubectl get crd powertargets.power.aura.sh -o json | jq -e '
  .spec.versions[] | select(.name == "v1alpha1") |
  .schema.openAPIV3Schema.properties.status.properties.action.properties as $action |
  ($action | has("auditEventID")) and
  ($action | has("auditPhase")) and
  ($action | has("auditAction")) and
  ($action.phase.enum | index("Contended") != null)' >/dev/null || {
  echo "refusing mutation: candidate PowerTarget CRD with durable audit and Contended phase is required" >&2
  exit 4
}

curl -fsSL "https://raw.githubusercontent.com/argoproj/argo-cd/${ARGO_CD_VERSION}/manifests/install.yaml" -o "$ARGO_MANIFEST"
[[ "$(shasum -a 256 "$ARGO_MANIFEST" | awk '{print $1}')" == "$ARGO_INSTALL_SHA256" ]] || { echo "FAIL: Argo CD manifest checksum mismatch" >&2; exit 5; }

mkdir -p "$BUILD_CONTEXT/repo"
cp -R "$REPO_ROOT/tests/acceptance/fixtures/argocd/e2e-repository/." "$BUILD_CONTEXT/repo/"
find "$BUILD_CONTEXT/repo" -type f -name '*.yaml' -exec sed -i.bak "s/__NAMESPACE__/${FIXTURE_NAMESPACE}/g" {} \;
find "$BUILD_CONTEXT/repo" -name '*.bak' -delete
cat >"$BUILD_CONTEXT/Dockerfile" <<'DOCKERFILE'
FROM alpine/git:v2.47.2@sha256:062a01ad7a0eb17cff382bc5e26086b4d710e56dfdfdf001109a49b6d9bd378c
USER root
COPY repo /seed
RUN apk add --no-cache git-daemon=2.47.3-r0 && \
    git init -b main /work && \
    git -C /work config user.name quality-campaign && \
    git -C /work config user.email quality@invalid.example && \
    cp -R /seed/. /work/ && git -C /work add . && git -C /work commit -m baseline && \
    git -C /work checkout -b revision-v2 && \
    sed -i 's#aura-power-quality/revision: v1#aura-power-quality/revision: v2#g' /work/*/*.yaml && \
    git -C /work add . && git -C /work commit -m 'unrelated metadata update' && \
    mkdir -p /srv && git clone --bare /work /srv/repo.git
ENTRYPOINT ["git", "daemon", "--reuseaddr", "--base-path=/srv", "--export-all", "--verbose", "/srv/repo.git"]
DOCKERFILE
docker build -q -t "$GIT_IMAGE" "$BUILD_CONTEXT" >/dev/null
"$KIND_BIN" load docker-image --name "$KIND_CLUSTER_NAME" "$GIT_IMAGE" >/dev/null

namespace_json="$(kubectl create -o json -f - <<YAML
apiVersion: v1
kind: Namespace
metadata:
  name: ${FIXTURE_NAMESPACE}
  labels:
    aura-power-quality/run: "${RUN_ID}"
  annotations:
    aura.sh/power-eligible: "true"
YAML
)"
namespace_uid="$(jq -er .metadata.uid <<<"$namespace_json")"
kubectl create -n "$FIXTURE_NAMESPACE" -f - <<YAML
apiVersion: apps/v1
kind: Deployment
metadata:
  name: git-fixture
  labels: {aura-power-quality/run: "${RUN_ID}"}
  annotations: {aura.sh/power-exempt: "true"}
spec:
  replicas: 1
  selector: {matchLabels: {app: git-fixture}}
  template:
    metadata: {labels: {app: git-fixture}}
    spec:
      containers:
        - name: git
          image: ${GIT_IMAGE}
          imagePullPolicy: Never
          ports: [{name: git, containerPort: 9418}]
---
apiVersion: v1
kind: Service
metadata:
  name: git-fixture
spec:
  selector: {app: git-fixture}
  ports: [{name: git, port: 9418, targetPort: git}]
YAML
kubectl rollout status deployment/git-fixture -n "$FIXTURE_NAMESPACE" --timeout=120s

kubectl create namespace "$ARGO_NAMESPACE"
kubectl label namespace "$ARGO_NAMESPACE" aura-power-quality/run="$RUN_ID" >/dev/null
kubectl apply -n "$ARGO_NAMESPACE" -f "$ARGO_MANIFEST" >/dev/null
kubectl patch configmap argocd-cm -n "$ARGO_NAMESPACE" --type=merge -p '{"data":{"application.resourceTrackingMethod":"annotation"}}' >/dev/null
for component in argocd-applicationset-controller argocd-repo-server argocd-server; do
  kubectl rollout status deployment/$component -n "$ARGO_NAMESPACE" --timeout=300s
done
kubectl rollout status statefulset/argocd-application-controller -n "$ARGO_NAMESPACE" --timeout=300s

kubectl apply -f - <<YAML
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: manual-${RUN_ID}
  namespace: ${ARGO_NAMESPACE}
  labels: {aura-power-quality/run: "${RUN_ID}"}
spec:
  project: default
  source: {repoURL: "${REPO_URL}", targetRevision: main, path: manual}
  destination: {server: "https://kubernetes.default.svc", namespace: "${FIXTURE_NAMESPACE}"}
  syncPolicy: {syncOptions: ["CreateNamespace=true"]}
---
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: contended-${RUN_ID}
  namespace: ${ARGO_NAMESPACE}
  labels: {aura-power-quality/run: "${RUN_ID}"}
spec:
  project: default
  source: {repoURL: "${REPO_URL}", targetRevision: main, path: contended}
  destination: {server: "https://kubernetes.default.svc", namespace: "${FIXTURE_NAMESPACE}"}
  syncPolicy:
    automated: {prune: true, selfHeal: true}
    syncOptions: ["CreateNamespace=true"]
---
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: ignore-${RUN_ID}
  namespace: ${ARGO_NAMESPACE}
  labels: {aura-power-quality/run: "${RUN_ID}"}
spec:
  project: default
  source: {repoURL: "${REPO_URL}", targetRevision: main, path: ignore-only}
  destination: {server: "https://kubernetes.default.svc", namespace: "${FIXTURE_NAMESPACE}"}
  ignoreDifferences:
    - {group: apps, kind: Deployment, jsonPointers: [/spec/replicas]}
  syncPolicy:
    automated: {prune: true, selfHeal: true}
    syncOptions: ["CreateNamespace=true"]
---
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: respected-${RUN_ID}
  namespace: ${ARGO_NAMESPACE}
  labels: {aura-power-quality/run: "${RUN_ID}"}
spec:
  project: default
  source: {repoURL: "${REPO_URL}", targetRevision: main, path: respected}
  destination: {server: "https://kubernetes.default.svc", namespace: "${FIXTURE_NAMESPACE}"}
  ignoreDifferences:
    - {group: apps, kind: Deployment, jsonPointers: [/spec/replicas]}
    - {group: apps, kind: StatefulSet, jsonPointers: [/spec/replicas]}
    - {group: batch, kind: CronJob, jsonPointers: [/spec/suspend]}
  syncPolicy:
    automated: {prune: true, selfHeal: true}
    syncOptions: ["CreateNamespace=true", "RespectIgnoreDifferences=true"]
---
apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: quality-${RUN_ID}
  namespace: ${ARGO_NAMESPACE}
  labels: {aura-power-quality/run: "${RUN_ID}"}
spec:
  generators:
    - list: {elements: [{name: appset}]}
  template:
    metadata:
      name: '{{name}}-${RUN_ID}'
      labels: {aura-power-quality/run: "${RUN_ID}"}
    spec:
      project: default
      source: {repoURL: "${REPO_URL}", targetRevision: main, path: '{{name}}'}
      destination: {server: "https://kubernetes.default.svc", namespace: "${FIXTURE_NAMESPACE}"}
      ignoreDifferences:
        - {group: apps, kind: Deployment, jsonPointers: [/spec/replicas]}
        - {group: apps, kind: StatefulSet, jsonPointers: [/spec/replicas]}
        - {group: batch, kind: CronJob, jsonPointers: [/spec/suspend]}
      syncPolicy:
        automated: {prune: true, selfHeal: true}
        syncOptions: ["CreateNamespace=true", "RespectIgnoreDifferences=true"]
YAML

sync_app "manual-${RUN_ID}"
for app in "contended-${RUN_ID}" "ignore-${RUN_ID}" "respected-${RUN_ID}"; do wait_for "Application $app initial sync" app_is_synced "$app"; done
wait_for "ApplicationSet generated Application" app_exists "appset-${RUN_ID}"
wait_for "ApplicationSet Application initial sync" app_is_synced "appset-${RUN_ID}"

# Manual sync restores every undelegated Aura-owned field and demonstrates the
# unsafe mode for all supported workload kinds.
create_policy "manual-${RUN_ID}" off "Deployment:manual-deployment" "StatefulSet:manual-statefulset" "CronJob:manual-cronjob"
wait_for "manual Application Deployment power-down" replicas_are deployment manual-deployment 0
wait_for "manual Application StatefulSet power-down" replicas_are statefulset manual-statefulset 0
wait_for "manual Application CronJob suspension" cron_suspend_is manual-cronjob true
sync_app "manual-${RUN_ID}"
wait_for "manual sync restoring Deployment replicas" replicas_are deployment manual-deployment 2
wait_for "manual sync restoring StatefulSet replicas" replicas_are statefulset manual-statefulset 1
wait_for "manual sync restoring CronJob suspension" cron_suspend_is manual-cronjob false
set_policy_state "manual-${RUN_ID}" on

# Real automated self-heal, with no field delegation, must cause one Aura
# transition and then settle in Contended. Aura must not replay the mutation.
create_policy "contended-${RUN_ID}" off "Deployment:contended-deployment"
contended_target="$(target_name Deployment contended-deployment)"
wait_for "candidate power-down audit before Argo self-heal" single_successful_audit_exists contended-deployment Deployment
contended_attempted_at="$(kubectl get powertarget "$contended_target" -n aura-system -o jsonpath='{.status.action.attemptedAt}')"
contended_audit_id="$(kubectl get powertarget "$contended_target" -n aura-system -o jsonpath='{.status.action.auditEventID}')"
[[ -n "$contended_attempted_at" && -n "$contended_audit_id" ]] || { echo "FAIL: self-heal action checkpoint is incomplete" >&2; exit 18; }
wait_for "Argo automated self-heal restoring replicas" replicas_are deployment contended-deployment 2
wait_for "candidate stable contention phase" target_action_phase_is "$contended_target" Contended
sleep 45
[[ "$(kubectl get powertarget "$contended_target" -n aura-system -o jsonpath='{.status.action.phase}')" == "Contended" ]] || { echo "FAIL: contention phase was not stable" >&2; exit 18; }
[[ "$(kubectl get powertarget "$contended_target" -n aura-system -o jsonpath='{.status.action.attemptedAt}')" == "$contended_attempted_at" ]] || { echo "FAIL: candidate replayed the contended action" >&2; exit 18; }
[[ "$(kubectl get powertarget "$contended_target" -n aura-system -o jsonpath='{.status.action.auditEventID}')" == "$contended_audit_id" ]] || { echo "FAIL: candidate replaced the original audit identity" >&2; exit 18; }
[[ "$(successful_audit_count contended-deployment Deployment)" == "1" ]] || { echo "FAIL: self-heal produced duplicate successful mutation audits" >&2; exit 18; }
replicas_are deployment contended-deployment 2 || { echo "FAIL: Aura and Argo still oscillate after contention" >&2; exit 18; }

# ignoreDifferences alone hides drift, but a later sync still reapplies replicas.
create_policy "ignore-${RUN_ID}" off "Deployment:ignore-only-deployment"
wait_for "ignore-only workload power-down" replicas_are deployment ignore-only-deployment 0
git_revision_v2 "ignore-${RUN_ID}"
wait_for "ignore-only unrelated Git update" label_is_v2 deployment ignore-only-deployment
wait_for "ignore-only sync reapplying replicas" replicas_are deployment ignore-only-deployment 2
set_policy_state "ignore-${RUN_ID}" on

# Correct delegation keeps Aura fields while Git remains authoritative elsewhere.
wait_for "CronJob creates an active Job" jobs_at_least respected-cronjob 1
jobs_before="$(job_count respected-cronjob)"
existing_job_name="$(kubectl get jobs -n "$FIXTURE_NAMESPACE" -o json | jq -er --arg owner respected-cronjob '[.items[] | select(any(.metadata.ownerReferences[]?; .kind == "CronJob" and .name == $owner))][0].metadata.name')"
existing_job_uid="$(kubectl get job "$existing_job_name" -n "$FIXTURE_NAMESPACE" -o jsonpath='{.metadata.uid}')"
create_policy "respected-${RUN_ID}" off "Deployment:respected-deployment" "StatefulSet:respected-statefulset" "CronJob:respected-cronjob"
wait_for "delegated Deployment power-down" replicas_are deployment respected-deployment 0
wait_for "delegated StatefulSet power-down" replicas_are statefulset respected-statefulset 0
wait_for "delegated CronJob suspension" cron_suspend_is respected-cronjob true
[[ "$(job_count respected-cronjob)" -ge "$jobs_before" ]] || { echo "FAIL: existing CronJob Job was removed" >&2; exit 20; }
job_identity_exists respected-cronjob "$existing_job_name" "$existing_job_uid" || { echo "FAIL: existing CronJob Job name/UID changed during suspension" >&2; exit 20; }
jobs_at_suspend="$(job_count respected-cronjob)"
jobs_at_suspend_uids="$(kubectl get jobs -n "$FIXTURE_NAMESPACE" -o json | jq --arg owner respected-cronjob '[.items[] | select(any(.metadata.ownerReferences[]?; .kind == "CronJob" and .name == $owner)) | .metadata.uid]')"
cron_target="$(target_name CronJob respected-cronjob)"
wait_for "active Job observation" bash -c "[[ \"\$(kubectl get powertarget '$cron_target' -n aura-system -o jsonpath='{.status.observedState.activeJobs}')\" -ge 1 ]]"

# Cross a known scheduled timestamp while suspended; no new Job may start.
suspended_epoch="$(date -u +%s)"
next_schedule_epoch="$(( (suspended_epoch / 60 + 1) * 60 ))"
sleep_seconds="$(( next_schedule_epoch + 10 - $(date -u +%s) ))"
(( sleep_seconds > 0 )) && sleep "$sleep_seconds"
jobs_suspended="$(job_count respected-cronjob)"
[[ "$jobs_suspended" == "$jobs_at_suspend" ]] || { echo "FAIL: CronJob created a Job while suspended" >&2; exit 21; }
job_identity_exists respected-cronjob "$existing_job_name" "$existing_job_uid" || { echo "FAIL: existing CronJob Job identity was not preserved while suspended" >&2; exit 21; }
git_revision_v2 "respected-${RUN_ID}"
for resource in "deployment:respected-deployment" "statefulset:respected-statefulset" "cronjob:respected-cronjob"; do
  IFS=: read -r kind name <<<"$resource"
  wait_for "$kind unrelated Git update" label_is_v2 "$kind" "$name"
done
[[ "$(kubectl get deployment respected-deployment -n "$FIXTURE_NAMESPACE" -o jsonpath='{.spec.replicas}')/$(kubectl get statefulset respected-statefulset -n "$FIXTURE_NAMESPACE" -o jsonpath='{.spec.replicas}')/$(kubectl get cronjob respected-cronjob -n "$FIXTURE_NAMESPACE" -o jsonpath='{.spec.suspend}')" == "0/0/true" ]] || { echo "FAIL: RespectIgnoreDifferences did not preserve Aura-owned fields" >&2; exit 22; }
resume_cutoff="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
set_policy_state "respected-${RUN_ID}" on
wait_for "delegated Deployment restore" replicas_are deployment respected-deployment 2
wait_for "delegated StatefulSet restore" replicas_are statefulset respected-statefulset 1
wait_for "delegated CronJob restore" cron_suspend_is respected-cronjob false
[[ "$(kubectl get cronjob respected-cronjob -n "$FIXTURE_NAMESPACE" -o jsonpath='{.spec.schedule}')" == "* * * * *" ]] || { echo "FAIL: CronJob schedule changed" >&2; exit 23; }
[[ "$(kubectl get cronjob respected-cronjob -n "$FIXTURE_NAMESPACE" -o jsonpath='{.spec.startingDeadlineSeconds}')" == "300" ]] || { echo "FAIL: startingDeadlineSeconds changed" >&2; exit 23; }
wait_for "Job for a timestamp missed before resume" missed_job_exists respected-cronjob "$resume_cutoff" "$jobs_at_suspend_uids"
job_identity_exists respected-cronjob "$existing_job_name" "$existing_job_uid" || { echo "FAIL: pre-existing CronJob Job name/UID changed after resume" >&2; exit 23; }

# ApplicationSet-generated Applications inherit the same ownership contract for
# every supported workload kind.
create_policy "appset-${RUN_ID}" off "Deployment:appset-deployment" "StatefulSet:appset-statefulset" "CronJob:appset-cronjob"
wait_for "ApplicationSet Deployment power-down" replicas_are deployment appset-deployment 0
wait_for "ApplicationSet StatefulSet power-down" replicas_are statefulset appset-statefulset 0
wait_for "ApplicationSet CronJob suspension" cron_suspend_is appset-cronjob true
appset_git_revision_v2
for resource in "deployment:appset-deployment" "statefulset:appset-statefulset" "cronjob:appset-cronjob"; do
  IFS=: read -r kind name <<<"$resource"
  wait_for "ApplicationSet $kind unrelated Git update" label_is_v2 "$kind" "$name"
done
[[ "$(kubectl get deployment appset-deployment -n "$FIXTURE_NAMESPACE" -o jsonpath='{.spec.replicas}')/$(kubectl get statefulset appset-statefulset -n "$FIXTURE_NAMESPACE" -o jsonpath='{.spec.replicas}')/$(kubectl get cronjob appset-cronjob -n "$FIXTURE_NAMESPACE" -o jsonpath='{.spec.suspend}')" == "0/0/true" ]] || { echo "FAIL: generated Application overwrote a delegated field" >&2; exit 24; }
set_policy_state "appset-${RUN_ID}" on
wait_for "ApplicationSet Deployment restore" replicas_are deployment appset-deployment 1
wait_for "ApplicationSet StatefulSet restore" replicas_are statefulset appset-statefulset 1
wait_for "ApplicationSet CronJob restore" cron_suspend_is appset-cronjob false

echo "kind_argocd_journey=passed argocd=${ARGO_CD_VERSION} annotation_tracking=true manual_sync_all_kinds=true self_heal_contended_single_action=true ignore_only=true respect_ignore_differences=true unrelated_git_update=true applicationset_all_kinds=true deployment=true statefulset=true cronjob=true existing_job_name_uid=true missed_scheduled_timestamp=true"
