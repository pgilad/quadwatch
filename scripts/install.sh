#!/usr/bin/env sh

set -eu

REPO="${QUADWATCH_INSTALL_REPO:-pgilad/quadwatch}"
BASE_URL="${QUADWATCH_INSTALL_BASE_URL:-https://github.com/${REPO}/releases}"
INSTALL_VERSION="${QUADWATCH_INSTALL_VERSION:-${QUADWATCH_INSTALL_RELEASE_DEFAULT_VERSION:-latest}}"
INSTALL_DIR="${QUADWATCH_INSTALL_DIR:-${HOME:-}/.local/bin}"
TARGET="${QUADWATCH_INSTALL_TARGET:-}"
SUPPORTED_TARGETS="linux-amd64, linux-arm64, darwin-amd64, darwin-arm64"

usage() {
  cat <<'EOF'
Usage:
  install.sh [--version <tag>] [--dir <path>] [--target <os-arch>]

Options:
  --version  Install a specific release tag instead of the latest release
  --dir      Install directory for the `quadwatch` binary (default: ~/.local/bin)
  --target   Override auto-detected target (linux-amd64, linux-arm64, darwin-amd64, darwin-arm64)
  --help     Show this help text

Environment:
  QUADWATCH_INSTALL_VERSION   Same as --version
  QUADWATCH_INSTALL_DIR       Same as --dir
  QUADWATCH_INSTALL_TARGET    Same as --target
  QUADWATCH_INSTALL_BASE_URL  Override the releases base URL
  QUADWATCH_INSTALL_REPO      Override the GitHub repo owner/name
EOF
}

if [ -t 1 ] && [ "${NO_COLOR:-}" = "" ] && [ "${TERM:-}" != "dumb" ]; then
  ESC="$(printf '\033')"
  GREEN="${ESC}[32m"
  YELLOW="${ESC}[33m"
  RED="${ESC}[31m"
  CYAN="${ESC}[36m"
  RESET="${ESC}[0m"
else
  GREEN=""
  YELLOW=""
  RED=""
  CYAN=""
  RESET=""
fi

say() { printf '%s\n' "$*"; }
detail() { printf '  %s\n' "$*"; }
section() { printf '%s %s\n' "${CYAN}==>${RESET}" "$*"; }
success() { printf '%s %s\n' "${GREEN}installed:${RESET}" "$*"; }
warn() { printf '%s %s\n' "${YELLOW}warning:${RESET}" "$*" >&2; }
fail() { printf '%s %s\n' "${RED}error:${RESET}" "$*" >&2; exit 1; }

have_cmd() { command -v "$1" >/dev/null 2>&1; }
need_cmd() { have_cmd "$1" || fail "missing required command: $1"; }

download() {
  url="$1"
  destination="$2"
  if have_cmd curl; then
    curl -fsSL "$url" -o "$destination" || fail "failed to download ${url} with curl"
    return
  fi
  if have_cmd wget; then
    wget -qO "$destination" "$url" || fail "failed to download ${url} with wget"
    return
  fi
  fail "missing required downloader: curl or wget"
}

checksum_file() {
  file_path="$1"
  if have_cmd shasum; then
    shasum -a 256 "$file_path" | awk '{print $1}'
    return
  fi
  if have_cmd sha256sum; then
    sha256sum "$file_path" | awk '{print $1}'
    return
  fi
  fail "missing required checksum tool: shasum or sha256sum"
}

verify_checksum() {
  archive_path="$1"
  archive_name="$2"
  checksums_path="$3"
  actual="$(checksum_file "$archive_path")"
  expected="$(awk -v name="$archive_name" '$2 == name {print $1}' "$checksums_path")"
  [ -n "$expected" ] || fail "checksums.txt did not contain ${archive_name}"
  [ "$actual" = "$expected" ] || fail "checksum mismatch for ${archive_name}"
}

detect_target() {
  case "$(uname -s)" in
    Linux) os="linux" ;;
    Darwin) os="darwin" ;;
    *) fail "unsupported operating system: $(uname -s). Supported targets: ${SUPPORTED_TARGETS}" ;;
  esac
  case "$(uname -m)" in
    x86_64|amd64) arch="amd64" ;;
    aarch64|arm64) arch="arm64" ;;
    *) fail "unsupported architecture: $(uname -m). Supported targets: ${SUPPORTED_TARGETS}" ;;
  esac
  printf '%s-%s\n' "$os" "$arch"
}

release_download_prefix() {
  if [ "$INSTALL_VERSION" = "latest" ]; then
    printf '%s\n' "${BASE_URL}/latest/download"
  else
    printf '%s\n' "${BASE_URL}/download/${INSTALL_VERSION}"
  fi
}

is_dir_on_path() {
  case ":${PATH:-}:" in
    *:"$1":*) return 0 ;;
    *) return 1 ;;
  esac
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --version) [ "$#" -ge 2 ] || fail "--version requires a release tag"; INSTALL_VERSION="$2"; shift 2 ;;
    --dir) [ "$#" -ge 2 ] || fail "--dir requires a path"; INSTALL_DIR="$2"; shift 2 ;;
    --target) [ "$#" -ge 2 ] || fail "--target requires a target"; TARGET="$2"; shift 2 ;;
    --help|-h) usage; exit 0 ;;
    *) fail "unknown option: $1" ;;
  esac
done

[ -n "${HOME:-}" ] || fail "HOME must be set"
[ -n "$INSTALL_DIR" ] || fail "install directory could not be determined"
need_cmd awk
need_cmd chmod
need_cmd cp
need_cmd mkdir
need_cmd mktemp
need_cmd mv
need_cmd rm
need_cmd tar
need_cmd uname

if [ -z "$TARGET" ]; then
  TARGET="$(detect_target)"
fi

download_prefix="$(release_download_prefix)"
archive_name="quadwatch-${TARGET}.tar.gz"
archive_url="${download_prefix}/${archive_name}"
checksums_url="${download_prefix}/checksums.txt"
install_path="${INSTALL_DIR}/quadwatch"

tmpdir="$(mktemp -d "${TMPDIR:-/tmp}/quadwatch-install.XXXXXX")"
install_tmp_path=""
cleanup() {
  if [ -n "$install_tmp_path" ] && [ -e "$install_tmp_path" ]; then
    rm -f "$install_tmp_path"
  fi
  rm -rf "$tmpdir"
}
trap cleanup EXIT INT TERM HUP

archive_path="${tmpdir}/${archive_name}"
checksums_path="${tmpdir}/checksums.txt"

say "quadwatch installer"
detail "version: ${INSTALL_VERSION}"
detail "target: ${TARGET}"
detail "install: ${install_path}"
say ""

section "Downloading quadwatch for ${TARGET}"
detail "from: ${archive_url}"
download "$archive_url" "$archive_path"
download "$checksums_url" "$checksums_path"

section "Verifying checksum"
verify_checksum "$archive_path" "$archive_name" "$checksums_path"

section "Installing quadwatch"
tar -xzf "$archive_path" -C "$tmpdir"
binary_path="${tmpdir}/quadwatch-${TARGET}/quadwatch"
[ -f "$binary_path" ] || fail "downloaded archive did not contain the expected quadwatch-${TARGET}/quadwatch path"

mkdir -p "$INSTALL_DIR"
install_tmp_path="$(mktemp "${INSTALL_DIR}/.quadwatch.tmp.XXXXXX")"
cp "$binary_path" "$install_tmp_path"
chmod 755 "$install_tmp_path"
mv -f "$install_tmp_path" "$install_path"
install_tmp_path=""

success "$install_path"
if is_dir_on_path "$INSTALL_DIR"; then
  detail "Run: quadwatch --version"
else
  warn "${INSTALL_DIR} is not on PATH"
  detail "Run: ${install_path} --version"
fi
