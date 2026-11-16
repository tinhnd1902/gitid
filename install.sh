#!/bin/sh
# gitid installer: downloads a release archive from GitHub, verifies its
# sha256 against the release's checksums.txt and installs the gitid binary.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/tinhnd1902/gitid/main/install.sh | sh
#
# Inspect it first if you prefer:
#   curl -fsSLo install.sh https://raw.githubusercontent.com/tinhnd1902/gitid/main/install.sh
#   less install.sh && sh install.sh
#
# Environment variables (all optional):
#   VERSION         release to install, "0.1.0" or "v0.1.0" (default: latest)
#   INSTALL_DIR     target directory; it must be writable (default: /usr/local/bin
#                   when writable, otherwise $HOME/.local/bin)
#   NO_MODIFY_PATH  set to 1 to never touch shell startup files; the installer
#                   then prints the line to add yourself
#
# What it does to your machine: writes one file (INSTALL_DIR/gitid) and, when
# that directory is not on PATH yet, appends ONE line to ONE shell startup file,
# marked "# added by gitid installer". It never uses sudo, has no telemetry and
# talks only to GitHub (the release host).
#
# Uninstall: remove the gitid binary and that marked line (see the README).
#
# POSIX sh only. Everything lives in functions and the last line calls main, so
# a truncated download of this script cannot run half of it.

set -eu

# Internal overrides, used by the test suite only.
GITID_BASE_URL="${GITID_BASE_URL:-https://github.com/tinhnd1902/gitid/releases}"
GITID_SYSTEM_BIN_DIR="${GITID_SYSTEM_BIN_DIR:-/usr/local/bin}"

MARKER="# added by gitid installer"

# state shared between functions (POSIX sh has no portable `local`)
G_TMP=""
G_TMP_TARGET=""
G_MADE=""
G_OS=""
G_ARCH=""
G_VER=""
G_DIR=""
G_RC=""
G_LINE=""

say() {
  printf '%s\n' "$*"
}

warn() {
  printf 'gitid-install: warning: %s\n' "$*" >&2
}

err() {
  printf 'gitid-install: error: %s\n' "$*" >&2
  exit 1
}

have() {
  command -v "$1" >/dev/null 2>&1
}

cleanup() {
  if [ -n "$G_TMP_TARGET" ]; then
    rm -f "$G_TMP_TARGET" 2>/dev/null || true
  fi
  if [ -n "$G_TMP" ]; then
    rm -rf "$G_TMP" 2>/dev/null || true
  fi
  # Directories this run created (deepest first) that are still empty: remove
  # them again, so a failed install leaves nothing behind.
  if [ -n "$G_MADE" ]; then
    printf '%s' "$G_MADE" | while IFS= read -r made_dir; do
      [ -n "$made_dir" ] && rmdir "$made_dir" 2>/dev/null || true
    done
  fi
}

detect_platform() {
  uname_s="$(uname -s)"
  uname_m="$(uname -m)"
  case "$uname_s" in
    Darwin) G_OS="darwin" ;;
    Linux) G_OS="linux" ;;
    *) err "unsupported operating system: $uname_s (supported: macOS, Linux)" ;;
  esac
  case "$uname_m" in
    x86_64 | amd64) G_ARCH="amd64" ;;
    arm64 | aarch64) G_ARCH="arm64" ;;
    *) err "unsupported architecture: $uname_m (supported: amd64, arm64)" ;;
  esac
  # An x86_64 shell running under Rosetta on Apple silicon: use the native build.
  if [ "$G_OS" = "darwin" ] && [ "$G_ARCH" = "amd64" ]; then
    translated="$(sysctl -n sysctl.proc_translated 2>/dev/null || true)"
    if [ "$translated" = "1" ]; then
      G_ARCH="arm64"
    fi
  fi
}

# curl wrapper: require TLS 1.2+ and https-only (also across redirects) when
# the base URL is https.
http_curl() {
  case "$GITID_BASE_URL" in
    https://*) curl --proto '=https' --tlsv1.2 "$@" ;;
    *) curl "$@" ;;
  esac
}

# download URL DEST
download() {
  if have curl; then
    http_curl -fsSL --retry 2 -o "$2" "$1"
  elif have wget; then
    wget -q -O "$2" "$1"
  else
    err "need curl or wget to download files"
  fi
}

# Prints the final URL that $GITID_BASE_URL/latest redirects to.
latest_url() {
  if have curl; then
    http_curl -fsSLI -o /dev/null -w '%{url_effective}' "$GITID_BASE_URL/latest"
  elif have wget; then
    wget -S --spider --max-redirect=0 "$GITID_BASE_URL/latest" 2>&1 |
      awk 'tolower($1) == "location:" { sub(/\r$/, "", $2); print $2; exit }' || true
  else
    err "need curl or wget to download files"
  fi
}

# Checks a release string like 0.1.0 or 0.2.0-rc.1 (no leading "v").
valid_version() {
  case "$1" in
    "" | *[!A-Za-z0-9._+-]*) return 1 ;;
    [0-9]*.[0-9]*.[0-9]*) return 0 ;;
    *) return 1 ;;
  esac
}

resolve_version() {
  want="${VERSION:-}"
  if [ -n "$want" ]; then
    want="${want#v}"
    valid_version "$want" || err "invalid VERSION '$want' (expected something like 0.1.0)"
    G_VER="$want"
    return 0
  fi
  url="$(latest_url)" || url=""
  case "$url" in
    */tag/*) tag="${url##*/tag/}" ;;
    *) tag="" ;;
  esac
  tag="${tag#v}"
  if ! valid_version "$tag"; then
    err "could not determine the latest release from $GITID_BASE_URL/latest; set VERSION, for example VERSION=0.1.0"
  fi
  G_VER="$tag"
}

choose_install_dir() {
  if [ -n "${INSTALL_DIR:-}" ]; then
    G_DIR="$INSTALL_DIR"
  elif [ -d "$GITID_SYSTEM_BIN_DIR" ] && [ -w "$GITID_SYSTEM_BIN_DIR" ]; then
    G_DIR="$GITID_SYSTEM_BIN_DIR"
  else
    [ -n "${HOME:-}" ] || err "HOME is not set; set INSTALL_DIR to the target directory"
    G_DIR="$HOME/.local/bin"
  fi
  case "$G_DIR" in
    /*) ;;
    *) G_DIR="$(pwd)/$G_DIR" ;;
  esac
  while [ "$G_DIR" != "/" ] && [ "${G_DIR%/}" != "$G_DIR" ]; do
    G_DIR="${G_DIR%/}"
  done
  # Do not create anything yet (the install can still fail): only check that the
  # nearest existing directory is writable. make_install_dir creates it later.
  probe="$G_DIR"
  while [ ! -d "$probe" ] && [ "$probe" != "/" ] && [ -n "$probe" ]; do
    probe="${probe%/*}"
  done
  [ -n "$probe" ] || probe="/"
  [ -w "$probe" ] || err "cannot write to $probe (permission denied); set INSTALL_DIR to a writable directory"
}

# Creates the install directory, remembering which levels this run created.
make_install_dir() {
  [ -d "$G_DIR" ] && return 0
  made_p="$G_DIR"
  while [ ! -d "$made_p" ] && [ "$made_p" != "/" ] && [ -n "$made_p" ]; do
    G_MADE="$G_MADE$made_p
"
    made_p="${made_p%/*}"
  done
  mkdir -p "$G_DIR" 2>/dev/null || err "cannot create $G_DIR (permission denied?); set INSTALL_DIR to a writable directory"
}

sha256_of() {
  if have sha256sum; then
    sha256sum "$1" | awk '{ print $1 }'
  elif have shasum; then
    shasum -a 256 "$1" | awk '{ print $1 }'
  else
    err "need sha256sum or shasum to verify the download (refusing to install unverified)"
  fi
}

verify_checksum() {
  # $1 archive file, $2 archive name, $3 checksums.txt
  expected="$(awk -v n="$2" '$2 == n { print $1; exit }' "$3")"
  [ -n "$expected" ] || err "no checksum entry for $2 in checksums.txt"
  actual="$(sha256_of "$1")"
  [ -n "$actual" ] || err "could not compute the sha256 of $2"
  expected="$(printf '%s' "$expected" | tr 'A-F' 'a-f')"
  actual="$(printf '%s' "$actual" | tr 'A-F' 'a-f')"
  if [ "$expected" != "$actual" ]; then
    err "checksum mismatch for $2 (expected $expected, got $actual); nothing was installed"
  fi
}

install_binary() {
  name="gitid_${G_VER}_${G_OS}_${G_ARCH}.tar.gz"
  rel="$GITID_BASE_URL/download/v${G_VER}"

  say "Installing gitid ${G_VER} (${G_OS}/${G_ARCH})"

  G_TMP="$(mktemp -d "${TMPDIR:-/tmp}/gitid-install.XXXXXX")" || err "could not create a temporary directory"

  download "$rel/$name" "$G_TMP/$name" ||
    err "download failed: $rel/$name (does release v${G_VER} exist for ${G_OS}/${G_ARCH}?)"
  download "$rel/checksums.txt" "$G_TMP/checksums.txt" ||
    err "download failed: $rel/checksums.txt"

  verify_checksum "$G_TMP/$name" "$name" "$G_TMP/checksums.txt"

  mkdir "$G_TMP/x"
  tar -xzf "$G_TMP/$name" -C "$G_TMP/x" || err "could not extract $name"
  if [ ! -f "$G_TMP/x/gitid" ] || [ -L "$G_TMP/x/gitid" ]; then
    err "the archive does not contain a gitid binary"
  fi

  make_install_dir
  [ -w "$G_DIR" ] || err "cannot write to $G_DIR (permission denied); set INSTALL_DIR to a writable directory"
  G_TMP_TARGET="$G_DIR/.gitid.tmp.$$"
  cp "$G_TMP/x/gitid" "$G_TMP_TARGET" || err "cannot write to $G_DIR (permission denied); set INSTALL_DIR to a writable directory"
  chmod 755 "$G_TMP_TARGET"
  if [ "$G_OS" = "darwin" ]; then
    xattr -d com.apple.quarantine "$G_TMP_TARGET" 2>/dev/null || true
  fi
  mv -f "$G_TMP_TARGET" "$G_DIR/gitid" || err "could not move the binary into $G_DIR"
  G_TMP_TARGET=""
}

# Sets G_RC (startup file) and G_LINE (line to put in it) for the current shell.
pick_rc() {
  shell_name="${SHELL:-}"
  shell_name="${shell_name##*/}"
  case "$G_DIR" in
    *'"'* | *'$'* | *'`'* | *\\*) G_RC=""; G_LINE=""; return 1 ;;
  esac
  shown="$G_DIR"
  if [ -n "${HOME:-}" ]; then
    case "$G_DIR" in
      "$HOME"/*) shown="\$HOME/${G_DIR#"$HOME"/}" ;;
    esac
  fi
  case "$shell_name" in
    fish)
      G_RC="${XDG_CONFIG_HOME:-${HOME:-}/.config}/fish/conf.d/gitid.fish"
      G_LINE="fish_add_path -g \"$shown\"  $MARKER"
      ;;
    zsh)
      G_RC="${ZDOTDIR:-${HOME:-}}/.zshrc"
      G_LINE="export PATH=\"$shown:\$PATH\"  $MARKER"
      ;;
    bash)
      if [ "$G_OS" = "darwin" ]; then
        G_RC="${HOME:-}/.bash_profile"
      else
        G_RC="${HOME:-}/.bashrc"
      fi
      G_LINE="export PATH=\"$shown:\$PATH\"  $MARKER"
      ;;
    *)
      G_RC="${HOME:-}/.profile"
      G_LINE="export PATH=\"$shown:\$PATH\"  $MARKER"
      ;;
  esac
  case "$G_RC" in
    /*) ;;
    *) G_RC=""; G_LINE=""; return 1 ;;
  esac
  return 0
}

setup_path() {
  case ":$PATH:" in
    *":$G_DIR:"*) return 0 ;;
  esac

  if ! pick_rc; then
    warn "$G_DIR is not on your PATH and the right startup file could not be determined."
    say "Add $G_DIR to your PATH to use gitid."
    return 0
  fi

  if [ "${NO_MODIFY_PATH:-}" = "1" ]; then
    say "$G_DIR is not on your PATH. Add this line to $G_RC to use gitid:"
    say "  $G_LINE"
    return 0
  fi

  if [ -f "$G_RC" ] && grep -Fxq -- "$G_LINE" "$G_RC"; then
    say "$G_DIR is already configured in $G_RC."
    return 0
  fi

  mkdir -p "$(dirname "$G_RC")" || err "cannot create the directory for $G_RC; set NO_MODIFY_PATH=1 to skip PATH setup"
  if [ -s "$G_RC" ] && [ -n "$(tail -c 1 "$G_RC")" ]; then
    printf '\n' >>"$G_RC" || err "cannot write to $G_RC; set NO_MODIFY_PATH=1 to skip PATH setup"
  fi
  printf '%s\n' "$G_LINE" >>"$G_RC" || err "cannot write to $G_RC; set NO_MODIFY_PATH=1 to skip PATH setup"

  say "Added $G_DIR to your PATH in $G_RC:"
  say "  $G_LINE"
  case "$G_RC" in
    *.fish) say "Open a new terminal (or run: source $G_RC) to use gitid." ;;
    *) say "Open a new terminal (or run: . $G_RC) to use gitid." ;;
  esac
}

main() {
  trap cleanup EXIT
  trap 'exit 130' INT
  trap 'exit 143' TERM
  trap 'exit 129' HUP

  detect_platform
  resolve_version
  choose_install_dir
  install_binary
  G_MADE=""

  if ! out="$("$G_DIR/gitid" version 2>&1)"; then
    err "the installed binary at $G_DIR/gitid failed to run: $out"
  fi
  say "Installed $out to $G_DIR/gitid"

  found="$(command -v gitid 2>/dev/null || true)"
  if [ -n "$found" ] && [ "$found" != "$G_DIR/gitid" ]; then
    warn "another gitid is first on your PATH: $found (the new one is $G_DIR/gitid)"
  fi

  setup_path
}

main "$@"
