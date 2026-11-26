#!/usr/bin/env bash

set -euo pipefail

command="$1"
repo="$2"
version="$3"

archives=()
for target in linux-amd64 linux-arm64 darwin-amd64 darwin-arm64; do
  archives+=("quadwatch-${version}-${target}.tar.gz" "quadwatch-${target}.tar.gz")
done
expected_assets=("${archives[@]}" checksums.txt install.sh)

read_release() {
  local errors
  errors="$(mktemp)"
  if release="$(gh release view --repo "${repo}" "${version}" --json isDraft,assets \
    --jq '(if .isDraft then "draft" else "published" end), (.assets[] | select(.state == "uploaded" and .size > 0) | .name)' 2>"${errors}")"; then
    status="${release%%$'\n'*}"
  elif grep -Fxq 'release not found' "${errors}"; then
    release=""
    status=missing
  else
    cat "${errors}" >&2
    rm -f "${errors}"
    return 1
  fi
  rm -f "${errors}"
}

has_asset() {
  grep -Fxq -- "$1" <<< "${release}"
}

is_complete() {
  [[ "${status}" == published ]] || return 1
  local asset
  for asset in "${expected_assets[@]}"; do
    has_asset "${asset}" || return 1
  done
}

read_release
case "${command}" in
  status)
    if is_complete; then
      printf '%s\n' complete
    else
      printf '%s\n' incomplete
    fi
    exit 0
    ;;
  publish) ;;
  *) printf 'error: unknown command %s\n' "${command}" >&2; exit 1 ;;
esac

if is_complete; then
  exit 0
fi

dist="$4"
metadata="$5"
for asset in "${archives[@]}"; do
  test -s "${dist}/${asset}"
done
test -s "${metadata}/install.sh"

if [[ "${status}" == missing ]]; then
  gh release create --repo "${repo}" "${version}" --verify-tag --draft \
    --title "${version}" --notes-file "${metadata}/release-notes.md"
  status=draft
fi

uploads=()
for asset in "${archives[@]}"; do
  if has_asset "${asset}"; then
    # Rebuilt archives can differ from earlier uploads. Compute checksums for the
    # archives that will be served by GitHub after this retry.
    gh release download --repo "${repo}" "${version}" --pattern "${asset}" \
      --dir "${dist}" --clobber
    if command -v shasum >/dev/null; then
      shasum -a 256 "${dist}/${asset}" | awk '{print $1}' > "${dist}/${asset}.sha256"
    else
      sha256sum "${dist}/${asset}" | awk '{print $1}' > "${dist}/${asset}.sha256"
    fi
  else
    uploads+=("${dist}/${asset}")
  fi
done
if ! has_asset install.sh; then
  uploads+=("${metadata}/install.sh")
fi

"$(dirname "$0")/write-checksums.sh" "${dist}" "${dist}/checksums.txt"
gh release upload --repo "${repo}" "${version}" "${dist}/checksums.txt" --clobber
if (( ${#uploads[@]} > 0 )); then
  gh release upload --repo "${repo}" "${version}" "${uploads[@]}" --clobber
fi
if [[ "${status}" == draft ]]; then
  gh release edit --repo "${repo}" "${version}" --draft=false
fi

read_release
if ! is_complete; then
  printf 'error: release %s is missing published assets\n' "${version}" >&2
  exit 1
fi
