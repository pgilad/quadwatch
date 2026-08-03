#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  scripts/verify-release.sh <version> <os-arch> <dist-dir> [expected-revision]

Verify release archive layout, checksums, embedded version, and optional Git revision.
EOF
}

if [[ $# -lt 3 || $# -gt 4 ]]; then
  usage >&2
  exit 1
fi

version="$1"
target="$2"
dist_dir="$3"
expected_revision="${4:-}"

case "${target}" in
  linux-amd64|linux-arm64|darwin-amd64|darwin-arm64) ;;
  *) echo "error: unsupported target: ${target}" >&2; exit 1 ;;
esac

checksum_file() {
  local file_path="$1"
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "${file_path}" | awk '{print $1}'
  else
    sha256sum "${file_path}" | awk '{print $1}'
  fi
}

for basename in "quadwatch-${version}-${target}" "quadwatch-${target}"; do
  archive_path="${dist_dir}/${basename}.tar.gz"
  checksum_path="${archive_path}.sha256"

  [[ -f "${archive_path}" ]] || { echo "error: missing archive: ${archive_path}" >&2; exit 1; }
  [[ -f "${checksum_path}" ]] || { echo "error: missing checksum: ${checksum_path}" >&2; exit 1; }

  expected_checksum="$(tr -d '[:space:]' < "${checksum_path}")"
  actual_checksum="$(checksum_file "${archive_path}")"
  [[ "${actual_checksum}" == "${expected_checksum}" ]] || { echo "error: checksum mismatch for ${archive_path}" >&2; exit 1; }

  archive_listing="$(tar -tzf "${archive_path}")"
  for expected_path in "${basename}/quadwatch" "${basename}/README.md" "${basename}/CHANGELOG.md"; do
    grep -Fqx "${expected_path}" <<< "${archive_listing}" || { echo "error: ${archive_path} does not contain ${expected_path}" >&2; exit 1; }
  done
done

staging_dir="$(mktemp -d "${TMPDIR:-/tmp}/quadwatch-verify.XXXXXX")"
trap 'rm -rf "${staging_dir}"' EXIT
stable_basename="quadwatch-${target}"
tar -xzf "${dist_dir}/${stable_basename}.tar.gz" -C "${staging_dir}"
binary_path="${staging_dir}/${stable_basename}/quadwatch"

build_metadata="$(go version -m "${binary_path}")"
if [[ -n "${expected_revision}" ]]; then
  grep -Fq "vcs.revision=${expected_revision}" <<< "${build_metadata}" || { echo "error: binary does not identify release revision ${expected_revision}" >&2; exit 1; }
  grep -Fq 'vcs.modified=false' <<< "${build_metadata}" || { echo "error: binary was built from a modified working tree" >&2; exit 1; }
fi

host_os="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$(uname -m)" in
  x86_64|amd64) host_arch=amd64 ;;
  aarch64|arm64) host_arch=arm64 ;;
  *) host_arch=unsupported ;;
esac

if [[ "${target}" == "${host_os}-${host_arch}" ]]; then
  actual_version="$("${binary_path}" version)"
  [[ "${actual_version}" == "${version}" ]] || { echo "error: binary version is ${actual_version}, expected ${version}" >&2; exit 1; }
fi

printf 'Verified release archives for %s\n' "${target}"
