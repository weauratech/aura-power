#!/usr/bin/env bash
set -euo pipefail

chart_dir="${1:?chart directory is required}"
destination="${2:?destination directory is required}"
version="${3:?chart version is required}"
app_version="${4:?app version is required}"

command -v helm >/dev/null || { echo "helm is required" >&2; exit 1; }
command -v tar >/dev/null || { echo "tar is required" >&2; exit 1; }
command -v python3 >/dev/null || { echo "python3 is required" >&2; exit 1; }
[[ "$version" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] || {
  echo "canonical chart version is required" >&2
  exit 1
}

scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
mkdir -p "$scratch/package" "$scratch/root" "$destination"
helm package "$chart_dir" --version "$version" --app-version "$app_version" --destination "$scratch/package" >/dev/null
archive="$scratch/package/$(basename "$chart_dir")-${version}.tgz"
tar -xzf "$archive" -C "$scratch/root"

# Helm's gzip header records wall-clock time. Normalize the complete archive so
# retries of the same commit have the same OCI layer and manifest digest. Python
# provides identical tar semantics on the macOS review host and Ubuntu runner.
python3 - "$scratch/root" "$(basename "$chart_dir")" "$destination/$(basename "$archive")" <<'PY'
import gzip
import pathlib
import sys
import tarfile

root = pathlib.Path(sys.argv[1])
chart_name = sys.argv[2]
destination = pathlib.Path(sys.argv[3])
paths = [root / chart_name, *sorted((root / chart_name).rglob("*"))]

with destination.open("wb") as raw:
    with gzip.GzipFile(filename="", mode="wb", fileobj=raw, mtime=0) as compressed:
        with tarfile.open(fileobj=compressed, mode="w", format=tarfile.USTAR_FORMAT) as archive:
            for path in paths:
                relative = path.relative_to(root).as_posix()
                info = archive.gettarinfo(str(path), arcname=relative)
                info.uid = 0
                info.gid = 0
                info.uname = ""
                info.gname = ""
                info.mtime = 0
                if info.isreg():
                    with path.open("rb") as content:
                        archive.addfile(info, content)
                else:
                    archive.addfile(info)
PY
helm show chart "$destination/$(basename "$archive")" >/dev/null
