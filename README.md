# claudex

Claude Code를 z.ai GLM, Kimi, DeepSeek, MiniMax, OpenRouter, LM Studio 같은 다른 LLM 프로바이더로 돌려 쓰는 CLI 래퍼.

매번 환경변수를 세팅할 필요 없이, 프로파일을 골라서 `claude`를 실행합니다.

## 설치

**macOS / Linux**

```bash
curl -fsSL https://raw.githubusercontent.com/yobuce/claudex/main/install.sh | bash
```

**Windows (PowerShell)**

```powershell
irm https://raw.githubusercontent.com/yobuce/claudex/main/install.ps1 | iex
```

수동 설치를 원하면 [Releases](https://github.com/yobuce/claudex/releases/latest)에서 OS/아키텍처에 맞는 아카이브를 직접 받으세요.

설치 스크립트가 `PATH`에 자동으로 추가하지만, macOS/Linux에서 `~/.local/bin`이 `PATH`에 없다면 셸 설정에 추가해야 합니다 — 스크립트가 안내 메시지를 출력합니다.

사전 준비: [Claude Code](https://docs.claude.com/en/docs/claude-code) CLI가 설치돼 있어야 합니다.

## 사용법

**1. 설정 파일 만들기**

```bash
cp claudex.config.example.json claudex.config.json
```

**2. 사용할 프로바이더의 API 키 넣기**

`claudex.config.json`을 열어 쓰려는 프로바이더 항목의 `YOUR_..._API_KEY` 자리에 실제 키를 붙여넣습니다. 쓰지 않는 프로바이더는 그대로 둬도 됩니다.

**3. 실행**

```bash
claudex
```

메뉴에서 화살표 키로 프로파일을 고르면 해당 프로바이더로 연결된 `claude`가 뜹니다. 숫자 키로 바로 선택할 수도 있습니다.

```text
  claudex — 프로파일을 선택하세요

   ❯ 1. GLM Coding Plan      z.ai GLM (Anthropic-compatible)
           opus   → GLM-4.7
           sonnet → GLM-4.7
           haiku  → GLM-4.5-Air
     2. Kimi (Moonshot)      Moonshot Kimi K2.5 (Anthropic-compatible)
     3. DeepSeek              DeepSeek V4 (Anthropic-compatible)
     4. MiniMax               MiniMax M2 series (Anthropic-compatible)
     5. OpenRouter            OpenRouter 멀티모델 게이트웨이
     6. LM Studio (local)     로컬 LM Studio 서버
     7. + 새 프로바이더 추가...

    ↑↓ 이동  Enter 선택  e 편집  d 삭제  Esc 취소
```

프로파일을 미리 지정하려면:

```bash
claudex -xSet "GLM Coding Plan" -p "hello"
```

`-xSet` 외의 인자는 전부 `claude`에 그대로 전달됩니다. 예를 들어 권한 확인 프롬프트 없이 실행하려면:

```bash
claudex -xSet "GLM Coding Plan" --dangerously-skip-permissions
```

인터랙티브 메뉴와 함께도 쓸 수 있습니다 (메뉴에서 프로파일 선택 후 해당 옵션이 claude에 전달됨):

```bash
claudex --dangerously-skip-permissions
```

## 지원 프로바이더

z.ai GLM · Kimi (Moonshot) · DeepSeek · MiniMax · OpenRouter · LM Studio (로컬)

기본 설정은 `claudex.config.example.json`에 다 들어 있어 손댈 필요가 없습니다. API 키만 채우면 동작합니다.

## 알아두면 좋은 점

- **ChatGPT Plus / Codex 계정은 쓸 수 없습니다.** OpenAI는 Anthropic 호환 엔드포인트를 제공하지 않습니다.
- **LM Studio(로컬)는 모델에 따라 품질 편차가 큽니다.** 툴 사용이나 긴 컨텍스트를 제대로 지원하지 않는 모델이 많습니다.
- `claudex.config.json`은 `.gitignore`에 포함돼 있어 실수로 커밋되지 않습니다.

## 소스에서 빌드

릴리즈 바이너리 대신 직접 빌드하려면 Go 1.21+ 가 필요합니다.

```bash
git clone https://github.com/yobuce/claudex.git
cd claudex
./build.sh       # dist/claudex-<os>-<arch> 생성
cp dist/claudex-$(uname -s | tr A-Z a-z)-$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/') ~/.local/bin/claudex
chmod +x ~/.local/bin/claudex
```

## License

MIT
