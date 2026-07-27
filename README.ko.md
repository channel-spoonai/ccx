# ccx

[🇺🇸 English](README.md)

Claude Code를 z.ai GLM, Kimi, DeepSeek, MiniMax, OpenRouter, NVIDIA NIM, LM Studio, **ChatGPT(Codex 구독)** 같은 다른 LLM 프로바이더로 돌려 쓰는 CLI 래퍼.

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

### 업데이트

ccx는 스스로 최신 상태를 유지합니다: 하루에 한 번 GitHub에서 새 릴리즈를 확인하고(캐시는 `~/.config/ccx/update-check.json`), 새 버전이 발견되면 다음 실행 시작 시(프로파일 메뉴 전에) 자동으로 적용합니다 — 새 버전은 그다음 실행부터 반영됩니다. 끄려면 `CCX_AUTO_UPDATE=0`을 설정하세요. 스크립트/비-TTY 실행과 쓰기 권한이 없는 설치 위치에서는 자동 적용 대신 한 줄 알림만 표시되고, dev 빌드(소스 빌드)는 업데이트 체크 자체를 하지 않습니다.

수동으로 즉시 업데이트하려면 언제든:

```bash
ccx update
```

설치 스크립트를 다시 실행해도 됩니다 — 동일하게 최신 바이너리를 받아 덮어씁니다. `ccx update`나 자동 업데이트가 없던 구버전(v0.1.x)에서 올라올 때는 이 방법(설치 원라이너 재실행)이 유일한 경로입니다.

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

z.ai GLM · Kimi (Moonshot) · DeepSeek · MiniMax · OpenRouter · NVIDIA NIM · LM Studio (로컬) · ChatGPT (Codex) · OpenAI API

기본 설정은 바이너리에 카탈로그로 임베드되어 있어 손댈 필요가 없습니다. 메뉴에서 추가하고 API 키만 입력하면 동작합니다.

### ChatGPT (Codex) 구독으로 사용하기

ChatGPT Plus/Pro/Business 구독으로 Claude Code를 돌립니다. 사용 전 OAuth 인증 1회 필요합니다.

```bash
ccx codex login                  # 브라우저로 ChatGPT 인증 (헤드리스: --device)
ccx -xSet "Codex"                # 인증 후 사용
```

모델 매핑 (Claude Code 티어 → Codex 모델):

| Claude Code | Codex |
|---|---|
| opus   | gpt-5.6-sol |
| sonnet | gpt-5.6-terra |
| haiku  | gpt-5.6-luna |

구형 ID(`gpt-5.5`, `gpt-5.4`, `gpt-5.4-mini`)도 계속 유효합니다. `gpt-5.6-sol`은 일부 ChatGPT 플랜에서 거부될 수 있으니 그 경우 해당 슬롯을 `gpt-5.6-terra`로 바꾸세요. ChatGPT 백엔드는 `[1m]` suffix와 무관하게 컨텍스트 창을 272K로 캡하는데, ccx가 이를 알고 `CLAUDE_CODE_AUTO_COMPACT_WINDOW=272000`을 자동 적용합니다([컨텍스트 윈도우](#컨텍스트-윈도우) 참고). 진짜 1M 컨텍스트가 필요하면 아래 OpenAI API 키 프로파일을 사용하세요.

상태/로그아웃: `ccx codex status` / `ccx codex logout`

### OpenAI API 키로 사용하기 (Responses API)

ChatGPT 구독 대신 종량제 OpenAI API를 쓰고 싶다면 `openai-responses` 프로파일을 사용합니다. API 키로 공식 Responses API(`https://api.openai.com/v1/responses`)에 직접 라우팅하며 — OAuth가 필요 없고 272K 캡도 없어 GPT-5.6 모델의 전체 1M+ 컨텍스트를 쓸 수 있습니다.

```json
{
  "name": "OpenAI API",
  "auth": "openai-responses",
  "apiKey": "env:OPENAI_API_KEY",
  "models": {
    "opus": "gpt-5.6-sol[1m]",
    "sonnet": "gpt-5.6-terra[1m]",
    "haiku": "gpt-5.6-luna"
  }
}
```

`apiKey`에는 키를 직접 넣거나 `env:변수명` 참조를 쓸 수 있습니다. 과금 주의: 입력이 272K 토큰을 넘는 요청은 OpenAI의 long-context 요율(해당 요청 전체에 입력 2배/출력 1.5배)로 과금됩니다 — 임계값 아래로 유지하고 싶으면 프로파일에 `"env": { "CLAUDE_CODE_AUTO_COMPACT_WINDOW": "272000" }`을 추가하세요.

### NVIDIA NIM (무료 개발자 티어)

[build.nvidia.com](https://build.nvidia.com/)은 100개가 넘는 오픈웨이트 모델(Nemotron 3, DeepSeek, GLM, Kimi, Llama, gpt-oss 등)을 OpenAI 호환 엔드포인트 하나로 제공합니다. 무료 NVIDIA Developer Program에 가입하면 `nvapi-` 키를 받습니다(크레딧 약 1000개, 분당 약 40요청). 무료 티어는 **계정 단위이지 모델별이 아닙니다** — "무료 모델" 구분 자체가 없고 API도 그런 정보를 노출하지 않습니다.

NVIDIA에는 Anthropic 엔드포인트가 없어 ccx가 내장 `openai-chat` 변환 프록시를 띄웁니다(lightning-mlx와 같은 경로).

```bash
export NVIDIA_API_KEY=nvapi-...
ccx -xSet "NVIDIA NIM"
```

메뉴로 프로파일을 추가하면 ccx가 서버에서 실제 서빙 중인 모델을 확인해 엄선된 목록만 티어별로 고르게 해줍니다:

| 모델 | NIM에서의 컨텍스트 |
|---|---|
| `z-ai/glm-5.2` | 202K |
| `minimaxai/minimax-m3` | 524K |
| `nvidia/nemotron-3-ultra-550b-a55b` | 1M |
| `nvidia/nemotron-3-super-120b-a12b` | 1M |
| `deepseek-ai/deepseek-v4-flash` | 1M |
| `deepseek-ai/deepseek-v4-pro` | 262K |

나머지 카탈로그는 숨깁니다. 대부분은 Claude Code의 에이전트 워크로드(긴 컨텍스트에서의 툴 콜링)를 감당하지 못하고, 상당수는 애초에 대화형이 아닙니다(임베딩·리랭커·가드레일·리워드·문서파싱). 목록 밖의 모델을 쓰려면 프로파일 `models`에 ID를 직접 적으면 됩니다 — ccx가 아는 건 위 표의 값뿐이므로 `[Nk]` suffix도 함께 적어주세요.

위 컨텍스트 수치는 모델 카드가 아니라 **NIM이 실제로 서빙하는 한도를 직접 측정한 값**입니다. NVIDIA는 자체 `--max-model-len`으로 배포하기 때문에 같은 모델도 프로바이더마다 다릅니다 — GLM-5.2는 z.ai에서 1M이지만 여기서는 202K, DeepSeek V4 Pro는 DeepSeek 직결에서 1M이지만 여기서는 262K입니다. ccx는 프로파일 추가 시 이 실측값을 suffix로 기록하므로 Claude Code가 오버플로 전에 압축합니다.

## 컨텍스트 윈도우

Claude Code는 모르는 모델 ID의 컨텍스트 윈도우를 200K로 가정합니다. 대부분의 서드파티 모델과 어긋나는 값이라 — 128K 로컬 모델은 컨텍스트 오버플로가 나고, 1M DeepSeek 모델은 절반도 못 씁니다. ccx가 모델별로 이걸 교정합니다:

- **모델 ID에 선언**: 프로파일 `models`에 `[131k]`, `[262k]`, `[1m]` 같은 suffix를 붙이세요 (예: `"sonnet": "kimi-k2.5[262k]"`). ccx가 이 표기를 Claude Code가 실제로 이해하는 형태 — 공식 `[1m]` 마커와 `CLAUDE_CODE_AUTO_COMPACT_WINDOW` 캡 — 로 변환하고, 프로바이더에는 suffix가 절대 전달되지 않게 제거합니다.
- **카탈로그 자동 적용**: 알려진 모델(GLM, Kimi, DeepSeek, MiniMax, GPT-5.5/5.6, NVIDIA NIM 라인업)은 ccx에 정확한 수치가 내장되어 있어 suffix 없이도 올바른 윈도우가 적용됩니다. 카탈로그의 정확값(예: Kimi 262,144)이 `[262k]` floor 표기보다 정밀하므로, 알려진 모델은 suffix 없이 두는 것이 좋습니다.
- **추가 시 자동 감지**: 메뉴에서 OpenRouter나 LM Studio 프로파일을 추가하면 ccx가 프로바이더에서 실제 컨텍스트 길이(LM Studio는 실제 로드된 값)를 조회해 suffix로 기록해 줍니다. OpenRouter 주의: 기록값은 모델 공표값과 1순위 프로바이더 값 중 작은 쪽인데, 로드밸런싱이 더 작은 컨텍스트를 서빙하는 하위 프로바이더로 라우팅할 수도 있습니다 — 문제가 되면 더 작은 `[Nk]` suffix를 직접 지정하세요.

실행 배너에 해석 결과가 표시됩니다 (예: `Context: sonnet 262k (suffix) → auto-compact 262144`). 사용자 설정이 항상 우선입니다: 프로파일 `env`나 셸에 `CLAUDE_CODE_AUTO_COMPACT_WINDOW`를 직접 지정하면 계산값 대신 그 값이 쓰입니다 (둘 다 지정하면 프로파일 `env`가 최종 적용). 기능을 끄려면 `CCX_CONTEXT_AUTO=0` — 이때도 ccx 전용 `[Nk]` 표기는 모델 ID에서 제거해 요청이 깨지지 않게 하고, 그 외 컨텍스트 설정(Codex 272K 자동 캡 포함)은 일절 적용하지 않습니다.

참고: auto-compact 윈도우는 세션당 단일값이라 opus/sonnet(및 최상위 `model`) 슬롯 중 가장 작은 윈도우를 채택합니다 (haiku는 제외 — 백그라운드 호출 전용이라 소형 haiku 모델이 세션 전체를 캡하는 일을 막습니다. 대신 배너에 경고를 띄웁니다).

## 알아두면 좋은 점

- **LM Studio(로컬)는 모델에 따라 품질 편차가 큽니다.** 툴 사용이나 긴 컨텍스트를 제대로 지원하지 않는 모델이 많습니다.
- 설정 파일(`~/.config/ccx/ccx.config.json`)은 홈 디렉터리에 저장되며 권한은 `0600`으로 잠깁니다. 다른 경로를 쓰려면 `CCX_CONFIG` 환경변수로 오버라이드할 수 있습니다.

## 소스에서 빌드

릴리즈 바이너리 대신 직접 빌드하려면 Go 1.26+ 가 필요합니다 (`go.mod` 기준).

```bash
git clone https://github.com/channel-spoonai/ccx.git
cd ccx
./build.sh       # dist/ccx-<os>-<arch> 생성
cp dist/ccx-$(uname -s | tr A-Z a-z)-$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/') ~/.local/bin/ccx
chmod +x ~/.local/bin/ccx
```

## License

MIT
