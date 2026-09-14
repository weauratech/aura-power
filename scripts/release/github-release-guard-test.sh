#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT

cat >"$scratch/gh" <<'FAKE_GH'
#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == api && "${2:-}" == repos/*/releases/tags/* ]]; then
  case "${FAKE_RELEASE_STATE:-absent}" in
    absent) echo 'gh: Not Found (HTTP 404)' >&2; exit 1 ;;
    error) echo 'gh: service unavailable (HTTP 500)' >&2; exit 1 ;;
    draft)
      if [[ -n "${FAKE_ASSET_NAME:-}" ]]; then
        printf '{"draft":true,"assets":[{"id":1,"name":"%s","digest":"%s"}]}\n' "$FAKE_ASSET_NAME" "${FAKE_ASSET_DIGEST:-}"
      else
        printf '{"draft":true,"assets":[]}\n'
      fi
      ;;
    published) printf '{"draft":false}\n' ;;
  esac
  exit 0
fi
if [[ "${1:-}" == release && "${2:-}" == upload ]]; then
  printf '%s\n' "$*" >>"${FAKE_UPLOAD_LOG:?}"
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
common=(env PATH="$scratch:$PATH" GH_TOKEN=test GITHUB_REPOSITORY=weauratech/aura-power RUNNER_TEMP="$scratch")

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
if FAKE_RELEASE_STATE=absent FAKE_LIST_ERROR=true "${common[@]}" "$guard" assert-releasable v2.2.0 >/dev/null 2>&1; then
  echo "GitHub release-list API failure was treated as an empty list" >&2
  exit 1
fi
if FAKE_RELEASE_STATE=absent FAKE_PUBLISHED_TAGS=v2.3.0 "${common[@]}" "$guard" assert-releasable v2.2.0 >/dev/null 2>&1; then
  echo "non-monotonic release was accepted" >&2
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

echo "GitHub release guard contracts passed"
