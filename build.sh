#!/usr/bin/env bash
# Cross-compile ccx for macOS/Linux/Windows.
# Output: dist/ccx-<os>-<arch>[.exe]
set -euo pipefail

cd "$(dirname "$0")"
mkdir -p dist

# 임베드용 사본을 모듈 루트의 정본과 동기화
cp ccx.config.example.json internal/config/ccx.config.example.json

TARGETS=(
  "darwin/amd64"
  "darwin/arm64"
  "linux/amd64"
  "linux/arm64"
  "windows/amd64"
)

# dev 빌드 식별자 — 정식 릴리즈는 goreleaser가 ldflags로 정확한 값 주입.
# 여기서 "dev"를 박아두면 ccx update가 자동 갱신을 거부한다.
VERSION="dev"
COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
LDFLAGS="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}"

for target in "${TARGETS[@]}"; do
  os="${target%/*}"
  arch="${target#*/}"
  ext=""
  [[ "$os" == "windows" ]] && ext=".exe"
  out="dist/ccx-${os}-${arch}${ext}"

  echo "→ $out"
  GOOS="$os" GOARCH="$arch" \
    go build -ldflags="${LDFLAGS}" -trimpath \
    -o "$out" ./cmd/ccx
done

echo
echo "Done. Binaries in dist/:"
ls -lh dist/ccx-*
