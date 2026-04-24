#!/bin/bash
# claudex installer - creates a shim in ~/.local/bin/

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SHIM_PATH="$HOME/.local/bin/claudex"

# ~/.local/bin 디렉토리 확인
mkdir -p "$HOME/.local/bin"

cat > "$SHIM_PATH" << SHIM
#!/bin/bash
exec node "${SCRIPT_DIR}/claudex.mjs" "\$@"
SHIM

chmod +x "$SHIM_PATH"

echo "claudex가 설치되었습니다: $SHIM_PATH"
echo ""
echo "사용법:"
echo "  claudex                                    # 프로파일 선택 메뉴"
echo "  claudex -xSet 'GLM Coding Plan' -p '안녕'  # 프로파일 직접 지정"
