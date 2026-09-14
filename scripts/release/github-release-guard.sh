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

load_release() {
  local requested_tag="$1" release_file="$2"
  local matches_file matches_raw error_file count release_id listed_draft
  matches_file="${RUNNER_TEMP:-/tmp}/aura-power-release-matches-${requested_tag}-$$.json"
  matches_raw="${matches_file}.raw"
  error_file="${matches_file}.error"

  # GitHub's /releases/tags/:tag endpoint does not return draft releases. List
  # every page, select the exact canonical tag, then dereference the unique ID.
  # This also detects ambiguous states such as concurrent or legacy duplicate
  # drafts instead of guessing which release may be mutated.
  if ! gh api --paginate "repos/${GITHUB_REPOSITORY}/releases?per_page=100" \
    --jq ".[] | select(.tag_name == \"${requested_tag}\")" >"$matches_raw" 2>"$error_file"; then
    echo "GitHub release list lookup failed closed for ${requested_tag}" >&2
    cat "$error_file" >&2
    return 1
  fi
  if ! jq -s '.' "$matches_raw" >"$matches_file"; then
    echo "GitHub release list returned malformed JSON for ${requested_tag}" >&2
    return 1
  fi

  count="$(jq 'length' "$matches_file")"
  if [[ "$count" -eq 0 ]]; then
    jq -n '{state:"absent"}' >"$release_file"
    return
  fi
  if [[ "$count" -ne 1 ]]; then
    echo "GitHub release lookup is ambiguous for ${requested_tag}: ${count} matching releases" >&2
    jq -r '.[] | "id=\(.id // \"unknown\") draft=\(.draft // \"unknown\")"' "$matches_file" >&2
    return 1
  fi

  release_id="$(jq -r '.[0].id // empty' "$matches_file")"
  listed_draft="$(jq -r '.[0].draft | if type == "boolean" then tostring else empty end' "$matches_file")"
  [[ "$release_id" =~ ^[1-9][0-9]*$ && -n "$listed_draft" ]] || {
    echo "GitHub release list returned an invalid release record for ${requested_tag}" >&2
    return 1
  }
  if ! gh api "repos/${GITHUB_REPOSITORY}/releases/${release_id}" >"$release_file" 2>"$error_file"; then
    echo "GitHub release ID lookup failed closed for ${requested_tag} (id ${release_id})" >&2
    cat "$error_file" >&2
    return 1
  fi
  if ! jq -e --arg tag "$requested_tag" --argjson id "$release_id" --arg listed_draft "$listed_draft" \
    '.id == $id and .tag_name == $tag and (.draft | type == "boolean") and (.draft | tostring) == $listed_draft and (.assets | type == "array")' \
    "$release_file" >/dev/null; then
    echo "GitHub release changed or returned an invalid record for ${requested_tag} (id ${release_id})" >&2
    return 1
  fi
}

release_state() {
  local requested_tag="$1" release_file
  release_file="${RUNNER_TEMP:-/tmp}/aura-power-release-${requested_tag}-$$.json"
  load_release "$requested_tag" "$release_file" || return 1
  if jq -e '.state == "absent"' "$release_file" >/dev/null; then
    printf 'absent\n'
  elif jq -e '.draft == true' "$release_file" >/dev/null; then
    printf 'draft\n'
  else
    printf 'published\n'
  fi
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
  release_file="${RUNNER_TEMP:-/tmp}/aura-power-release-assets-${requested_tag}-$$.json"
  load_release "$requested_tag" "$release_file" || exit 1
  if jq -e '.state == "absent"' "$release_file" >/dev/null; then state=absent
  elif jq -e '.draft == true' "$release_file" >/dev/null; then state=draft
  else state=published
  fi
  [[ "$state" == draft ]] || { echo "release $requested_tag is not a draft" >&2; exit 1; }

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
