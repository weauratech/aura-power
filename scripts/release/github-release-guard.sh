#!/usr/bin/env bash
set -euo pipefail

command -v gh >/dev/null || { echo "gh is required" >&2; exit 1; }
: "${GH_TOKEN:?GH_TOKEN is required}"
: "${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is required}"

mode="${1:-}"
tag="${2:-${GITHUB_REF_NAME:-}}"
[[ "$tag" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] || {
  echo "canonical release tag is required" >&2
  exit 1
}

release_state() {
  local requested_tag="$1" response error_file status
  response="${RUNNER_TEMP:-/tmp}/aura-power-release-${requested_tag}-$$.response"
  error_file="${response}.error"
  set +e
  gh api "repos/${GITHUB_REPOSITORY}/releases/tags/${requested_tag}" >"$response" 2>"$error_file"
  status=$?
  set -e
  if [[ $status -eq 0 ]]; then
    if jq -e '.draft == true' "$response" >/dev/null; then
      printf 'draft\n'
    else
      printf 'published\n'
    fi
    return
  fi
  if grep -Eq '\(HTTP 404\)$' "$error_file"; then
    printf 'absent\n'
    return
  fi
  echo "GitHub release state lookup failed closed for ${requested_tag}" >&2
  cat "$error_file" >&2
  exit 1
}

latest_published_tag() {
  local candidate latest="" releases_file
  releases_file="${RUNNER_TEMP:-/tmp}/aura-power-published-releases-$$.txt"
  if ! gh api --paginate "repos/${GITHUB_REPOSITORY}/releases?per_page=100" \
    --jq '.[] | select(.draft == false and .prerelease == false) | .tag_name' >"$releases_file"; then
    echo "GitHub published-release lookup failed closed" >&2
    return 1
  fi
  while IFS= read -r candidate; do
    [[ "$candidate" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] || continue
    if [[ -z "$latest" || "$(printf '%s\n%s\n' "$latest" "$candidate" | sort -V | tail -n 1)" == "$candidate" ]]; then
      latest="$candidate"
    fi
  done <"$releases_file"
  printf '%s\n' "$latest"
}

upload_immutable_assets() {
  local requested_tag="$1"
  shift
  [[ $# -gt 0 ]] || { echo "at least one release asset is required" >&2; exit 2; }

  local state release_file file name local_digest remote_digest asset_id asset_count remote_file requested_names=""
  if ! state="$(release_state "$requested_tag")"; then exit 1; fi
  [[ "$state" == draft ]] || { echo "release $requested_tag is not a draft" >&2; exit 1; }

  release_file="${RUNNER_TEMP:-/tmp}/aura-power-release-assets-${requested_tag}-$$.json"
  gh api "repos/${GITHUB_REPOSITORY}/releases/tags/${requested_tag}" >"$release_file"

  for file in "$@"; do
    [[ -f "$file" ]] || { echo "release asset does not exist: $file" >&2; exit 1; }
    name="${file##*/}"
    if grep -Fxq "$name" <<<"$requested_names"; then
      echo "duplicate release asset name: $name" >&2
      exit 1
    fi
    requested_names="${requested_names}${name}"$'\n'

    asset_count="$(jq --arg name "$name" '[.assets[] | select(.name == $name)] | length' "$release_file")"
    [[ "$asset_count" -le 1 ]] || { echo "duplicate remote release asset name: $name" >&2; exit 1; }
    if [[ "$asset_count" -eq 0 ]]; then
      gh release upload "$requested_tag" "$file" --repo "$GITHUB_REPOSITORY"
      continue
    fi

    asset_id="$(jq -r --arg name "$name" '.assets[] | select(.name == $name) | .id' "$release_file")"
    local_digest="sha256:$(sha256sum "$file" | awk '{print $1}')"
    remote_digest="$(jq -r --argjson id "$asset_id" '.assets[] | select(.id == $id) | (.digest // "")' "$release_file")"
    if [[ -z "$remote_digest" ]]; then
      remote_file="${RUNNER_TEMP:-/tmp}/aura-power-release-asset-${asset_id}-$$"
      gh api "repos/${GITHUB_REPOSITORY}/releases/assets/${asset_id}" \
        -H 'Accept: application/octet-stream' >"$remote_file"
      remote_digest="sha256:$(sha256sum "$remote_file" | awk '{print $1}')"
    fi
    [[ "$remote_digest" == "$local_digest" ]] || {
      echo "release asset $name already exists with digest $remote_digest, expected $local_digest" >&2
      exit 1
    }
    echo "release asset $name already exists with the expected digest"
  done
}

case "$mode" in
  state)
    release_state "$tag"
    ;;
  latest)
    latest_published_tag
    ;;
  assert-releasable)
    if ! state="$(release_state "$tag")"; then exit 1; fi
    [[ "$state" != published ]] || { echo "published GitHub Release already exists for $tag" >&2; exit 1; }
    if ! latest="$(latest_published_tag)"; then exit 1; fi
    if [[ -n "$latest" ]]; then
      newest="$(printf '%s\n%s\n' "$latest" "$tag" | sort -V | tail -n 1)"
      [[ "$newest" == "$tag" && "$latest" != "$tag" ]] || {
        echo "release $tag is not newer than published release $latest" >&2
        exit 1
      }
    fi
    printf 'github_release_guard=passed state=%s latest=%s\n' "$state" "${latest:-none}"
    ;;
  ensure-draft)
    if ! state="$(release_state "$tag")"; then exit 1; fi
    case "$state" in
      draft) ;;
      absent) gh release create "$tag" --repo "$GITHUB_REPOSITORY" --verify-tag --draft --title "$tag" --generate-notes ;;
      published) echo "refusing to replace published release $tag" >&2; exit 1 ;;
    esac
    if ! state="$(release_state "$tag")"; then exit 1; fi
    [[ "$state" == draft ]] || { echo "release $tag is not a draft" >&2; exit 1; }
    ;;
  upload-assets)
    shift 2
    upload_immutable_assets "$tag" "$@"
    ;;
  *)
    echo "usage: $0 {state|latest|assert-releasable|ensure-draft|upload-assets} [vMAJOR.MINOR.PATCH] [assets...]" >&2
    exit 2
    ;;
esac
