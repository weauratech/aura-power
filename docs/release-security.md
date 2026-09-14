# Release integrity and verification

Aura Power releases use one version across the Git tag, Helm `version`, Helm
`appVersion`, image tags, and CLI archives. The release workflow accepts only a
canonical `vMAJOR.MINOR.PATCH` SemVer tag whose commit is reachable from
`origin/main`. The current release identity is `v2.3.0` / `2.3.0`.

The amd64 server and controller images are built once and staged in GHCR by
digest without a mutable candidate tag. Kind pulls each candidate by that exact
registry digest. After acceptance, an arm64-hosted runner builds and executes
both arm64 images, rejecting architecture and exec-format mismatches. The four
child manifests are assembled under a commit, run, and attempt scoped staging
tag. Container base images are pinned by digest, and the release tag and commit
are embedded in both binaries and OCI labels. Final version tags are created
from the immutable index digest only after signing, attestations, release-file
verification and draft upload pass. Every tag is resolved back to that digest.

Each architecture child receives its own SPDX SBOM attestation, which is
verified before promotion. A scanner is
never allowed to select the host architecture from a multi-platform index and
bind that partial result to the index. The index receives build provenance.

After acceptance, the workflow:

- stages the final-version Helm package in a commit/run scoped OCI repository,
  verifies it, then copies the same digest to the public chart tag and creates
  final-repository signatures and attestations;
- produces SPDX JSON SBOMs for every image architecture and the exact chart;
- builds CLI archives and per-archive SBOMs once with GoReleaser, verifies every
  checksum, inspects every archive and executes the native Linux amd64 archive;
- creates GitHub artifact attestations for images, chart, and release files;
- signs the image digests, chart digest, and `checksums.txt` with Sigstore
  keyless signing;
- verifies OCI signatures, every architecture SBOM attestation, checksum
  bundles and GitHub attestations before promoting public tags. The GitHub
  Release is made public only after registry promotion, release-file upload and
  diagnostic artifact upload succeed.

The signed `release-manifest.json` asset records the exact commit, the server
and controller index digests, both `linux/amd64` and `linux/arm64` child
digests, and the chart digest. The existing `serverImage` and `controllerImage`
references remain for compatibility; `serverDigests` and `controllerDigests`
are the machine-readable source for deployment and runtime verification.
Install production releases by index digest. `image.digest` and `image.tag` are
mutually exclusive:

Existing-release upgrades in this example require Helm 3.14 or later for
`--reset-then-reuse-values`. Check `helm version --short` before continuing.

```bash
helm version --short
export SERVER_DIGEST="$(jq -er '.serverDigests.index' release-manifest.json)"
export CONTROLLER_DIGEST="$(jq -er '.controllerDigests.index' release-manifest.json)"
helm show crds oci://ghcr.io/weauratech/charts/aura-power --version 2.3.0 > /tmp/aura-power-crds.yaml
kubectl apply --server-side --dry-run=server --field-manager=aura-power-release -f /tmp/aura-power-crds.yaml
kubectl apply --server-side --field-manager=aura-power-release -f /tmp/aura-power-crds.yaml
helm upgrade --install aura-power oci://ghcr.io/weauratech/charts/aura-power \
  --version 2.3.0 --namespace aura-system --create-namespace \
  --reset-then-reuse-values \
  --set-string server.image.tag= \
  --set-string controller.image.tag= \
  --set-string server.image.digest="$SERVER_DIGEST" \
  --set-string controller.image.digest="$CONTROLLER_DIGEST" \
  --dry-run=server
# Repeat the same Helm command without --dry-run=server after reviewing the render.
```

Do not use `--atomic` for a stateful v2.2 migration. If the rollout fails, first
restore the managed workloads with the current controller and follow the safe
downgrade procedure before changing the controller version.

Verify an image signature and its GitHub-hosted provenance before deployment:

```bash
export RELEASE=v2.3.0
export IMAGE=ghcr.io/weauratech/aura-power-controller
export DIGEST=sha256:... # copy the controller digest from the release
export IDENTITY="https://github.com/weauratech/aura-power/.github/workflows/release.yaml@refs/tags/${RELEASE}"

gh release download "$RELEASE" \
  --repo weauratech/aura-power \
  --pattern release-manifest.json \
  --pattern release-manifest.json.bundle
cosign verify-blob \
  --certificate-identity "$IDENTITY" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --bundle release-manifest.json.bundle release-manifest.json

cosign verify \
  --certificate-identity "$IDENTITY" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  "${IMAGE}@${DIGEST}"

gh attestation verify "oci://${IMAGE}@${DIGEST}" \
  --repo weauratech/aura-power
```

Use the same commands with
`ghcr.io/weauratech/charts/aura-power@sha256:...` for the chart. For
downloaded CLI archives, first verify `checksums.txt` with its adjacent Sigstore
bundle, then verify the archive's checksum and GitHub attestation:

```bash
cosign verify-blob \
  --certificate-identity "$IDENTITY" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --bundle checksums.txt.bundle checksums.txt
sha256sum --check checksums.txt --ignore-missing
gh attestation verify ./aura-power_2.3.0_linux_amd64.tar.gz \
  --repo weauratech/aura-power
```

## Repository controls required

The workflow serializes releases globally and only advances `latest` when the
new canonical SemVer is newer than every published stable release. A versioned
image or chart alias may be absent or already point to the candidate digest; a
different digest stops the run before replacement. GitHub API failures also
stop the run instead of being interpreted as an absent release.

The workflow enforces artifact identity, but repository governance remains a
GitHub configuration responsibility. Protect `main`, require the complete CI
suite and independent review, restrict tag creation and deletion to release
maintainers, prevent force pushes, and configure required reviewers on the
`release` environment referenced by the workflow. Keep GitHub Actions restricted to approved pinned
actions. GHCR packages and GitHub Releases should reject mutable replacement of
versioned artifacts through organization policy and maintainer permissions.

GitHub artifact attestations and keyless signing require Actions OIDC and
artifact attestations to be enabled for the repository and organization. A
failed or retried run cannot replace an already public release, draft asset or
versioned OCI alias with a different digest. Chart archives are normalized
before staging so retries of the same tagged source retain the same OCI digest.
Before the last transition, staging references, final digest aliases or a draft
may exist. A retry reuses them only when their identity matches; a rebuild with
a different digest fails closed and requires recovery from the retained run
artifacts. Publishing the GitHub Release remains the final transition.
