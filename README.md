# ccx

Claude Code를 z.ai GLM, Kimi, DeepSeek, MiniMax, OpenRouter, LM Studio, **ChatGPT(Codex 구독)** 같은 다른 LLM 프로바이더로 돌려 쓰는 CLI 래퍼.

매번 환경변수를 세팅할 필요 없이, 프로파일을 골라서 `claude`를 실행합니다.

## 설치

**macOS / Linux**

```bash
curl -fsSL https://raw.githubusercontent.com/channel-spoonai/ccx/main/install.sh | bash
```

**Windows (PowerShell)**

```powershell
irm https://raw.githubusercontent.com/channel-spoonai/ccx/main/install.ps1 | iex
```

수동 설치를 원하면 [Releases](https://github.com/channel-spoonai/ccx/releases/latest)에서 OS/아키텍처에 맞는 아카이브를 직접 받으세요.

설치 스크립트가 `PATH`에 자동으로 추가하지만, macOS/Linux에서 `~/.local/bin`이 `PATH`에 없다면 셸 설정에 추가해야 합니다 — 스크립트가 안내 메시지를 출력합니다.

사전 준비: [Claude Code](https://docs.claude.com/en/docs/claude-code) CLI가 설치돼 있어야 합니다.

## 사용법

```bash
ccx
```

처음 실행하면 빈 메뉴가 뜹니다. **+ 새 프로바이더 추가**를 골라 카탈로그에서 프로바이더를 선택하고 API 키만 입력하면 `~/.config/ccx/ccx.config.json` 에 자동 저장됩니다. 그 다음부터는 메뉴에서 화살표 키로 프로파일을 고르면 해당 프로바이더로 연결된 `claude` 가 뜹니다. 숫자 키로 바로 선택할 수도 있습니다.

```text
  ccx — 프로파일을 선택하세요

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
ccx -xSet "GLM Coding Plan" -p "hello"
```

`-xSet` 외의 인자는 전부 `claude`에 그대로 전달됩니다. 예를 들어 권한 확인 프롬프트 없이 실행하려면:

```bash
ccx -xSet "GLM Coding Plan" --dangerously-skip-permissions
```

인터랙티브 메뉴와 함께도 쓸 수 있습니다 (메뉴에서 프로파일 선택 후 해당 옵션이 claude에 전달됨):

```bash
ccx --dangerously-skip-permissions
```

### 비대화형 한 줄 실행 (LM Studio 등 로컬 모델 활용)

`-p`(claude의 print 모드)를 쓰면 응답을 받고 즉시 종료합니다. `--model`로 모델을 오버라이드할 수도 있어서, LM Studio에 로드한 임의의 로컬 모델을 명령어 한 줄로 호출하기 좋습니다.

```bash
# 프로파일에 지정된 모델 그대로 쓰기
ccx -xSet "LM Studio (local)" -p "Go 채널 사용 예시 3줄로 요약해줘"

# 모델만 바꿔서 호출 (LM Studio에 로드해둔 식별자 사용)
ccx -xSet "LM Studio (local)" --model "qwen/qwen3-coder-30b" -p "이 함수 리팩토링 아이디어"

# 셸 파이프와 조합 — 스크립트에서 LLM을 부르듯 사용
cat src/main.go | ccx -xSet "LM Studio (local)" -p "버그 가능성 짚어줘"
```

같은 패턴이 모든 프로바이더에서 동작합니다 (예: `ccx -xSet "GLM Coding Plan" --model "GLM-4.7" -p "..."`). `-xSet` 외의 인자는 전부 그대로 `claude`에 전달되므로 [Claude Code CLI 레퍼런스](https://docs.claude.com/en/docs/claude-code/cli-reference)의 옵션을 모두 사용할 수 있습니다.

## 지원 프로바이더

z.ai GLM · Kimi (Moonshot) · DeepSeek · MiniMax · OpenRouter · LM Studio (로컬) · ChatGPT (Codex)

기본 설정은 바이너리에 카탈로그로 임베드되어 있어 손댈 필요가 없습니다. 메뉴에서 추가하고 API 키만 입력하면 동작합니다.

### ChatGPT (Codex) 구독으로 사용하기

ChatGPT Plus/Pro/Business 구독을 OAuth로 인증해 그 빌링으로 Claude Code를 돌립니다. 별도 프록시 설치 없이 ccx에 내장.

```bash
ccx codex login                  # 브라우저가 열려 ChatGPT 계정으로 인증
# 또는: ccx codex login --device  (헤드리스/SSH 환경)
ccx -xSet "ChatGPT (Codex)" -p "안녕"
```

ccx가 로컬 프록시(랜덤 포트)를 띄워 Anthropic Messages 요청을 OpenAI Responses API로 변환·forwarding합니다. 토큰은 `~/.config/ccx/auth/codex.json` (mode 0600)에 저장되고 만료 5분 전 자동 refresh.

상태/로그아웃: `ccx codex status` / `ccx codex logout`

한계: reasoning 콘텐츠와 tool_result 내 이미지는 변환 과정에서 손실됩니다.

## 알아두면 좋은 점

- **LM Studio(로컬)는 모델에 따라 품질 편차가 큽니다.** 툴 사용이나 긴 컨텍스트를 제대로 지원하지 않는 모델이 많습니다.
- 설정 파일(`~/.config/ccx/ccx.config.json`)은 홈 디렉터리에 저장되며 권한은 `0600`으로 잠깁니다. 다른 경로를 쓰려면 `CCX_CONFIG` 환경변수로 오버라이드할 수 있습니다.

## 소스에서 빌드

릴리즈 바이너리 대신 직접 빌드하려면 Go 1.21+ 가 필요합니다.

```bash
git clone https://github.com/channel-spoonai/ccx.git
cd ccx
./build.sh       # dist/ccx-<os>-<arch> 생성
cp dist/ccx-$(uname -s | tr A-Z a-z)-$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/') ~/.local/bin/ccx
chmod +x ~/.local/bin/ccx
```

## License

MIT
