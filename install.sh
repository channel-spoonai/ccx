#!/usr/bin/env bash
# claudex installer — downloads the latest release binary for the current OS/arch.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/yobuce/claudex/main/install.sh | bash
#   curl -fsSL https://raw.githubusercontent.com/yobuce/claudex/main/install.sh | bash -s -- v0.1.0
#
# Env overrides:
#   CLAUDEX_VERSION   설치할 버전 태그 (기본: latest)
#   CLAUDEX_BIN_DIR   설치 경로 (기본: ~/.local/bin)
set -euo pipefail

REPO="yobuce/claudex"
BIN_DIR="${CLAUDEX_BIN_DIR:-$HOME/.local/bin}"
VERSION="${1:-${CLAUDEX_VERSION:-latest}}"

err() { echo "Error: $*" >&2; exit 1; }
info() { echo "→ $*"; }

# OS/arch 감지
case "$(uname -s)" in
  Darwin) OS=darwin ;;
  Linux)  OS=linux ;;
  *)      err "지원하지 않는 OS: $(uname -s) (Windows는 install.ps1 사용)" ;;
esac

case "$(uname -m)" in
  x86_64|amd64)   ARCH=amd64 ;;
  arm64|aarch64)  ARCH=arm64 ;;
  *)              err "지원하지 않는 아키텍처: $(uname -m)" ;;
esac

# 다운로드 도구 선택
if command -v curl >/dev/null 2>&1; then
  DL() { curl -fsSL "$1"; }
  DL_OUT() { curl -fsSL -o "$2" "$1"; }
elif command -v wget >/dev/null 2>&1; then
  DL() { wget -qO- "$1"; }
  DL_OUT() { wget -qO "$2" "$1"; }
else
  err "curl 또는 wget이 필요합니다."
fi

# 최신 버전 조회
if [[ "$VERSION" == "latest" ]]; then
  info "최신 릴리즈 조회 중..."
  VERSION=$(DL "https://api.github.com/repos/$REPO/releases/latest" \
    | grep -o '"tag_name"[[:space:]]*:[[:space:]]*"[^"]*"' \
    | head -n1 \
    | sed -E 's/.*"([^"]+)"$/\1/')
  [[ -n "$VERSION" ]] || err "최신 릴리즈 태그를 가져올 수 없습니다."
fi

# 태그에서 v 접두사 제거 (아카이브 이름은 v 없는 형태)
VER_NUM="${VERSION#v}"
ARCHIVE="claudex-${VER_NUM}-${OS}-${ARCH}.tar.gz"
URL="https://github.com/$REPO/releases/download/${VERSION}/${ARCHIVE}"

info "다운로드: $URL"
TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT

DL_OUT "$URL" "$TMP_DIR/$ARCHIVE" || err "다운로드 실패. 버전/아키텍처를 확인하세요: $URL"

info "압축 해제"
tar -xzf "$TMP_DIR/$ARCHIVE" -C "$TMP_DIR"

# 바이너리 위치 (아카이브 루트 또는 하위 디렉터리)
BIN_SRC=""
for cand in "$TMP_DIR/claudex" "$TMP_DIR/claudex-${VER_NUM}-${OS}-${ARCH}/claudex"; do
  [[ -f "$cand" ]] && BIN_SRC="$cand" && break
done
[[ -n "$BIN_SRC" ]] || BIN_SRC=$(find "$TMP_DIR" -name claudex -type f | head -n1)
[[ -n "$BIN_SRC" ]] || err "아카이브에서 claudex 바이너리를 찾을 수 없습니다."

mkdir -p "$BIN_DIR"
TARGET="$BIN_DIR/claudex"
cp "$BIN_SRC" "$TARGET"
chmod +x "$TARGET"

# macOS quarantine 속성 제거 (Gatekeeper 경고 방지)
if [[ "$OS" == "darwin" ]] && command -v xattr >/dev/null 2>&1; then
  xattr -d com.apple.quarantine "$TARGET" 2>/dev/null || true
fi

echo ""
echo "✓ claudex $VERSION 설치 완료: $TARGET"

# PATH 안내
case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *)
    echo ""
    echo "⚠ $BIN_DIR 가 PATH에 없습니다. 셸 설정에 추가하세요:"
    echo "    export PATH=\"$BIN_DIR:\$PATH\""
    ;;
esac

echo ""
echo "설정 파일 위치:"
echo "  1. \$CLAUDEX_CONFIG"
echo "  2. ~/.config/claudex/claudex.config.json"
echo ""
echo "예제 설정: https://github.com/$REPO/blob/main/claudex.config.example.json"
echo ""
echo "사용법:"
echo "  claudex                                    # 프로파일 선택 메뉴"
echo "  claudex -xSet 'GLM Coding Plan' -p '안녕'  # 프로파일 직접 지정"
