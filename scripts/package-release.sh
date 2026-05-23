#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  scripts/package-release.sh <version> [output-dir] --target <os-arch>

Arguments:
  <version>     Release version embedded in the binary and archive file names
  [output-dir]  Directory for packaged artifacts (default: target/release/dist)
  --target      Release target: linux-amd64, linux-arm64, darwin-amd64, darwin-arm64
EOF
}

if [[ $# -lt 1 ]]; then
  usage >&2
  exit 1
fi

version=""
output_dir="target/release/dist"
target=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --target)
      if [[ $# -lt 2 ]]; then
        echo "error: --target requires a release target" >&2
        usage >&2
        exit 1
      fi
      target="$2"
      shift 2
      ;;
    -*)
      echo "error: unknown option: $1" >&2
      usage >&2
      exit 1
      ;;
    *)
      if [[ -z "${version}" ]]; then
        version="$1"
      elif [[ "${output_dir}" == "target/release/dist" ]]; then
        output_dir="$1"
      else
        echo "error: unexpected argument: $1" >&2
        usage >&2
        exit 1
      fi
      shift
      ;;
  esac
done

[[ -n "${version}" ]] || { usage >&2; exit 1; }
[[ -n "${target}" ]] || { echo "error: --target is required" >&2; usage >&2; exit 1; }

case "${target}" in
  linux-amd64) goos=linux; goarch=amd64 ;;
  linux-arm64) goos=linux; goarch=arm64 ;;
  darwin-amd64) goos=darwin; goarch=amd64 ;;
  darwin-arm64) goos=darwin; goarch=arm64 ;;
  *) echo "error: unsupported target: ${target}" >&2; exit 1 ;;
esac

mkdir -p "${output_dir}"
output_dir="$(cd "${output_dir}" && pwd)"

staging_dir="$(mktemp -d "${TMPDIR:-/tmp}/quadwatch-release.XXXXXX")"
trap 'rm -rf "${staging_dir}"' EXIT

binary_path="${staging_dir}/quadwatch"
CGO_ENABLED=0 GOOS="${goos}" GOARCH="${goarch}" go build -trimpath -ldflags="-s -w -X main.version=${version}" -o "${binary_path}" .
chmod 755 "${binary_path}"

archive_basename="quadwatch-${version}-${target}"
stable_archive_basename="quadwatch-${target}"

for basename in "${archive_basename}" "${stable_archive_basename}"; do
  package_dir="${staging_dir}/${basename}"
  mkdir -p "${package_dir}"
  cp "${binary_path}" "${package_dir}/quadwatch"
  cp README.md "${package_dir}/README.md"
  if [[ -f CHANGELOG.md ]]; then
    cp CHANGELOG.md "${package_dir}/CHANGELOG.md"
  fi
  tarball_path="${output_dir}/${basename}.tar.gz"
  (
    cd "${staging_dir}"
    tar -czf "${tarball_path}" "${basename}"
  )
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "${tarball_path}" | awk '{print $1}' > "${tarball_path}.sha256"
  else
    sha256sum "${tarball_path}" | awk '{print $1}' > "${tarball_path}.sha256"
  fi
  printf 'Packaged %s\n' "${tarball_path}"
done
