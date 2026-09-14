#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo_root"

tag="${RELEASE_TAG:-${GITHUB_REF_NAME:-}}"
[[ "$tag" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] || {
  echo "release tag must be canonical SemVer with a v prefix: ${tag:-<empty>}" >&2
  exit 1
}
version="${tag#v}"

chart_version="$(awk '$1 == "version:" {print $2; exit}' charts/aura-power/Chart.yaml | tr -d '"')"
app_version="$(awk '$1 == "appVersion:" {print $2; exit}' charts/aura-power/Chart.yaml | tr -d '"')"
[[ "$chart_version" == "$version" ]] || {
  echo "Chart.version ${chart_version} does not match release ${version}" >&2
  exit 1
}
[[ "$app_version" == "$version" ]] || {
  echo "Chart.appVersion ${app_version} does not match release ${version}" >&2
  exit 1
}
[[ "$chart_version" == "$app_version" ]] || {
  echo "Chart.version and Chart.appVersion must identify one release" >&2
  exit 1
}

if [[ "${REQUIRE_TAG_REF:-true}" == "true" ]]; then
  [[ "${GITHUB_REF_TYPE:-tag}" == "tag" ]] || { echo "release must run from a tag ref" >&2; exit 1; }
  [[ "$(git tag --list "$tag" | wc -l | tr -d ' ')" == "1" ]] || {
    echo "release tag must exist exactly once: $tag" >&2
    exit 1
  }
  [[ "$(git rev-list -n 1 "$tag")" == "${GITHUB_SHA:-$(git rev-parse HEAD)}" ]] || {
    echo "release tag does not resolve to the workflow commit" >&2
    exit 1
  }
fi

if [[ "${REQUIRE_MAIN_ANCESTRY:-true}" == "true" ]]; then
  git show-ref --verify --quiet refs/remotes/origin/main || {
    echo "origin/main is unavailable; checkout must fetch full history" >&2
    exit 1
  }
  git merge-base --is-ancestor "${GITHUB_SHA:-$(git rev-parse HEAD)}" origin/main || {
    echo "release commit is not reachable from origin/main" >&2
    exit 1
  }
fi

printf 'release_preflight=passed tag=%s version=%s chart=%s\n' "$tag" "$version" "$chart_version"
