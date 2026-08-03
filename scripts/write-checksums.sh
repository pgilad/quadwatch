#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  scripts/write-checksums.sh <dist-dir> <output-path>

Build a checksum manifest from the .sha256 files in a release distribution directory.
EOF
}

if [[ $# -ne 2 ]]; then
  usage >&2
  exit 1
fi

dist_dir="$1"
output_path="$2"

[[ -d "${dist_dir}" ]] || { echo "error: distribution directory does not exist: ${dist_dir}" >&2; exit 1; }

mkdir -p "$(dirname "${output_path}")"
manifest_tmp="$(mktemp "${output_path}.tmp.XXXXXX")"
trap 'rm -f "${manifest_tmp}"' EXIT

count=0
while IFS= read -r checksum_path; do
  checksum="$(tr -d '[:space:]' < "${checksum_path}")"
  archive_path="${checksum_path%.sha256}"
  archive_name="$(basename "${archive_path}")"

  [[ "${checksum}" =~ ^[0-9a-fA-F]{64}$ ]] || { echo "error: invalid SHA-256 in ${checksum_path}" >&2; exit 1; }
  [[ -f "${archive_path}" ]] || { echo "error: checksum has no matching archive: ${checksum_path}" >&2; exit 1; }

  printf '%s  %s\n' "${checksum}" "${archive_name}" >> "${manifest_tmp}"
  count=$((count + 1))
done < <(find "${dist_dir}" -type f -name '*.sha256' | LC_ALL=C sort)

[[ "${count}" -gt 0 ]] || { echo "error: no release checksums found in ${dist_dir}" >&2; exit 1; }

mv -f "${manifest_tmp}" "${output_path}"
trap - EXIT
printf 'Wrote %s with %d checksums\n' "${output_path}" "${count}"
