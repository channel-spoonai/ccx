#!/usr/bin/env bash
# claudex installer
# - Go 바이너리가 있으면 그걸 ~/.local/bin/claudex로 복사
# - 없으면 Node shim으로 폴백 (claude.mjs 필요)
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
elif [[ -f "$SCRIPT_DIR/claudex.mjs" ]] && command -v node >/dev/null 2>&1; then
  cat > "$TARGET" <<SHIM
#!/usr/bin/env bash
exec node "${SCRIPT_DIR}/claudex.mjs" "\$@"
SHIM
  chmod +x "$TARGET"
  echo "claudex (Node shim) 설치됨: $TARGET"
  echo "  (Go 바이너리가 없거나 플랫폼 미지원 → Node 폴백)"
else
  echo "Error: dist/claudex-<os>-<arch> 바이너리도 없고 Node도 없습니다." >&2
  echo "  먼저 ./build.sh 를 실행하거나 Node.js를 설치하세요." >&2
  exit 1
fi

echo ""
echo "설정 파일 위치 (첫 번째로 발견되는 것 사용):"
echo "  1. \$CLAUDEX_CONFIG"
echo "  2. ~/.config/claudex/claudex.config.json"
echo "  3. ./claudex.config.json (현재 디렉토리)"
echo "  4. 바이너리 디렉토리"
echo ""
echo "사용법:"
echo "  claudex                                    # 프로파일 선택 메뉴"
echo "  claudex -xSet 'GLM Coding Plan' -p '안녕'  # 프로파일 직접 지정"
