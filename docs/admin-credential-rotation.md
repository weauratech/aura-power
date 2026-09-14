# Administrator credential rotation

Aura Power stores password hashes in SQLite and reads its JWT signing key and
bootstrap administrator password from a Kubernetes Secret. Rotate the password
through the API so the existing user ID and approval history remain intact.
Changing only the Secret does not update an existing SQLite password hash.

Use a maintenance window and a restricted shell. Start with `umask 077`, keep
request bodies, curl configuration and Secret patches in mode `0600` temporary files, source new values from an
approved secret manager, and remove the files when verification finishes.
Never place passwords or JWT keys in command arguments, logs, Helm values, or
evidence bundles.

## Before rotation

1. Verify a current PVC backup or VolumeSnapshot and record the PVC and auth
   Secret UIDs without recording their contents.
2. Confirm the release authority. If Argo CD owns the Helm release, change its
   declared values through that source rather than racing it with Helm.
3. Log in as the primary administrator and create a temporary recovery
   administrator through `POST /api/v1/users`. Verify a separate login and
   `GET /api/v1/auth/me` for both accounts.
4. Save one old access token and one old refresh token only in restricted
   temporary storage for the negative checks below.

Upgrades from a release that emitted untyped tokens intentionally invalidate
all existing sessions. After such an upgrade, log in again before continuing.
The server refuses to open a SQLite database containing orphaned requester or
reviewer references. If readiness reports a database failure, stop and inspect
the backup; do not delete approval rows to make startup succeed.

## Rotate the administrator password

Send the URL, authorization header and request body through a mode `0600` curl
configuration file so neither the password nor bearer token appears in process
arguments:

```bash
umask 077
rotation_body="$(mktemp)"
rotation_config="$(mktemp)"
cleanup_rotation_files() {
  for file in "$rotation_body" "$rotation_config"; do
    if [ -f "$file" ]; then
      : >"$file"
      unlink "$file"
    fi
  done
}
trap cleanup_rotation_files EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
# Write currentPassword/newPassword JSON to $rotation_body from the approved
# secret manager, then write this curl configuration to $rotation_config:
cat >"$rotation_config" <<EOF
url = "${AURA_POWER_URL}/api/v1/auth/password"
request = "PUT"
header = "content-type: application/json"
header = "authorization: Bearer ${CURRENT_ACCESS_TOKEN}"
data-binary = "@${rotation_body}"
EOF
curl --fail-with-body --config "$rotation_config"
```

The response must be `200`. The previous access and refresh tokens must then
return `401`, the old password must fail login, and the new password must log in
successfully. Delete the unused recovery administrator with the new primary
session. Its token becomes invalid immediately because deleted users are
checked against SQLite on every authenticated request.

## Remove credentials from Helm history

The chart-managed Secret is embedded in Helm release manifests. Transfer it to
`server.auth.existingSecret` before rotating its contents. Record its UID and
create the effective-values file with restrictive permissions before Helm
writes credentials into it:

```bash
umask 077
effective_values="$(mktemp)"
cleanup_effective_values() {
  if [ -f "$effective_values" ]; then
    : >"$effective_values"
    unlink "$effective_values"
  fi
}
trap 'cleanup_effective_values; cleanup_rotation_files' EXIT
helm get values aura-power -n aura-system --all >"$effective_values"
yq -i '
  .server.auth.keepManagedSecret = true
' "$effective_values"
```

First review and run a preparatory upgrade with
`--reset-values -f "$effective_values" --history-max 1`. It must keep the managed Secret and
record `helm.sh/resource-policy: keep` in both the live object and Helm release
manifest. This explicit release revision is the deletion guard; do not rely on
patching only the live Secret.

Next sanitize the same restricted values file:

```bash
yq -i '
  .server.auth.existingSecret = "aura-power-server-secret" |
  .server.auth.keepManagedSecret = false |
  .server.auth.jwtSecret = "" |
  .server.auth.initialAdmin.password = null
' "$effective_values"
```

Review `helm upgrade --dry-run=server --hide-secret` and confirm the rendered
StatefulSet references the existing Secret while no auth Secret is rendered.
Run the reviewed externalizing upgrade with
`--reset-values -f "$effective_values" --history-max 1`, verify the Secret UID, and repeat the
same sanitized upgrade once. Helm prunes history before creating a revision,
so this final no-op upgrade removes the preparatory revision that still
contained the Secret. Confirm every remaining revision has sanitized values and
no auth Secret in its manifest. Do not delete Helm storage Secrets manually.

After the transfer succeeds, remove the temporary retention annotation. Patch
`jwt-secret` and `admin-password` in place from a mode `0600` patch file, verify
the Secret UID is unchanged, and explicitly restart the server StatefulSet.
The pod-template checksum reflects Helm values and does not observe a direct
change to an externally managed Secret.

## Acceptance checks

- Server StatefulSet is Ready and the PVC and Secret UIDs are unchanged.
- The new administrator password succeeds; the old password fails.
- Pre-rotation access and refresh tokens return `401`.
- The recovery administrator's token returns `401` after deletion.
- `helm get values` and `helm get manifest` for every retained revision contain
  neither credential values nor a rendered auth Secret.
- API, audit, controller, and managed workloads remain healthy.

Truncate and unlink every temporary request, curl, patch and values file after
the acceptance checks. Storage media may retain deleted blocks, so keep these
files on encrypted local storage while they exist.

Logout clears browser cookies but JWTs are stateless. A copied token remains
valid until expiry unless the user's password or role changes, the account is
deleted, or the global JWT signing key is rotated. Refresh tokens are not
single-use in this release; protect them as long-lived credentials.
