#!/usr/bin/env bash
# Cross-compile claudex for macOS/Linux/Windows.
# Output: dist/claudex-<os>-<arch>[.exe]
set -euo pipefail

cd "$(dirname "$0")"
mkdir -p dist

TARGETS=(
  "darwin/amd64"
  "darwin/arm64"
  "linux/amd64"
  "linux/arm64"
  "windows/amd64"
)

for target in "${TARGETS[@]}"; do
  os="${target%/*}"
  arch="${target#*/}"
  ext=""
  [[ "$os" == "windows" ]] && ext=".exe"
  out="dist/claudex-${os}-${arch}${ext}"

  echo "→ $out"
  GOOS="$os" GOARCH="$arch" \
    go build -ldflags="-s -w" -trimpath \
    -o "$out" ./cmd/claudex
done

echo
echo "Done. Binaries in dist/:"
ls -lh dist/claudex-*
