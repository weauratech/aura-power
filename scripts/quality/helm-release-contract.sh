#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo_root"

for crd in powerpolicies poweroverrides; do
  chart="charts/aura-power/crds/${crd}.yaml"
  config="config/crd/bases/power.aura.sh_${crd}.yaml"
  grep -q 'namespaceGroups:' "$chart"
  grep -q 'namespaceGroups:' "$config"
done

# Chart.appVersion omits the conventional Git tag prefix. The release workflow
# must publish and sign the same unprefixed image tag used by Helm defaults.
grep -Eq 'VERSION="?\$\{TAG#v\}"?' .github/workflows/release.yaml
grep -Eq 'IMAGE_SERVER.*\$\{VERSION\}' .github/workflows/release.yaml
grep -Eq 'IMAGE_CONTROLLER.*\$\{VERSION\}' .github/workflows/release.yaml
grep -q 'run: make quality quality-acceptance quality-envtest quality-load' .github/workflows/release.yaml
perl -0ne 'exit(!/kind-acceptance:.*needs: validate/s)' .github/workflows/release.yaml
[[ "$(grep -c 'needs: kind-acceptance' .github/workflows/release.yaml)" -eq 2 ]]
perl -0ne 'exit(!/goreleaser:.*needs: sign/s)' .github/workflows/release.yaml
perl -0ne 'exit(!/helm:.*needs: \[sign, goreleaser\]/s)' .github/workflows/release.yaml
perl -0ne 'exit(!/promote-latest:.*needs: helm/s)' .github/workflows/release.yaml
if sed -n '/docker-manifest:/,/^  sign:/p' .github/workflows/release.yaml | grep -q ':latest'; then
  echo "mutable latest must not be published before signing and artifact release" >&2
  exit 1
fi
grep -Eq 'cosign sign --yes.*@\$\{SERVER_DIGEST\}' .github/workflows/release.yaml
grep -Eq 'cosign sign --yes.*@\$\{CONTROLLER_DIGEST\}' .github/workflows/release.yaml

default_render="$(mktemp)"
ephemeral_render="$(mktemp)"
external_secret_render="$(mktemp)"
trap 'rm -f "$default_render" "$ephemeral_render" "$external_secret_render"' EXIT

helm template aura-power charts/aura-power >"$default_render"
helm template aura-power charts/aura-power --set server.persistence.enabled=false >"$ephemeral_render"
helm template aura-power charts/aura-power --set server.auth.existingSecret=managed-auth >"$external_secret_render"

grep -q 'name: ACCESS_TOKEN_TTL' "$default_render"
grep -q 'name: REFRESH_TOKEN_TTL' "$default_render"
grep -q 'name: CONTROL_NAMESPACE' "$default_render"
grep -q 'name: LEADER_ELECTION_ENABLED' "$default_render"
grep -q 'name: SYSTEM_NAMESPACES' "$default_render"
perl -0ne 'exit(!/name: data\n\s+emptyDir:/s)' "$ephemeral_render"
if grep -q '# Source: aura-power/templates/server-secret.yaml' "$external_secret_render"; then
  echo "server.auth.existingSecret unexpectedly rendered a managed Secret" >&2
  exit 1
fi
perl -0ne 'exit(!/name: managed-auth\n\s+key: jwt-secret/s)' "$external_secret_render"
perl -0ne 'exit(!/name: managed-auth\n\s+key: admin-password/s)' "$external_secret_render"

if helm template aura-power charts/aura-power --set server.replicas=2 >/dev/null 2>&1; then
  echo "server.replicas=2 must be rejected while SQLite is pod-local" >&2
  exit 1
fi
if helm template aura-power charts/aura-power --set controller.replicas=2 --set controller.leaderElection.enabled=false >/dev/null 2>&1; then
  echo "multiple controllers without leader election must be rejected" >&2
  exit 1
fi

observability_render="$(helm template aura-power charts/aura-power --namespace aura-system --set networkPolicy.enabled=true --set serviceMonitor.enabled=true --set serviceMonitor.namespace=monitoring)"
grep -q 'port: 9003' <<<"$observability_render"
[[ "$(grep -c '^kind: ServiceMonitor$' <<<"$observability_render")" -eq 2 ]]
[[ "$(grep -c '^  namespace: monitoring$' <<<"$observability_render")" -eq 2 ]]
[[ "$(grep -c '^      - aura-system$' <<<"$observability_render")" -eq 2 ]]

controller_network_render="$(helm template aura-power charts/aura-power --set networkPolicy.enabled=true --set server.enabled=false --set 'networkPolicy.additionalControllerEgressPorts[0]=8088')"
grep -q 'port: 8088' <<<"$controller_network_render"
server_network_render="$(helm template aura-power charts/aura-power --set networkPolicy.enabled=true --set controller.enabled=false --set 'networkPolicy.additionalControllerEgressPorts[0]=8088')"
if grep -q 'port: 8088' <<<"$server_network_render"; then
  echo "controller notification egress leaked into the server NetworkPolicy" >&2
  exit 1
fi

memory_render="$(helm template aura-power charts/aura-power --set serviceMonitor.enabled=true --set prometheusRule.enabled=true --set controller.config.pprofBindAddress=127.0.0.1:6060)"
grep -q 'name: PPROF_BIND_ADDRESS' <<<"$memory_render"
grep -q 'value: "127.0.0.1:6060"' <<<"$memory_render"
grep -q '^kind: PrometheusRule$' <<<"$memory_render"
grep -q 'alert: AuraPowerControllerHeapNearLimit' <<<"$memory_render"
grep -q 'alert: AuraPowerControllerWorkingSetNearLimit' <<<"$memory_render"
grep -q 'alert: AuraPowerControllerOOMKilled' <<<"$memory_render"
if helm template aura-power charts/aura-power --set prometheusRule.enabled=true >/dev/null 2>&1; then
  echo "prometheusRule requires serviceMonitor so controller metrics are scraped" >&2
  exit 1
fi
if helm template aura-power charts/aura-power --set controller.config.pprofBindAddress=0.0.0.0:6060 >/dev/null 2>&1; then
  echo "non-loopback pprof address must be rejected" >&2
  exit 1
fi

external_webhook_render="$(helm template aura-power charts/aura-power --set webhook.enabled=true --set webhook.existingSecret=managed-webhook --set webhook.caBundle=Y2E=)"
grep -q 'secretName: managed-webhook' <<<"$external_webhook_render"
grep -q 'caBundle: Y2E=' <<<"$external_webhook_render"
[[ "$(grep -c '^kind: Secret$' <<<"$external_webhook_render")" -eq 1 ]]
if helm template aura-power charts/aura-power --set webhook.enabled=true --set webhook.existingSecret=managed-webhook >/dev/null 2>&1; then
  echo "webhook.existingSecret without webhook.caBundle must be rejected" >&2
  exit 1
fi

echo "Helm/release contract checks passed"
