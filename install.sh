#!/usr/bin/env bash
# claudex installer — copies the pre-built Go binary to ~/.local/bin/claudex
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
BIN_DIR="$HOME/.local/bin"
TARGET="$BIN_DIR/claudex"

mkdir -p "$BIN_DIR"

detect_binary() {
  local os arch
  case "$(uname -s)" in
    Darwin)  os=darwin ;;
    Linux)   os=linux ;;
    *)       return 1 ;;
  esac
  case "$(uname -m)" in
    x86_64|amd64)  arch=amd64 ;;
    arm64|aarch64) arch=arm64 ;;
    *)             return 1 ;;
  esac
  local candidate="$SCRIPT_DIR/dist/claudex-${os}-${arch}"
  [[ -x "$candidate" ]] && echo "$candidate" && return 0
  return 1
}

if BIN="$(detect_binary)"; then
  cp "$BIN" "$TARGET"
  chmod +x "$TARGET"
  echo "claudex (Go 바이너리) 설치됨: $TARGET"
else
  echo "Error: dist/claudex-<os>-<arch> 바이너리를 찾을 수 없습니다." >&2
  echo "  먼저 ./build.sh 를 실행하세요." >&2
  exit 1
fi

echo ""
echo "설정 파일 위치:"
echo "  1. \$CLAUDEX_CONFIG"
echo "  2. ~/.config/claudex/claudex.config.json"
echo ""
echo "사용법:"
echo "  claudex                                    # 프로파일 선택 메뉴"
echo "  claudex -xSet 'GLM Coding Plan' -p '안녕'  # 프로파일 직접 지정"
