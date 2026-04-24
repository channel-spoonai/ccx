# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What is claudex

Claude Code를 다른 LLM 프로바이더(z.ai GLM, OpenRouter 등)로 전환하기 위한 CLI 래퍼. 프로파일 기반으로 환경변수를 설정하고 `claude`를 실행한다. 외부 의존성 없이 Node.js 빌트인 모듈만 사용하는 단일 파일(`claudex.mjs`) 구조.

## Running

```bash
node claudex.mjs                                         # 인터랙티브 프로파일 선택 메뉴
node claudex.mjs -xSet "GLM Coding Plan" -p "hello"      # 프로파일 직접 지정 + claude 인자 전달
```

설치 후(`install.sh`): `claudex` 명령어로 직접 실행 가능.

## Architecture

단일 파일 `claudex.mjs` (Node.js ESM, zero dependencies)에 모든 로직이 있다:

1. **Config** (`loadConfig`) — `claudex.config.json`을 읽어 프로파일 배열을 로드. `CLAUDEX_CONFIG` 환경변수로 경로 오버라이드 가능.
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

`claudex.config.json` (gitignored — API 키 포함):
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

`claudex.config.example.json`이 템플릿 역할. 새 프로바이더 추가 시 example 파일도 함께 업데이트할 것.

## Installation

`install.sh`를 실행하면 `~/.local/bin/claudex` shim이 생성됨 (macOS/Linux 공용).
`~/.local/bin`이 PATH에 포함되어 있어야 `claudex` 명령어로 직접 실행 가능.

## Key Conventions

- 이 프로젝트의 모든 사용자 메시지와 주석은 한국어로 작성
- 프로파일 이름 매칭은 case-insensitive
- `claudex`는 `claude` 실행파일 자체를 수정하지 않음 — 환경변수만으로 동작
