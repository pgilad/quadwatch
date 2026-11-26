#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
test_root="$(mktemp -d "${TMPDIR:-/tmp}/quadwatch-release-test.XXXXXX")"
trap 'rm -rf "${test_root}"' EXIT

source_dir="${test_root}/source"
build_dir="${test_root}/build"
mkdir -p "${source_dir}" "${build_dir}"

tar -C "${repo_root}" \
  --exclude=.git \
  --exclude=coverage.out \
  --exclude=quadwatch \
  --exclude=target \
  -cf - . | tar -C "${source_dir}" -xf -

cd "${source_dir}"
git init -q
git config user.name "quadwatch release test"
git config user.email "release-test@quadwatch.invalid"
git add .
git commit -qm "test: release source"
candidate_sha="$(git rev-parse HEAD)"

release_outputs="${test_root}/release-outputs"
GITHUB_OUTPUT="${release_outputs}" ./scripts/prepare-release.sh 123
release_version="$(sed -n 's/^release_version=//p' "${release_outputs}")"
[[ -n "${release_version}" ]] || { echo "error: prepare-release did not output a version" >&2; exit 1; }

./scripts/render-installer.sh "${release_version}" target/release/metadata/install.sh
grep -Fq "QUADWATCH_INSTALL_RELEASE_DEFAULT_VERSION=${release_version}" target/release/metadata/install.sh

git add CHANGELOG.md
git commit -qm "chore(release): ${release_version}"
release_sha="$(git rev-parse HEAD)"
git tag -a "${release_version}" -m "${release_version}"
git branch candidate-base "${candidate_sha}"
git branch release-candidate "${release_sha}"
git bundle create "${test_root}/release.bundle" \
  refs/heads/release-candidate \
  "refs/tags/${release_version}" \
  "^${candidate_sha}"
git bundle verify "${test_root}/release.bundle"

cd "${build_dir}"
git init -q
git fetch -q "${source_dir}" refs/heads/candidate-base
git checkout -q --detach FETCH_HEAD
git fetch -q "${test_root}/release.bundle" \
  refs/heads/release-candidate \
  "refs/tags/${release_version}:refs/tags/${release_version}"
git checkout -q --detach FETCH_HEAD
[[ "$(git rev-parse HEAD)" == "${release_sha}" ]] || { echo "error: restored release commit does not match ${release_sha}" >&2; exit 1; }
[[ -z "$(git status --porcelain)" ]] || { echo "error: restored release tree is dirty" >&2; exit 1; }

case "$(uname -s)-$(uname -m)" in
  Linux-x86_64|Linux-amd64) target=linux-amd64 ;;
  Linux-aarch64|Linux-arm64) target=linux-arm64 ;;
  Darwin-x86_64|Darwin-amd64) target=darwin-amd64 ;;
  Darwin-arm64) target=darwin-arm64 ;;
  *) echo "error: unsupported release-test host: $(uname -s)-$(uname -m)" >&2; exit 1 ;;
esac

./scripts/package-release.sh "${release_version}" target/release/dist --target "${target}"
./scripts/verify-release.sh "${release_version}" "${target}" target/release/dist "${release_sha}"
./scripts/write-checksums.sh target/release/dist target/release/checksums.txt

release_download_dir="${test_root}/releases/download/${release_version}"
install_dir="${test_root}/bin"
mkdir -p "${release_download_dir}"
cp "target/release/dist/quadwatch-${target}.tar.gz" "${release_download_dir}/"
cp target/release/checksums.txt "${release_download_dir}/checksums.txt"

QUADWATCH_INSTALL_BASE_URL="file://${test_root}/releases" \
QUADWATCH_INSTALL_DIR="${install_dir}" \
QUADWATCH_INSTALL_TARGET="${target}" \
sh "${source_dir}/target/release/metadata/install.sh"

[[ "$("${install_dir}/quadwatch" version)" == "${release_version}" ]] || { echo "error: installed binary has the wrong version" >&2; exit 1; }
publish_dir="${test_root}/publish"
mkdir -p "${publish_dir}/dist" "${publish_dir}/metadata" "${publish_dir}/bin" "${publish_dir}/remote"
cp "${source_dir}/target/release/metadata/install.sh" "${publish_dir}/metadata/"
cp "${source_dir}/target/release/notes.md" "${publish_dir}/metadata/release-notes.md"
if command -v shasum >/dev/null; then
  checksum=(shasum -a 256)
else
  checksum=(sha256sum)
fi
for platform in linux-amd64 linux-arm64 darwin-amd64 darwin-arm64; do
  for archive in "quadwatch-${release_version}-${platform}.tar.gz" "quadwatch-${platform}.tar.gz"; do
    printf '%s\n' "${archive}" > "${publish_dir}/dist/${archive}"
    "${checksum[@]}" "${publish_dir}/dist/${archive}" | awk '{print $1}' > "${publish_dir}/dist/${archive}.sha256"
  done
done
cat > "${publish_dir}/bin/gh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
test "$1" = release
operation="$2"
test "$3" = --repo
test "$5" = "${TEST_RELEASE_VERSION}"
shift 5
state="${TEST_RELEASE_REMOTE}"
case "${operation}" in
  view)
    if [[ ! -f "${state}/draft" ]]; then
      echo 'release not found' >&2
      exit 1
    fi
    cat "${state}/draft"
    for file in "${state}"/assets/*; do
      if [[ -s "${file}" ]]; then basename "${file}"; fi
    done
    ;;
  create)
    test ! -e "${state}/draft"
    printf '%s\n' draft > "${state}/draft"
    mkdir "${state}/assets"
    ;;
  upload)
    for file in "$@"; do
      if [[ "${file}" == --clobber ]]; then continue; fi
      cp "${file}" "${state}/assets/"
      if [[ "${file}" == *.tar.gz && -f "${state}/fail-upload" ]]; then
        rm "${state}/fail-upload"
        exit 1
      fi
    done
    ;;
  download)
    test "$1" = --pattern
    test "$3" = --dir
    cp "${state}/assets/$2" "$4/"
    ;;
  edit)
    test "$1" = --draft=false
    test "$(cat "${state}/draft")" = draft
    printf '%s\n' published > "${state}/draft"
    ;;
  *) exit 1 ;;
esac
EOF
chmod +x "${publish_dir}/bin/gh"
export PATH="${publish_dir}/bin:${PATH}"
export TEST_RELEASE_VERSION="${release_version}" TEST_RELEASE_REMOTE="${publish_dir}/remote"
publisher="${source_dir}/scripts/publish-release.sh"
publish() {
  bash "${publisher}" publish example/quadwatch "${release_version}" "${publish_dir}/dist" "${publish_dir}/metadata"
}
release_status() {
  bash "${publisher}" status example/quadwatch "${release_version}"
}

test "$(release_status)" = incomplete
touch "${TEST_RELEASE_REMOTE}/fail-upload"
if publish; then
  echo "error: release publish passed despite an interrupted upload" >&2
  exit 1
fi
test "$(release_status)" = incomplete
first_archive="quadwatch-${release_version}-linux-amd64.tar.gz"
printf '%s\n' rebuilt > "${publish_dir}/dist/${first_archive}"
"${checksum[@]}" "${publish_dir}/dist/${first_archive}" | awk '{print $1}' > "${publish_dir}/dist/${first_archive}.sha256"
publish
test "$(release_status)" = complete
test "$(cat "${TEST_RELEASE_REMOTE}/assets/${first_archive}")" = "${first_archive}"
(cd "${TEST_RELEASE_REMOTE}/assets" && "${checksum[@]}" -c checksums.txt)
publish

printf 'Release and publication retry smoke tests passed for %s\n' "${target}"
