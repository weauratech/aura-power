#!/usr/bin/env bash
# shellcheck disable=SC2016 # Contracts intentionally match literal shell/GitHub expressions.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo_root"

for crd in powerpolicies poweroverrides; do
  chart="charts/aura-power/crds/${crd}.yaml"
  config="config/crd/bases/power.aura.sh_${crd}.yaml"
  grep -q 'namespaceGroups:' "$chart"
  grep -q 'namespaceGroups:' "$config"
done

# A release has one SemVer identity and validates it before expensive work.
grep -q '^version: 2.2.0$' charts/aura-power/Chart.yaml
grep -q '^appVersion: "2.2.0"$' charts/aura-power/Chart.yaml
grep -Fq 'group: aura-power-release-promotion' .github/workflows/release.yaml
perl -0ne 'exit(!/validate:.*Validate release identity.*scripts\/release\/preflight\.sh/s)' .github/workflows/release.yaml
grep -Fq 'scripts/release/github-release-guard.sh assert-releasable "$GITHUB_REF_NAME"' .github/workflows/release.yaml
grep -Fq 'scripts/release/github-release-guard.sh ensure-draft "$GITHUB_REF_NAME"' .github/workflows/release.yaml
if grep -Eq 'gh release view .*\|\| true' .github/workflows/release.yaml; then
  echo "GitHub release state checks must fail closed" >&2
  exit 1
fi
RELEASE_TAG=v2.2.0 REQUIRE_TAG_REF=false REQUIRE_MAIN_ANCESTRY=false scripts/release/preflight.sh >/dev/null
if RELEASE_TAG=2.2.0 REQUIRE_TAG_REF=false REQUIRE_MAIN_ANCESTRY=false scripts/release/preflight.sh >/dev/null 2>&1; then
  echo "release preflight accepted a tag without the v prefix" >&2
  exit 1
fi
if RELEASE_TAG=v2.2.1 REQUIRE_TAG_REF=false REQUIRE_MAIN_ANCESTRY=false scripts/release/preflight.sh >/dev/null 2>&1; then
  echo "release preflight accepted a tag that differs from the chart" >&2
  exit 1
fi
grep -q 'run: make quality quality-acceptance quality-envtest quality-load' .github/workflows/release.yaml
grep -q './scripts/quality/kind-hpa-journey.sh' .github/workflows/release.yaml
grep -q './scripts/quality/kind-control-namespace-journey.sh' .github/workflows/release.yaml
perl -0ne 'exit(!/authorize-release:.*needs: validate.*environment: release/s)' .github/workflows/release.yaml
perl -0ne 'exit(!/build-amd64:.*needs: authorize-release/s)' .github/workflows/release.yaml
perl -0ne 'exit(!/kind-acceptance:.*needs: build-amd64/s)' .github/workflows/release.yaml
if grep -q '^  publish-amd64:' .github/workflows/release.yaml; then
  echo "single-platform aliases must not be wrapped into a new OCI index" >&2
  exit 1
fi
perl -0ne 'exit(!/build-arm64:.*needs: kind-acceptance/s)' .github/workflows/release.yaml
perl -0ne 'exit(!/assemble-images:.*needs: \[build-amd64, build-arm64\]/s)' .github/workflows/release.yaml
grep -q 'docker pull "\${IMAGE_SERVER}@\${TESTED_SERVER_DIGEST}"' .github/workflows/release.yaml
grep -q 'docker pull "\${IMAGE_CONTROLLER}@\${TESTED_CONTROLLER_DIGEST}"' .github/workflows/release.yaml
[[ "$(grep -c 'push-by-digest=true,name-canonical=true,push=true' .github/workflows/release.yaml)" -eq 4 ]]
[[ "$(grep -c 'VERSION=\${{ github.ref_name }}' .github/workflows/release.yaml)" -eq 4 ]]
[[ "$(grep -c 'COMMIT=\${{ github.sha }}' .github/workflows/release.yaml)" -eq 4 ]]
grep -q 'Execute native arm64 image smoke checks' .github/workflows/release.yaml
grep -q "'{{.Architecture}}'.*= arm64" .github/workflows/release.yaml
grep -q "args: 'release --clean --skip=publish'" .github/workflows/release.yaml
grep -q 'sha256sum --check checksums.txt' .github/workflows/release.yaml
grep -q 'smoke/aura-power --help' .github/workflows/release.yaml
grep -q 'find dist -maxdepth 1 -type f' .github/workflows/release.yaml
grep -q 'charts/staging/\${GITHUB_SHA}-\${GITHUB_RUN_ID}-\${GITHUB_RUN_ATTEMPT}' .github/workflows/release.yaml
grep -Fq 'scripts/release/package-chart-reproducibly.sh charts/aura-power . "$version" "$version"' .github/workflows/release.yaml
grep -Fq 'scripts/release/github-release-guard.sh upload-assets "$GITHUB_REF_NAME" release-assets/*' .github/workflows/release.yaml
if grep -Eq 'gh release upload .*--clobber' .github/workflows/release.yaml; then
  echo "release assets must never be overwritten without a digest match" >&2
  exit 1
fi
grep -q 'version: v2.18.1' .github/workflows/release.yaml
[[ "$(grep -c 'actions/attest@1e69f48acb82d1966a394da916b4c1698aa569d6' .github/workflows/release.yaml)" -eq 11 ]]
grep -q 'artifact-metadata: write' .github/workflows/release.yaml
grep -q 'cosign sign-blob --yes --bundle' .github/workflows/release.yaml
grep -q 'cosign verify-blob' .github/workflows/release.yaml
for sbom in server-amd64 server-arm64 controller-amd64 controller-arm64; do
  [[ "$(grep -c "${sbom}.spdx.json" .github/workflows/release.yaml)" -ge 2 ]]
done
for subject in 'IMAGE_SERVER}@${SERVER_AMD64}' 'IMAGE_SERVER}@${SERVER_ARM64}' 'IMAGE_CONTROLLER}@${CONTROLLER_AMD64}' 'IMAGE_CONTROLLER}@${CONTROLLER_ARM64}'; do
  grep -Fq "gh attestation verify \"oci://\${${subject}\"" .github/workflows/release.yaml
done
if grep -Eq 'syft .*\$\{(SERVER|CONTROLLER)_DIGEST\}' .github/workflows/release.yaml; then
  echo "multi-platform index must not receive a host-selected SBOM" >&2
  exit 1
fi
grep -q 'oras cp .*\${STAGING_CHART}@\${CHART_DIGEST}' .github/workflows/release.yaml
grep -q 'imagetools create.*:latest.*@\${SERVER_DIGEST}' .github/workflows/release.yaml
grep -q 'immutable alias .*already points to' .github/workflows/release.yaml
perl -0ne 'exit(!/Verify staged signatures and attestations.*Verify exact release files before promotion.*Stage draft GitHub Release.*Guard immutable public aliases.*Upload verified release files to the draft.*Promote immutable version aliases.*Verify promoted immutable artifacts.*Promote monotonic latest image aliases.*Publish GitHub Release as the final transition\n\s+env:.*\n\s+run: gh release edit[^\n]+--draft=false[^\n]*\n\z/s)' .github/workflows/release.yaml
grep -Fq 'cosign sign --yes "${IMAGE_SERVER}@${SERVER_DIGEST}"' .github/workflows/release.yaml
grep -Fq 'cosign sign --yes "${IMAGE_CONTROLLER}@${CONTROLLER_DIGEST}"' .github/workflows/release.yaml

# Runtime identity and build inputs are immutable and traceable to the tag.
grep -Eq '^FROM node:22-alpine@sha256:[a-f0-9]{64} AS frontend$' Dockerfile.server
grep -Eq '^FROM golang:1.26-alpine@sha256:[a-f0-9]{64} AS backend$' Dockerfile.server
grep -Eq '^FROM gcr.io/distroless/static-debian12:nonroot@sha256:[a-f0-9]{64}$' Dockerfile.server
grep -Eq '^FROM golang:1.26-alpine@sha256:[a-f0-9]{64} AS backend$' Dockerfile.controller
grep -Eq '^FROM gcr.io/distroless/static-debian12:nonroot@sha256:[a-f0-9]{64}$' Dockerfile.controller
grep -Fq -- '-X main.version=${VERSION} -X main.commit=${COMMIT}' Dockerfile.server
grep -Fq -- '-X main.version=${VERSION} -X main.commit=${COMMIT}' Dockerfile.controller
grep -Fq '"version", version, "commit", commit' cmd/server/main.go
grep -Fq '"version", version, "commit", commit' cmd/controller/main.go
grep -Fq 'cli.NewRootCmdWithVersion(version, commit)' cmd/aura-power/main.go
grep -Fq 'Version: fmt.Sprintf("%s (commit %s)", version, commit)' internal/cli/root.go
scripts/release/github-release-guard-test.sh

chart_repro_dir="$(mktemp -d)"
scripts/release/package-chart-reproducibly.sh charts/aura-power "$chart_repro_dir/one" 2.2.0 2.2.0
sleep 1
scripts/release/package-chart-reproducibly.sh charts/aura-power "$chart_repro_dir/two" 2.2.0 2.2.0
cmp "$chart_repro_dir/one/aura-power-2.2.0.tgz" "$chart_repro_dir/two/aura-power-2.2.0.tgz"

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

grep -q 'image: "ghcr.io/weauratech/aura-power-server:2.2.0"' "$default_render"
grep -q 'image: "ghcr.io/weauratech/aura-power-controller:2.2.0"' "$default_render"
server_digest="sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
controller_digest="sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
digest_render="$(helm template aura-power charts/aura-power --set server.image.digest="$server_digest" --set controller.image.digest="$controller_digest")"
grep -q "image: \"ghcr.io/weauratech/aura-power-server@${server_digest}\"" <<<"$digest_render"
grep -q "image: \"ghcr.io/weauratech/aura-power-controller@${controller_digest}\"" <<<"$digest_render"
if helm template aura-power charts/aura-power --set server.image.tag=v2.2.0 --set server.image.digest="$server_digest" >/dev/null 2>&1; then
  echo "server image tag and digest must be mutually exclusive" >&2
  exit 1
fi
if helm template aura-power charts/aura-power --set controller.image.digest=sha256:not-a-digest >/dev/null 2>&1; then
  echo "malformed controller image digest must be rejected" >&2
  exit 1
fi

# The controller authors only its generated decision resources: built-in
# schedules and namespace-annotation policies. Keep these permissions explicit
# so least-privilege hardening cannot silently disable either feature.
perl -0ne 'exit(!/resources: \["powerpolicies"\]\n\s+verbs: \["get", "list", "watch", "create", "update"\]/s)' "$default_render"
perl -0ne 'exit(!/resources: \["powerschedules"\]\n\s+verbs: \["get", "list", "watch", "create"\]/s)' "$default_render"
if perl -0ne 'exit(!/resources: \["poweroverrides", "powernamespacegroups"\]\n\s+verbs: \[[^\]]*("create"|"update"|"patch"|"delete")/s)' "$default_render"; then
  echo "controller unexpectedly writes user-authored decision inputs" >&2
  exit 1
fi

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
