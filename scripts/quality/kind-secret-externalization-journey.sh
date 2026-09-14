#!/usr/bin/env bash
set -Eeuo pipefail

# Proves the three-revision transition from a chart-managed auth Secret to an
# existingSecret without deleting/recreating the object or retaining credential
# material in Helm history.

: "${KUBECONFIG:?set KUBECONFIG to the disposable Kind kubeconfig}"

context="$(kubectl config current-context)"
[[ "$context" == kind-* ]] || { echo "refusing mutation outside Kind: $context" >&2; exit 2; }

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
release="aura-power-secret-transfer"
namespace="aura-power-secret-transfer"
workdir="$(mktemp -d)"
initial_values="$workdir/initial.json"
retained_values="$workdir/retained.json"
sanitized_values="$workdir/sanitized.json"
umask 077

hash_stdin() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum | awk '{print $1}'
  else
    shasum -a 256 | awk '{print $1}'
  fi
}

cleanup() {
  local original_status=$?
  trap - EXIT INT TERM HUP
  helm uninstall "$release" -n "$namespace" >/dev/null 2>&1 || true
  kubectl delete namespace "$namespace" --wait=true --timeout=120s >/dev/null 2>&1 || true
  find "$workdir" -type f -exec sh -c ': >"$1"' _ {} \;
  rm -rf "$workdir"
  exit "$original_status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM HUP

if kubectl get namespace "$namespace" >/dev/null 2>&1; then
  echo "refusing mutation: test namespace already exists" >&2
  exit 3
fi
kubectl create namespace "$namespace" >/dev/null

jq -n '{
  controller:{enabled:false},
  webhook:{enabled:false},
  rbac:{create:false},
  server:{
    replicas:1,
    persistence:{enabled:false},
    auth:{
      initialAdmin:{username:"admin",password:"Kind transfer passphrase 2026!"},
      jwtSecret:"kind-transfer-jwt-signing-key-2026-only",
      existingSecret:"",
      keepManagedSecret:false
    }
  }
}' >"$initial_values"

helm install "$release" "$repo_root/charts/aura-power" -n "$namespace" \
  -f "$initial_values" >/dev/null

secret="$(kubectl get secret -n "$namespace" -l "app.kubernetes.io/instance=$release,app.kubernetes.io/component=server" -o json | jq -r 'if (.items|length)==1 then .items[0].metadata.name else empty end')"
[[ -n "$secret" ]] || { echo "managed auth Secret was not created exactly once" >&2; exit 10; }
uid_before="$(kubectl get secret "$secret" -n "$namespace" -o jsonpath='{.metadata.uid}')"
data_hash_before="$(kubectl get secret "$secret" -n "$namespace" -o json | jq -cS '.data' | hash_stdin)"

helm get values "$release" -n "$namespace" --all -o json |
  jq '.server.auth.keepManagedSecret = true' >"$retained_values"
helm upgrade "$release" "$repo_root/charts/aura-power" -n "$namespace" \
  --reset-values -f "$retained_values" --history-max 1 >/dev/null

[[ "$(kubectl get secret "$secret" -n "$namespace" -o jsonpath='{.metadata.annotations.helm\.sh/resource-policy}')" == "keep" ]] || {
  echo "preparatory revision did not retain the managed Secret" >&2
  exit 11
}
retained_manifest="$(helm get manifest "$release" -n "$namespace")"
grep -q 'helm.sh/resource-policy: keep' <<<"$retained_manifest" || {
  echo "preparatory Helm manifest did not record the retention policy" >&2
  exit 12
}

jq --arg secret "$secret" '
  .server.auth.existingSecret = $secret |
  .server.auth.keepManagedSecret = false |
  .server.auth.jwtSecret = "" |
  .server.auth.initialAdmin.password = null
' "$retained_values" >"$sanitized_values"

for _ in 1 2; do
  helm upgrade "$release" "$repo_root/charts/aura-power" -n "$namespace" \
    --reset-values -f "$sanitized_values" --history-max 1 >/dev/null
  [[ "$(kubectl get secret "$secret" -n "$namespace" -o jsonpath='{.metadata.uid}')" == "$uid_before" ]] || {
    echo "auth Secret UID changed during externalization" >&2
    exit 13
  }
  [[ "$(kubectl get secret "$secret" -n "$namespace" -o json | jq -cS '.data' | hash_stdin)" == "$data_hash_before" ]] || {
    echo "auth Secret data changed during externalization" >&2
    exit 14
  }
done

history="$(helm history "$release" -n "$namespace" -o json)"
[[ "$(jq 'length' <<<"$history")" == "2" ]] || { echo "unexpected retained Helm revision count" >&2; exit 15; }
while IFS= read -r revision; do
  values="$(helm get values "$release" -n "$namespace" --revision "$revision" --all -o json)"
  [[ "$(jq -r '.server.auth.existingSecret' <<<"$values")" == "$secret" ]] || { echo "revision $revision is not externalized" >&2; exit 16; }
  [[ "$(jq -r '.server.auth.jwtSecret' <<<"$values")" == "" ]] || { echo "revision $revision retains a JWT value" >&2; exit 17; }
  [[ "$(jq -r '.server.auth.initialAdmin.password // empty' <<<"$values")" == "" ]] || { echo "revision $revision retains an admin password" >&2; exit 18; }
  revision_manifest="$(helm get manifest "$release" -n "$namespace" --revision "$revision")"
  if grep -q '# Source: aura-power/templates/server-secret.yaml' <<<"$revision_manifest"; then
    echo "revision $revision still renders the auth Secret" >&2
    exit 19
  fi
done < <(jq -r '.[].revision' <<<"$history")

echo "managed_secret_externalization=passed retained_revisions=2 uid_preserved=true"
