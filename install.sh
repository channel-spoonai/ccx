#!/usr/bin/env bash
# ccx installer — downloads the latest release binary for the current OS/arch.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/channel-spoonai/ccx/main/install.sh | bash
#   curl -fsSL https://raw.githubusercontent.com/channel-spoonai/ccx/main/install.sh | bash -s -- v0.1.0
#
# Env overrides:
#   CCX_VERSION   version tag to install (default: latest)
#   CCX_BIN_DIR   install path (default: ~/.local/bin)
set -euo pipefail

REPO="channel-spoonai/ccx"
BIN_DIR="${CCX_BIN_DIR:-$HOME/.local/bin}"
VERSION="${1:-${CCX_VERSION:-latest}}"

err() { echo "Error: $*" >&2; exit 1; }
info() { echo "→ $*"; }

# Detect OS/arch
case "$(uname -s)" in
  Darwin) OS=darwin ;;
  Linux)  OS=linux ;;
  *)      err "unsupported OS: $(uname -s) (use install.ps1 on Windows)" ;;
esac

case "$(uname -m)" in
  x86_64|amd64)   ARCH=amd64 ;;
  arm64|aarch64)  ARCH=arm64 ;;
  *)              err "unsupported architecture: $(uname -m)" ;;
esac

# Pick a download tool
if command -v curl >/dev/null 2>&1; then
  DL() { curl -fsSL "$1"; }
  DL_OUT() { curl -fsSL -o "$2" "$1"; }
elif command -v wget >/dev/null 2>&1; then
  DL() { wget -qO- "$1"; }
  DL_OUT() { wget -qO "$2" "$1"; }
else
  err "curl or wget is required."
fi

# Resolve latest version
if [[ "$VERSION" == "latest" ]]; then
  info "fetching latest release..."
  VERSION=$(DL "https://api.github.com/repos/$REPO/releases/latest" \
    | grep -o '"tag_name"[[:space:]]*:[[:space:]]*"[^"]*"' \
    | head -n1 \
    | sed -E 's/.*"([^"]+)"$/\1/')
  [[ -n "$VERSION" ]] || err "could not resolve latest release tag."
fi

# Archive names use the version without the leading "v"
VER_NUM="${VERSION#v}"
ARCHIVE="ccx-${VER_NUM}-${OS}-${ARCH}.tar.gz"
URL="https://github.com/$REPO/releases/download/${VERSION}/${ARCHIVE}"

info "downloading: $URL"
TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT

DL_OUT "$URL" "$TMP_DIR/$ARCHIVE" || err "download failed. check version/arch: $URL"

info "extracting"
tar -xzf "$TMP_DIR/$ARCHIVE" -C "$TMP_DIR"

# Binary location (archive root or subdirectory)
BIN_SRC=""
for cand in "$TMP_DIR/ccx" "$TMP_DIR/ccx-${VER_NUM}-${OS}-${ARCH}/ccx"; do
  [[ -f "$cand" ]] && BIN_SRC="$cand" && break
done
[[ -n "$BIN_SRC" ]] || BIN_SRC=$(find "$TMP_DIR" -name ccx -type f | head -n1)
[[ -n "$BIN_SRC" ]] || err "could not find ccx binary in archive."

mkdir -p "$BIN_DIR"
TARGET="$BIN_DIR/ccx"
cp "$BIN_SRC" "$TARGET"
chmod +x "$TARGET"

# Remove macOS quarantine attribute (avoids Gatekeeper warnings)
if [[ "$OS" == "darwin" ]] && command -v xattr >/dev/null 2>&1; then
  xattr -d com.apple.quarantine "$TARGET" 2>/dev/null || true
fi

echo ""
echo "✓ installed ccx $VERSION at: $TARGET"

# PATH hint
case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *)
    echo ""
    echo "⚠ $BIN_DIR is not on PATH. Add it to your shell config:"
    echo "    export PATH=\"$BIN_DIR:\$PATH\""
    ;;
esac

echo ""
echo "Config file lookup:"
echo "  1. \$CCX_CONFIG"
echo "  2. ~/.config/ccx/ccx.config.json"
echo ""
echo "Example config: https://github.com/$REPO/blob/main/ccx.config.example.json"
echo ""
echo "Usage:"
echo "  ccx                                    # interactive profile menu"
echo "  ccx -xSet 'GLM Coding Plan' -p 'hi'    # run with a specific profile"
