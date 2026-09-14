#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT

cat >"$scratch/gh" <<'FAKE_GH'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"${FAKE_CALL_LOG:?}"

# This endpoint deliberately cannot see drafts. The guard must never depend on
# it for state or asset lookup.
if [[ "${1:-}" == api && "${2:-}" == repos/*/releases/tags/* ]]; then
  echo 'gh: Not Found (HTTP 404)' >&2
  exit 1
fi

state="${FAKE_RELEASE_STATE:-absent}"
if [[ -n "${FAKE_STATE_FILE:-}" && -s "${FAKE_STATE_FILE}" ]]; then state="$(cat "${FAKE_STATE_FILE}")"; fi

if [[ "${1:-}" == api && "${2:-}" == --paginate && "$*" == *'.tag_name == '* ]]; then
  [[ "${FAKE_LOOKUP_ERROR:-false}" != true ]] || { echo 'gh: service unavailable (HTTP 500)' >&2; exit 1; }
  asset='[]'
  if [[ -n "${FAKE_ASSET_NAME:-}" ]]; then
    asset="$(printf '[{"id":11,"name":"%s","digest":"%s"}]' "$FAKE_ASSET_NAME" "${FAKE_ASSET_DIGEST:-}")"
  fi
  case "$state" in
    absent) ;;
    error) echo 'gh: service unavailable (HTTP 500)' >&2; exit 1 ;;
    draft) printf '{"id":101,"tag_name":"v2.2.0","draft":true,"assets":%s}\n' "$asset" ;;
    published) printf '{"id":101,"tag_name":"v2.2.0","draft":false,"assets":%s}\n' "$asset" ;;
    duplicate)
      printf '{"id":101,"tag_name":"v2.2.0","draft":true,"assets":[]}\n'
      printf '{"id":102,"tag_name":"v2.2.0","draft":false,"assets":[]}\n'
      ;;
    malformed) printf '{"id":"bad","tag_name":"v2.2.0","draft":"yes","assets":[]}\n' ;;
  esac
  exit 0
fi
if [[ "${1:-}" == api && "${2:-}" == repos/*/releases/101 ]]; then
  detail_state="${FAKE_DETAIL_STATE:-$state}"
  draft=true
  [[ "$detail_state" != published ]] || draft=false
  asset='[]'
  if [[ -n "${FAKE_ASSET_NAME:-}" ]]; then
    asset="$(printf '[{"id":11,"name":"%s","digest":"%s"}]' "$FAKE_ASSET_NAME" "${FAKE_ASSET_DIGEST:-}")"
  fi
  printf '{"id":101,"tag_name":"%s","draft":%s,"assets":%s}\n' "${FAKE_DETAIL_TAG:-v2.2.0}" "$draft" "$asset"
  exit 0
fi
if [[ "${1:-}" == release && "${2:-}" == upload ]]; then
  printf '%s\n' "$*" >>"${FAKE_UPLOAD_LOG:?}"
  exit 0
fi
if [[ "${1:-}" == release && "${2:-}" == create ]]; then
  printf 'draft\n' >"${FAKE_STATE_FILE:?}"
  printf 'created\n' >>"${FAKE_CREATE_LOG:?}"
  exit 0
fi
if [[ "${1:-}" == api && "${2:-}" == --paginate ]]; then
  [[ "${FAKE_LIST_ERROR:-false}" != true ]] || { echo 'gh: service unavailable (HTTP 500)' >&2; exit 1; }
  printf '%s\n' ${FAKE_PUBLISHED_TAGS:-}
  exit 0
fi
echo "unexpected fake gh invocation: $*" >&2
exit 2
FAKE_GH
chmod +x "$scratch/gh"

guard="$repo_root/scripts/release/github-release-guard.sh"
call_log="$scratch/calls"
: >"$call_log"
common=(env PATH="$scratch:$PATH" GH_TOKEN=test GITHUB_REPOSITORY=weauratech/aura-power RUNNER_TEMP="$scratch" FAKE_CALL_LOG="$call_log")

FAKE_RELEASE_STATE=absent FAKE_PUBLISHED_TAGS=v2.1.7 "${common[@]}" "$guard" assert-releasable v2.2.0 >/dev/null
FAKE_RELEASE_STATE=draft FAKE_PUBLISHED_TAGS=v2.1.7 "${common[@]}" "$guard" assert-releasable v2.2.0 >/dev/null

if FAKE_RELEASE_STATE=published "${common[@]}" "$guard" assert-releasable v2.2.0 >/dev/null 2>&1; then
  echo "published release was accepted" >&2
  exit 1
fi
if FAKE_RELEASE_STATE=error "${common[@]}" "$guard" assert-releasable v2.2.0 >/dev/null 2>&1; then
  echo "GitHub release API failure was treated as absence" >&2
  exit 1
fi
if FAKE_RELEASE_STATE=absent FAKE_LOOKUP_ERROR=true "${common[@]}" "$guard" assert-releasable v2.2.0 >/dev/null 2>&1; then
  echo "GitHub release lookup failure was treated as absence" >&2
  exit 1
fi
if FAKE_RELEASE_STATE=absent FAKE_LIST_ERROR=true "${common[@]}" "$guard" assert-releasable v2.2.0 >/dev/null 2>&1; then
  echo "GitHub release-list API failure was treated as an empty list" >&2
  exit 1
fi
if FAKE_RELEASE_STATE=absent FAKE_PUBLISHED_TAGS=v2.3.0 "${common[@]}" "$guard" assert-releasable v2.2.0 >/dev/null 2>&1; then
  echo "non-monotonic release was accepted" >&2
  exit 1
fi

# Drafts are invisible to /releases/tags/:tag but visible in the paginated
# collection and by numeric ID. State checks, creation and mutation must use
# that path and fail closed on duplicates or a concurrent state transition.
: >"$call_log"
FAKE_RELEASE_STATE=draft FAKE_PUBLISHED_TAGS=v2.1.7 "${common[@]}" "$guard" ensure-draft v2.2.0
if grep -q '/releases/tags/' "$call_log"; then
  echo "draft lookup used the endpoint that excludes drafts" >&2
  exit 1
fi
state_file="$scratch/state"
create_log="$scratch/creates"
: >"$state_file"
: >"$create_log"
FAKE_RELEASE_STATE=absent FAKE_STATE_FILE="$state_file" FAKE_CREATE_LOG="$create_log" \
  "${common[@]}" "$guard" ensure-draft v2.2.0
[[ "$(cat "$state_file")" == draft ]]
[[ "$(wc -l <"$create_log" | tr -d ' ')" == 1 ]]
if FAKE_RELEASE_STATE=duplicate "${common[@]}" "$guard" ensure-draft v2.2.0 >/dev/null 2>&1; then
  echo "duplicate release records were accepted" >&2
  exit 1
fi
if FAKE_RELEASE_STATE=draft FAKE_DETAIL_STATE=published "${common[@]}" "$guard" ensure-draft v2.2.0 >/dev/null 2>&1; then
  echo "release state transition between list and ID lookup was accepted" >&2
  exit 1
fi
if FAKE_RELEASE_STATE=malformed "${common[@]}" "$guard" ensure-draft v2.2.0 >/dev/null 2>&1; then
  echo "malformed release record was accepted" >&2
  exit 1
fi

asset="$scratch/aura-power.tar.gz"
printf 'immutable release asset\n' >"$asset"
asset_digest="sha256:$(sha256sum "$asset" | awk '{print $1}')"
upload_log="$scratch/uploads"
: >"$upload_log"
FAKE_RELEASE_STATE=draft FAKE_UPLOAD_LOG="$upload_log" "${common[@]}" "$guard" upload-assets v2.2.0 "$asset"
grep -Fq 'release upload v2.2.0' "$upload_log"
: >"$upload_log"
FAKE_RELEASE_STATE=draft FAKE_ASSET_NAME="${asset##*/}" FAKE_ASSET_DIGEST="$asset_digest" FAKE_UPLOAD_LOG="$upload_log" \
  "${common[@]}" "$guard" upload-assets v2.2.0 "$asset" >/dev/null
[[ ! -s "$upload_log" ]]
if FAKE_RELEASE_STATE=draft FAKE_ASSET_NAME="${asset##*/}" \
  FAKE_ASSET_DIGEST=sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa \
  FAKE_UPLOAD_LOG="$upload_log" "${common[@]}" "$guard" upload-assets v2.2.0 "$asset" >/dev/null 2>&1; then
  echo "release asset with a different digest was overwritten" >&2
  exit 1
fi
if FAKE_RELEASE_STATE=published FAKE_UPLOAD_LOG="$upload_log" \
  "${common[@]}" "$guard" upload-assets v2.2.0 "$asset" >/dev/null 2>&1; then
  echo "published release accepted an asset upload" >&2
  exit 1
fi

echo "GitHub release guard contracts passed"
