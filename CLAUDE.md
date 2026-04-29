# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What is ccx

Claude Code를 다른 LLM 프로바이더(z.ai GLM, OpenRouter 등)로 전환하기 위한 CLI 래퍼. 프로파일 기반으로 환경변수를 설정하고 `claude`를 실행한다. 외부 의존성 없이 Node.js 빌트인 모듈만 사용하는 단일 파일(`ccx.mjs`) 구조.

## Running

```bash
node ccx.mjs                                         # 인터랙티브 프로파일 선택 메뉴
node ccx.mjs -xSet "GLM Coding Plan" -p "hello"      # 프로파일 직접 지정 + claude 인자 전달
```

설치 후(`install.sh`): `ccx` 명령어로 직접 실행 가능.

## Architecture

단일 파일 `ccx.mjs` (Node.js ESM, zero dependencies)에 모든 로직이 있다:

1. **Config** (`loadConfig`) — `ccx.config.json`을 읽어 프로파일 배열을 로드. `CCX_CONFIG` 환경변수로 경로 오버라이드 가능.
2. **Args** (`parseArgs`) — `-xSet "name"`만 추출하고, 나머지 인자는 전부 claude에 패스스루.
3. **Menu** (`selectProfile`) — `process.stdin.setRawMode(true)`로 화살표 키 네비게이션 구현. ANSI escape 코드로 렌더링. 숫자 키(1-9)로도 바로 선택 가능. TTY가 아니면 실패하므로 비대화형 환경에서는 반드시 `-xSet` 사용.
4. **Launch** (`launchClaude`) — 프로파일 설정을 환경변수로 매핑한 뒤 `spawn(CLAUDE_CMD, args, { stdio: 'inherit', shell: true })`로 실행. `shell: true`라서 passthrough 인자에 특수문자가 있으면 호출 측에서 인용 처리 필요.

프로파일 → 환경변수 매핑:
- `baseUrl` → `ANTHROPIC_BASE_URL`
- `apiKey` → `ANTHROPIC_API_KEY` (`x-api-key` 헤더로 보내는 프로바이더, 예: OpenRouter)
- `authToken` → `ANTHROPIC_AUTH_TOKEN` (`Authorization: Bearer` 헤더로 보내는 프로바이더, 예: z.ai)
- `models.opus/sonnet/haiku` → `ANTHROPIC_DEFAULT_{OPUS,SONNET,HAIKU}_MODEL`
- `model` → `ANTHROPIC_MODEL`
- `env` → 임의 환경변수 (예: `API_TIMEOUT_MS`)

`apiKey`와 `authToken`은 상호 배타적이 아니라 단순히 둘 다 설정되면 둘 다 주입되므로, 프로바이더 문서에 맞춰 **하나만** 사용할 것.

## Configuration

`ccx.config.json` (gitignored — API 키 포함):
```json
{
  "profiles": [{
    "name": "Profile Name",
    "description": "optional",
    "baseUrl": "https://...",
    "authToken": "key",
    "models": { "opus": "model-id", "sonnet": "model-id", "haiku": "model-id" },
    "env": { "API_TIMEOUT_MS": "3000000" }
  }]
}
```

`ccx.config.example.json`이 템플릿 역할. 새 프로바이더 추가 시 example 파일도 함께 업데이트할 것.

## Supported Providers

ccx는 Claude Code를 재사용하기 때문에 **Anthropic 호환 엔드포인트(`/v1/messages`)를 제공하는 프로바이더만** 지원한다. 별도 SDK나 변환 로직은 없다. 각 프로바이더의 정확한 URL/모델 ID는 자주 바뀌므로 `ccx.config.example.json` 업데이트 시 공식 문서를 다시 확인할 것.

| Provider | baseUrl | Auth 필드 | 비고 |
|---|---|---|---|
| z.ai GLM | `https://api.z.ai/api/anthropic` | `authToken` | docs.z.ai/scenario-example/develop-tools/claude |
| Kimi (Moonshot) | `https://api.moonshot.ai/anthropic` | `authToken` | platform.kimi.ai/docs/guide/agent-support |
| DeepSeek | `https://api.deepseek.com/anthropic` | `apiKey` | api-docs.deepseek.com/guides/anthropic_api |
| MiniMax | `https://api.minimax.io/anthropic` | `apiKey` | platform.minimax.io/docs/api-reference/text-anthropic-api |
| OpenRouter | `https://openrouter.ai/api` | `apiKey` | openrouter.ai/docs — Claude Code가 `/v1/messages`를 자동 append하므로 `/v1` 없이 지정. 모델 ID는 `provider/model[:tag]` 형식 (예: `google/gemma-2-9b-it:free`) |
| LM Studio (로컬) | `http://localhost:1234` | `authToken: "lmstudio"` (더미, 선택) | lmstudio.ai/docs/developer/anthropic-compat, v0.4.1+ 필요 |

**로컬 프로바이더 주의사항**: 모델 ID는 LM Studio에서 로드한 실제 식별자여야 한다(예: `ibm/granite-4-micro`). Claude Code가 기대하는 툴 사용/캐시 제어 동작을 로컬 모델이 완전히 지원하지 않을 수 있다.

### 미지원: OAuth 기반 프로바이더

**ChatGPT Plus / Codex 구독 계정은 ccx에서 직접 사용할 수 없다.** ccx는 OAuth 플로우를 수행하지 않고 정적 토큰만 주입한다. OpenAI는 Anthropic 호환 엔드포인트를 제공하지 않기 때문에 브릿지 프록시가 필요하다. 실사용 시 패턴:

1. 별도 프록시(예: `anthropic-max-router`, `claude-to-chatgpt`)를 로컬에서 실행해 OAuth/ChatGPT 쿠키를 처리하고 `http://localhost:PORT`에 Anthropic 호환 엔드포인트를 노출
2. ccx 프로파일의 `baseUrl`을 그 로컬 URL로 지정

프록시 쪽에서 인증이 끝나므로 ccx의 `authToken`은 더미로도 충분하다.

## Installation

`install.sh`를 실행하면 `~/.local/bin/ccx` shim이 생성됨 (macOS/Linux 공용).
`~/.local/bin`이 PATH에 포함되어 있어야 `ccx` 명령어로 직접 실행 가능.

## Releasing

릴리즈는 `/release` 슬래시 명령으로 자동화되어 있다(`v*` 태그 push → GitHub Actions가 goreleaser로 5개 OS/arch 아카이브 생성).

GitHub push 인증은 **레포 루트의 `.env` 파일**(gitignore됨)에 저장된 `GIT_RELEASE_TOKEN`을 사용한다. osxkeychain이나 remote URL은 건드리지 않는다 — 토큰은 일회성 credential helper로만 주입한다:

```bash
set -a; . ./.env; set +a
git -c credential.helper= \
    -c "credential.helper=!f() { echo username=x-access-token; echo password=$GIT_RELEASE_TOKEN; }; f" \
    push origin main vX.Y.Z
```

토큰이 만료/회수되면 `.env`만 갱신하면 된다. 401/403 응답이 나오면 사용자에게 갱신 요청.

## Key Conventions

- 이 프로젝트의 모든 사용자 메시지와 주석은 한국어로 작성
- 프로파일 이름 매칭은 case-insensitive
- `ccx`는 `claude` 실행파일 자체를 수정하지 않음 — 환경변수만으로 동작
