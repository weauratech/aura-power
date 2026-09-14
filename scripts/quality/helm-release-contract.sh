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
grep -q '^version: 2.3.0$' charts/aura-power/Chart.yaml
grep -q '^appVersion: "2.3.0"$' charts/aura-power/Chart.yaml
if grep -R --line-number -- '--token=' scripts/quality/eks-*.sh; then
  echo "EKS quality scripts must not expose bearer tokens in process arguments" >&2
  exit 1
fi
grep -q 'aws_cmd eks update-kubeconfig' scripts/quality/eks-core-journey.sh
grep -q 'AURA_POWER_RUNTIME_KUBECONFIG' scripts/quality/eks-fixture-watchdog.sh
grep -Fq 'create -f - -o json' scripts/quality/eks-core-journey.sh
grep -Fq 'write_recovery_state' scripts/quality/eks-core-journey.sh
grep -Fq 'RECOVERY_STATE' scripts/quality/eks-fixture-watchdog.sh
grep -Fq '"create deployments.apps ${FIXTURE_NAMESPACE}"' scripts/quality/eks-core-journey.sh
grep -Fq '"update deployments.apps ${FIXTURE_NAMESPACE}"' scripts/quality/eks-core-journey.sh
grep -Fq '"update deployments.apps/scale ${FIXTURE_NAMESPACE}"' scripts/quality/eks-core-journey.sh
grep -Fq '"patch deployments.apps ${FIXTURE_NAMESPACE}"' scripts/quality/eks-core-journey.sh
grep -Fq '"delete deployments.apps ${FIXTURE_NAMESPACE}"' scripts/quality/eks-core-journey.sh
grep -Fq '"create powerpolicies.power.aura.sh ${CONTROL_NAMESPACE}"' scripts/quality/eks-core-journey.sh
grep -Fq 'POLICY_UID=' scripts/quality/eks-core-journey.sh
grep -Fq 'preconditions:{uid:$uid}' scripts/quality/eks-fixture-watchdog.sh
grep -Fq 'kube delete --raw "$api_path" -f -' scripts/quality/eks-fixture-watchdog.sh
grep -Fq 'RUNTIME_KUBECONFIG="${RECOVERY_DIR}/kubeconfig"' scripts/quality/eks-core-journey.sh
grep -Fq 'mkdir "$SUPERVISOR_READY"' scripts/quality/eks-fixture-watchdog.sh
grep -Fq 'same_process_identity "$PARENT_PID" "$PARENT_IDENTITY"' scripts/quality/eks-fixture-watchdog.sh
grep -Fq 'run_with_process_timeout "$WATCHDOG_COMMAND_TIMEOUT_SECONDS"' scripts/quality/eks-fixture-watchdog.sh
grep -Fq 'PROCESS_GUARD_ACTIVE_FILE="${RECOVERY_DIR}/active-command"' scripts/quality/eks-core-journey.sh
grep -Fq 'quiesce_active_process_state "$ACTIVE_COMMAND_STATE"' scripts/quality/eks-fixture-watchdog.sh
grep -Fq 'WATCHDOG_LOG="${RECOVERY_DIR}/watchdog.log"' scripts/quality/eks-core-journey.sh
grep -Fq 'CHANNEL_WATCH_LOG="${RECOVERY_DIR}/channel-watch.log"' scripts/quality/eks-core-journey.sh
perl -0ne 'exit(!/nohup "\$WATCHDOG" watch.*supervisor-ready.*recovery supervisor lost after readiness.*namespace_response=.*kube create/s)' scripts/quality/eks-core-journey.sh
perl -0ne 'exit(!/cleanup_on_exit\(\).*quiesce_active_process_state.*rmdir "\$MUTATION_IN_PROGRESS"/s)' scripts/quality/eks-core-journey.sh
perl -0ne 'exit(!/stat -c '\''%a'\''.*stat -f '\''%Lp'\''/s)' scripts/quality/eks-fixture-watchdog.sh
scripts/quality/process-guard-test.sh
scripts/quality/eks-fixture-watchdog-test.sh
grep -Fq 'group: aura-power-release-promotion' .github/workflows/release.yaml
perl -0ne 'exit(!/validate:.*Validate release identity.*scripts\/release\/preflight\.sh/s)' .github/workflows/release.yaml
grep -Fq 'scripts/release/github-release-guard.sh assert-releasable "$GITHUB_REF_NAME"' .github/workflows/release.yaml
grep -Fq 'scripts/release/github-release-guard.sh ensure-draft "$GITHUB_REF_NAME"' .github/workflows/release.yaml
if grep -Eq 'gh release view .*\|\| true' .github/workflows/release.yaml; then
  echo "GitHub release state checks must fail closed" >&2
  exit 1
fi
RELEASE_TAG=v2.3.0 REQUIRE_TAG_REF=false REQUIRE_MAIN_ANCESTRY=false scripts/release/preflight.sh >/dev/null
if RELEASE_TAG=2.3.0 REQUIRE_TAG_REF=false REQUIRE_MAIN_ANCESTRY=false scripts/release/preflight.sh >/dev/null 2>&1; then
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
perl -0ne 'exit(!/kind-acceptance:.*Export and verify disposable Kind kubeconfig.*test -f "\$kubeconfig".*current-context\)" = kind-aura-power-quality.*KUBECONFIG=\$kubeconfig.*GITHUB_ENV/s)' .github/workflows/release.yaml
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
for digest_field in serverDigest serverAMD64 serverARM64 controllerDigest controllerAMD64 controllerARM64; do
  grep -Fq -- "--arg ${digest_field}" .github/workflows/release.yaml
done
grep -Fq 'serverDigests:{index:$serverDigest,linuxAmd64:$serverAMD64,linuxArm64:$serverARM64}' .github/workflows/release.yaml
grep -Fq 'controllerDigests:{index:$controllerDigest,linuxAmd64:$controllerAMD64,linuxArm64:$controllerARM64}' .github/workflows/release.yaml
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

# Production examples preserve environment-specific values, clear mutable tags,
# and prove the rendered upgrade against the live API before mutation.
assert_safe_upgrade_docs() {
  local release_doc="$1"
  local upgrade_block
  upgrade_block="$(awk '/^helm upgrade --install aura-power / { capture=1 } capture && /^```$/ { exit } capture { print }' "$release_doc")"
  grep -Fq 'Existing-release upgrades in this example require Helm 3.14 or later' "$release_doc" || return 1
  grep -Eq '^  --reset-then-reuse-values \\$' <<<"$upgrade_block" || return 1
  grep -Eq '^  --set-string server\.image\.tag= \\$' <<<"$upgrade_block" || return 1
  grep -Eq '^  --set-string controller\.image\.tag= \\$' <<<"$upgrade_block" || return 1
  grep -Eq '^  --set-string server\.image\.digest=.* \\$' <<<"$upgrade_block" || return 1
  grep -Eq '^  --set-string controller\.image\.digest=.* \\$' <<<"$upgrade_block" || return 1
  grep -Eq '^  --dry-run=server$' <<<"$upgrade_block" || return 1
  perl -0ne 'exit(!/kubectl apply --server-side --dry-run=server --field-manager=aura-power-release.*kubectl apply --server-side --field-manager=aura-power-release/s)' "$release_doc" || return 1
}
assert_safe_upgrade_docs docs/release-security.md
upgrade_doc_mutation="$(mktemp)"
sed '/--dry-run=server$/d' docs/release-security.md >"$upgrade_doc_mutation"
if assert_safe_upgrade_docs "$upgrade_doc_mutation"; then
  echo "production upgrade documentation contract accepted a missing Helm dry run" >&2
  exit 1
fi
rm -f "$upgrade_doc_mutation"
grep -Fq -- 'EXPECTED_CONTEXT=aura-power-quality-eks-operations' docs/upgrade-v2.md
grep -Fq -- '--expected-context "$EXPECTED_CONTEXT"' docs/upgrade-v2.md
if grep -Fq -- '--expected-context "$(kubectl config current-context)"' docs/upgrade-v2.md; then
  echo "downgrade documentation derives expected context from the active context" >&2
  exit 1
fi

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
scripts/release/package-chart-reproducibly.sh charts/aura-power "$chart_repro_dir/one" 2.3.0 2.3.0
sleep 1
scripts/release/package-chart-reproducibly.sh charts/aura-power "$chart_repro_dir/two" 2.3.0 2.3.0
cmp "$chart_repro_dir/one/aura-power-2.3.0.tgz" "$chart_repro_dir/two/aura-power-2.3.0.tgz"

default_render="$(mktemp)"
ephemeral_render="$(mktemp)"
external_secret_render="$(mktemp)"
retained_secret_render="$(mktemp)"
trap 'rm -f "$default_render" "$ephemeral_render" "$external_secret_render" "$retained_secret_render"' EXIT

helm template aura-power charts/aura-power >"$default_render"
helm template aura-power charts/aura-power --set server.persistence.enabled=false >"$ephemeral_render"
helm template aura-power charts/aura-power --set server.auth.existingSecret=managed-auth >"$external_secret_render"
helm template aura-power charts/aura-power --set server.auth.keepManagedSecret=true >"$retained_secret_render"

grep -q 'name: ACCESS_TOKEN_TTL' "$default_render"
grep -q 'name: REFRESH_TOKEN_TTL' "$default_render"
grep -q 'name: CONTROL_NAMESPACE' "$default_render"
grep -q 'name: LEADER_ELECTION_ENABLED' "$default_render"
grep -q 'name: SYSTEM_NAMESPACES' "$default_render"
grep -q 'path: /readyz' "$default_render"
grep -Fq '.status.recentAttempts[]?.auditEventRefs[]? // empty' scripts/quality/eks-core-journey.sh
grep -Fq 'same_process_identity "$CHANNEL_WATCH_PID" "$CHANNEL_WATCH_IDENTITY"' scripts/quality/eks-core-journey.sh
grep -Fq 'any(.items[]; .spec.action == "workload.powered_down") and any(.items[]; .spec.action == "workload.restored")' scripts/quality/eks-core-journey.sh
perl -0ne 'exit(!/name: data\n\s+emptyDir:/s)' "$ephemeral_render"
if grep -q '# Source: aura-power/templates/server-secret.yaml' "$external_secret_render"; then
  echo "server.auth.existingSecret unexpectedly rendered a managed Secret" >&2
  exit 1
fi
perl -0ne 'exit(!/name: managed-auth\n\s+key: jwt-secret/s)' "$external_secret_render"
perl -0ne 'exit(!/name: managed-auth\n\s+key: admin-password/s)' "$external_secret_render"
perl -0ne 'exit(!/name: aura-power-server-secret\n\s+annotations:\n\s+helm.sh\/resource-policy: keep/s)' "$retained_secret_render"

grep -q 'image: "ghcr.io/weauratech/aura-power-server:2.3.0"' "$default_render"
grep -q 'image: "ghcr.io/weauratech/aura-power-controller:2.3.0"' "$default_render"
server_digest="sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
controller_digest="sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
digest_render="$(helm template aura-power charts/aura-power --set server.image.digest="$server_digest" --set controller.image.digest="$controller_digest")"
grep -q "image: \"ghcr.io/weauratech/aura-power-server@${server_digest}\"" <<<"$digest_render"
grep -q "image: \"ghcr.io/weauratech/aura-power-controller@${controller_digest}\"" <<<"$digest_render"
if helm template aura-power charts/aura-power --set server.image.tag=v2.3.0 --set server.image.digest="$server_digest" >/dev/null 2>&1; then
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
maintenance_render="$(helm template aura-power charts/aura-power --set server.replicas=0 --set controller.replicas=0)"
grep -q '^  replicas: 0$' <<<"$maintenance_render"
[[ "$(grep -c '^  replicas: 0$' <<<"$maintenance_render")" -eq 2 ]]
grep -Fq 'server is paused with server.replicas=0' charts/aura-power/templates/NOTES.txt
grep -Fq 'controller is paused with controller.replicas=0' charts/aura-power/templates/NOTES.txt
grep -Fq 'DELETE is not intercepted' charts/aura-power/templates/NOTES.txt
grep -Fq 'failurePolicy={{ .Values.webhook.failurePolicy }}' charts/aura-power/templates/NOTES.txt
grep -Fq 'The admission webhook is disabled' charts/aura-power/templates/NOTES.txt
fail_closed_render="$(helm template aura-power charts/aura-power --set webhook.enabled=true --set webhook.failurePolicy=Fail)"
[[ "$(grep -c '^    failurePolicy: Fail$' <<<"$fail_closed_render")" -eq 2 ]]
ignore_render="$(helm template aura-power charts/aura-power --set webhook.enabled=true --set webhook.failurePolicy=Ignore)"
[[ "$(grep -c '^    failurePolicy: Ignore$' <<<"$ignore_render")" -eq 2 ]]
disabled_webhook_render="$(helm template aura-power charts/aura-power --set webhook.enabled=false)"
if grep -q '^kind: ValidatingWebhookConfiguration$' <<<"$disabled_webhook_render"; then
  echo "disabled admission unexpectedly rendered a webhook configuration" >&2
  exit 1
fi
current_server_digest="sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
current_controller_digest="sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
quiescence_render="$(helm template aura-power charts/aura-power -f <(cat <<YAML
server:
  replicas: 1
  image:
    repository: registry.example/current-server
    tag: ""
    digest: ${current_server_digest}
controller:
  replicas: 0
  image:
    repository: registry.example/current-controller
    tag: ""
    digest: ${current_controller_digest}
YAML
))"
grep -q "image: \"registry.example/current-server@${current_server_digest}\"" <<<"$quiescence_render"
grep -q "image: \"registry.example/current-controller@${current_controller_digest}\"" <<<"$quiescence_render"
if grep -Eq 'image: .*:2\.2\.2' <<<"$quiescence_render"; then
  echo "maintenance quiescence unexpectedly selected candidate application images" >&2
  exit 1
fi
grep -Fq 'CURRENT_SERVER_DIGEST' charts/aura-power/README.md
grep -Fq 'CURRENT_CONTROLLER_DIGEST' charts/aura-power/README.md
grep -Fq 'At this point no candidate binary has run' charts/aura-power/README.md
grep -Fq '.webhook.enabled == true and .webhook.failurePolicy == "Fail"' charts/aura-power/README.md
grep -Fq 'DELETE' charts/aura-power/README.md
grep -Fq 'holderIdentity' charts/aura-power/README.md
grep -Fq 'complete configured discovery and reconciliation intervals' charts/aura-power/README.md
[[ "$(grep -c -- '--cleanup-on-fail --wait --timeout 8m' charts/aura-power/README.md)" -eq 5 ]]
if helm template aura-power charts/aura-power --set server.replicas=-1 >/dev/null 2>&1; then
  echo "negative server replicas must be rejected" >&2
  exit 1
fi
if helm template aura-power charts/aura-power --set controller.replicas=-1 >/dev/null 2>&1; then
  echo "negative controller replicas must be rejected" >&2
  exit 1
fi
if helm template aura-power charts/aura-power --set controller.replicas=2 --set controller.leaderElection.enabled=false >/dev/null 2>&1; then
  echo "multiple controllers without leader election must be rejected" >&2
  exit 1
fi
helm template aura-power charts/aura-power --set controller.replicas=2 --set controller.leaderElection.enabled=true >/dev/null

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

for workflow in .github/workflows/ci.yaml .github/workflows/release.yaml; do
  perl -0ne 'exit(!/name: Prove managed auth Secret externalization\n\s+run: \|\n\s+export KUBECONFIG="\$\{KUBECONFIG:-\$HOME\/\.kube\/config\}"\n\s+\.\/scripts\/quality\/kind-secret-externalization-journey\.sh/s)' "$workflow" || {
    echo "FAIL: $workflow must give the Secret externalization journey an explicit Kind kubeconfig" >&2
    exit 1
  }
done

echo "Helm/release contract checks passed"
