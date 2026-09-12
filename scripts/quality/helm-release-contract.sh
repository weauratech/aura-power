#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo_root"

for crd in powerpolicies poweroverrides; do
  chart="charts/aura-power/crds/${crd}.yaml"
  config="config/crd/bases/power.aura.sh_${crd}.yaml"
  rg -q 'namespaceGroups:' "$chart"
  rg -q 'namespaceGroups:' "$config"
done

# Chart.appVersion omits the conventional Git tag prefix. The release workflow
# must publish and sign the same unprefixed image tag used by Helm defaults.
rg -q 'VERSION=\$\{TAG#v\}' .github/workflows/release.yaml
rg -q 'IMAGE_SERVER.*\$\{VERSION\}' .github/workflows/release.yaml
rg -q 'IMAGE_CONTROLLER.*\$\{VERSION\}' .github/workflows/release.yaml

echo "Helm/release contract checks passed"
