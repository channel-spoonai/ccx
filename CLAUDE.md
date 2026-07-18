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

ccx는 Claude Code를 재사용하므로 기본 경로는 **Anthropic 호환 엔드포인트(`/v1/messages`)를 그대로 사용**한다. Anthropic 엔드포인트가 없는 업스트림은 내장 변환 프록시로 지원한다 — `auth: "openai-chat"`(Chat Completions), `auth: "codex-oauth"`(ChatGPT Responses), `auth: "openai-responses"`(OpenAI API Responses). 각 프로바이더의 정확한 URL/모델 ID는 자주 바뀌므로 `ccx.config.example.json` 업데이트 시 공식 문서를 다시 확인할 것.

| Provider | baseUrl | Auth 필드 | 비고 |
|---|---|---|---|
| z.ai GLM | `https://api.z.ai/api/anthropic` | `authToken` | docs.z.ai/scenario-example/develop-tools/claude |
| Kimi (Moonshot) | `https://api.moonshot.ai/anthropic` | `authToken` | platform.kimi.ai/docs/guide/agent-support |
| DeepSeek | `https://api.deepseek.com/anthropic` | `apiKey` | api-docs.deepseek.com/guides/anthropic_api |
| MiniMax | `https://api.minimax.io/anthropic` | `apiKey` | platform.minimax.io/docs/api-reference/text-anthropic-api |
| OpenRouter | `https://openrouter.ai/api` | `apiKey` | openrouter.ai/docs — Claude Code가 `/v1/messages`를 자동 append하므로 `/v1` 없이 지정. 모델 ID는 `provider/model[:tag]` 형식 (예: `google/gemma-2-9b-it:free`) |
| LM Studio (로컬) | `http://localhost:1234` | `authToken: "lmstudio"` (더미, 선택) | lmstudio.ai/docs/developer/anthropic-compat, v0.4.1+ 필요 |
| Lightning-MLX (로컬) | `http://127.0.0.1:<port>` | `authToken` (더미, 선택) | `auth: "openai-chat"` 디스크리미네이터 사용 — ccx가 OpenAI Chat Completions 변환 프록시를 띄워 라우팅. 모델 ID는 `/v1/models` 응답값(서버 기본 `local`) |
| OpenAI API | (자동 — 기본 `api.openai.com/v1/responses`) | `apiKey` | `auth: "openai-responses"` 디스크리미네이터 — codex 변환 프록시를 API 키 모드로 재사용. GPT-5.6 전체 1M 컨텍스트 (272K 캡 없음) |

**로컬 프로바이더 주의사항**: 모델 ID는 LM Studio/Lightning-MLX에 실제 로드된 식별자여야 한다(예: LM Studio `ibm/granite-4-micro`, Lightning-MLX `local`). Claude Code가 기대하는 툴 사용/캐시 제어 동작을 로컬 모델이 완전히 지원하지 않을 수 있다.

### OpenAI Chat Completions 변환 프로바이더: `auth: "openai-chat"`

OpenAI 호환 `/v1/chat/completions` 엔드포인트만 노출하고 Anthropic `/v1/messages`는 지원하지 않거나, Anthropic 엔드포인트가 있어도 streaming 사양이 Claude Code와 충돌하는 서버를 위해 ccx 내장 변환 프록시를 사용한다. lightning-mlx, vLLM, LocalAI, 일부 OpenRouter 모델 등에 활용.

```json
{
  "name": "lightning-mlx (local)",
  "auth": "openai-chat",
  "baseUrl": "http://127.0.0.1:8010",
  "authToken": "lightning-mlx",
  "models": { "opus": "local", "sonnet": "local", "haiku": "local" }
}
```

**아키텍처**:
- `internal/translate/openaichat/` — Anthropic Messages ↔ OpenAI Chat Completions 변환 (request/accumulate/sse)
- `internal/proxy/openaichat/` — 로컬 HTTP 프록시 + self-respawn 데몬 (`__openai-chat-proxy` hidden 서브명령)
- 라우팅 흐름은 codex 어댑터와 동일: 부모 ccx → SpawnDaemon (자식이 ready 메시지로 포트 보고) → syscall.Exec(claude) → 자식이 부모 PID polling으로 종료 감지

**enable_thinking 처리**: 디폴트는 `false`. lightning-mlx 같은 Qwen3 reasoning 모델은 streaming 시 `delta.reasoning_content`로 chain-of-thought를 흘리며 동일 token budget을 공유해 실제 응답이 1-2글자만 남는 사양 차이가 있다. Claude Code는 이 비표준 필드를 활용할 수 없으므로 reasoning을 비활성화한다. 사용자가 reasoning을 활성화하려면 `profile.env`에 `"CCX_OPENAICHAT_ENABLE_THINKING": "true"` 추가.

**한계 / 위험**:
- prompt caching (Anthropic 전용) 같은 헤더는 strip됨
- tool_result 내 이미지는 텍스트 placeholder로 치환 (대부분의 OpenAI 호환 서버는 tool role content를 string만 받음)
- 토큰 카운트는 chars/4 휴리스틱 — 정확한 토크나이저 없음

### OAuth 기반 프로바이더: ChatGPT (Codex)

ChatGPT Plus/Pro/Business 구독을 OAuth로 인증해 Claude Code를 라우팅한다. 별도 프록시 바이너리 없이 ccx 내장.

```bash
ccx codex login                  # 브라우저 PKCE 플로우
ccx codex login --device         # 헤드리스/SSH 환경용 디바이스 코드 플로우
ccx codex status                 # 현재 인증 상태
ccx codex logout                 # 토큰 삭제
ccx -xSet "Codex"                # 라우팅 시작
```

프로파일은 `auth: "codex-oauth"` 디스크리미네이터만 두고 baseUrl/authToken은 비워둔다 — ccx가 자동으로 로컬 프록시(랜덤 포트)를 띄우고 채워준다.

모델 ID는 프록시에서 `[1m]`/`[200k]` 컨텍스트 suffix만 제거하고 그대로 업스트림에 패스스루된다 — 허용 목록이 없어 새 모델은 config 갱신만으로 사용 가능. 카탈로그 기본값은 GPT-5.6 패밀리(opus→`gpt-5.6-sol`, sonnet→`gpt-5.6-terra`, haiku→`gpt-5.6-luna`). **ChatGPT 백엔드는 컨텍스트를 272K로 캡**하므로(모델의 API 스펙이 1M이어도, openai/codex#32806 참고) `ctxwin.Apply`가 codex-oauth 프로파일에 `min(W, 272000)` 캡을 자동 적용한다 — 이건 버그가 아니라 백엔드 실측 한도이며, 수동 env 지정은 더 이상 불필요(지정하면 그 값이 우선). 전체 1M 컨텍스트는 `auth: "openai-responses"`(API 키) 경로에서만 가능. `gpt-5.6-sol`은 일부 ChatGPT 플랜에서 거부될 수 있음(그 경우 `gpt-5.6-terra`로 대체).

**아키텍처**:
- `internal/auth/codex/` — PKCE/디바이스 코드 OAuth 클라이언트, 토큰 저장(`~/.config/ccx/auth/codex.json` mode 0600), 자동 refresh
- `internal/translate/codex/` — Anthropic Messages ↔ OpenAI Responses 변환 + SSE 스트리밍
- `internal/proxy/codex/` — 로컬 HTTP 프록시 + self-respawn 데몬 (`__codex-proxy` hidden 서브명령)
- 부모 ccx → `SpawnDaemon` (자식이 ready 메시지로 포트 보고) → `syscall.Exec(claude)` 로 PID 보존 전환 → 자식이 부모 PID(=claude) polling으로 종료 감지 (생존 판정은 `internal/procutil` — unix는 signal 0, Windows는 OpenProcess+WaitForSingleObject; Windows에 signal 0을 쓰면 항상 살아있다고 오판해 데몬이 누수된다)

**OAuth 파라미터**(`internal/auth/codex/constants.go`): client_id `app_EMoamEEZ73f0CkXaXp7hrann`, originator `claude-code-proxy` — OpenAI가 식별하는 값이라 변경 시 즉시 차단될 수 있어 의도적으로 raine/claude-code-proxy 구현체와 동일하게 유지.

**한계 / 위험**:
- ChatGPT 구독을 비공식 클라이언트로 사용하는 것은 OpenAI ToS의 회색지대 — 계정 정지 위험 존재
- Codex의 reasoning 콘텐츠는 strip되어 Claude Code의 thinking UI에 표시되지 않음
- tool_result 내 이미지는 `[image omitted]` 플레이스홀더로 치환 (Codex 백엔드가 거부)
- prompt caching, computer-use 같은 Anthropic 전용 기능은 strip됨
- 토큰 카운트는 정확한 토크나이저 없이 chars/4 휴리스틱 — 한국어/CJK는 underestimate 가능

### OpenAI API 키 프로바이더: `auth: "openai-responses"`

ChatGPT 구독 대신 종량제 OpenAI API 키로 같은 변환 경로를 쓴다. codex 프록시를 API 키 모드로 재사용 — `internal/proxy/codex`의 `UpstreamConfig{Endpoint, APIKey}`가 zero value면 ChatGPT OAuth 모드, APIKey가 있으면 `Authorization: Bearer <key>`만 보내고 ChatGPT 전용 헤더(originator, ChatGPT-Account-Id, session 트로이카)와 OAuth refresh를 생략한다.

```json
{
  "name": "OpenAI API",
  "auth": "openai-responses",
  "apiKey": "env:OPENAI_API_KEY",
  "models": { "opus": "gpt-5.6-sol[1m]", "sonnet": "gpt-5.6-terra[1m]", "haiku": "gpt-5.6-luna" }
}
```

- `apiKey`는 리터럴 키 또는 `env:VAR` 참조 (`ResolveSecret`), 비어있으면 `authToken` fallback
- `baseUrl`은 호환 게이트웨이 오버라이드 용 — `/responses`로 끝나면 그대로, `/v1`로 끝나면 `/responses`만, 그 외에는 `/v1/responses`를 붙임. `env:` 참조가 미해석이면 조용한 폴백 대신 에러 (`internal/launcher/openai_responses.go`)
- ChatGPT 백엔드의 272K 캡이 없어 GPT-5.6의 1M+ 컨텍스트를 그대로 사용. 단 272K 초과 입력은 long-context 요율(입력 2배/출력 1.5배) 과금 — 절약하려면 `env`에 `CLAUDE_CODE_AUTO_COMPACT_WINDOW=272000` 추가
- 데몬 전달 환경변수: `CCX_CODEX_UPSTREAM_URL` / `CCX_CODEX_UPSTREAM_APIKEY` (`internal/proxy/codex/spawn.go`)

## Context Windows (`internal/ctxwin`)

Claude Code는 모델 ID 패턴 하드코딩으로 컨텍스트 윈도우를 추론하고 커스텀 ID는 200K로 가정한다. 오버라이드 수단은 둘뿐이다(2026-07 로컬 리스너 실측 완료):

- `[1m]` suffix — 유일하게 공식 인식되는 suffix. Claude Code가 1M으로 인식하고 **API 전송 전 strip한다** (직결 프로바이더에도 안전). `[200k]` 같은 다른 suffix는 인식하지 않고 **업스트림에 리터럴로 보낸다** — 그래서 ccx가 env 주입 전에 반드시 제거해야 한다.
- `CLAUDE_CODE_AUTO_COMPACT_WINDOW` — 인식 용량 설정, 단 모델 추론 윈도우로 캡되어 **하향만 가능**.

`ctxwin.Apply`(Launch 최상단, 4개 auth 경로 공통)가 프로파일 copy의 모델 ID suffix(없으면 `catalog.go`의 정적 수치)를 전달 공식으로 변환한다: W≥1M → `[1m]` / 200K<W<1M → `[1m]`+ACW=W / W==200K → 표기 제거만 / W<200K → ACW=W. **200K<W<1M 구간의 `[1m]`+ACW는 분리 불가능한 짝** — 이 불변식 때문에 (a) ACW 후보에서 제외되는 haiku는 W≥1M일 때만 `[1m]`을 받고(짝 없는 `[1m]`은 1M 과대 인식), (b) 사용자 명시 ACW가 티어 실제 윈도우보다 크면 그 티어의 `[1m]` 부착을 생략한다(200K 추정이 안전). ACW는 전역 단일값이라 opus/sonnet/model 중 min을 채택하고 haiku는 제외(소형 haiku가 세션 전체를 캡하는 것 방지, 대신 배너 경고). codex-oauth는 소스 무관 `min(W, 272000)` 캡. 우선순위: 사용자 명시값(`profile.env` > ambient — BuildEnv의 p.Env 루프가 마지막이라) > 계산값. `CCX_CONTEXT_AUTO=0`이면 컨텍스트 설정을 전부 생략하되 ccx 전용 suffix의 업스트림 유출만은 strip으로 막는다(`[1m]`은 보존). 카탈로그 prefix 매칭은 토큰 경계 검사 포함(`kimi-k30`이 `kimi-k3`에 매칭되지 않음).

프로파일 생성 flows에서는 OpenRouter(`EffectiveContext` — 모델/1순위 프로바이더 중 min)와 LM Studio(`FetchLMStudioContexts` — 네이티브 `/api/v1/models`의 `loaded_instances[].config.context_length`, 실할당값만 신뢰, v0 폴백)가 감지값을 `ContextSuffix`로 모델 ID에 박제한다. z.ai/DeepSeek/MiniMax/OpenAI는 모델 목록 API가 컨텍스트를 노출하지 않아 `catalog.go` 정적 테이블(최장 prefix 매칭)이 담당 — 신규 모델은 테이블 갱신 후 릴리즈하면 자동 업데이트로 전파된다.

## Installation

`install.sh`(macOS/Linux)는 `releases/latest`를 조회해 최신 아카이브를 받아 `~/.local/bin/ccx`(`CCX_BIN_DIR`로 오버라이드)에 설치한다. `~/.local/bin`이 PATH에 있어야 `ccx`로 직접 실행 가능. Windows는 `install.ps1`이 `%LOCALAPPDATA%\Programs\ccx\ccx.exe`에 설치한다.

설치 스크립트 재실행은 버전과 무관하게 최신 바이너리를 같은 경로에 덮어쓰므로, 자동 업데이트/`ccx update`가 없던 구버전(v0.1.x)에서 올라오는 유일한 경로다.

## Updating (자동 업데이트 아키텍처)

`internal/update/` — 버전 체크·알림·자동 적용을 담당. 흐름:

1. **체크(24h 캐시)**: 시작 시 `MaybeNotify`(check.go)가 캐시(`~/.config/ccx/update-check.json`)를 본다. fresh(<24h)하고 새 태그면 태그 반환, stale이면 백그라운드 goroutine으로 fetch만 하고 통과. **Unix launch는 `syscall.Exec`라 goroutine이 죽으므로**, launch 직전 `WaitBackgroundFetch(2s)`(main.go의 두 Launch 지점)가 캐시 쓰기를 보장한다 — 이 호출을 제거하면 `-xSet` 직행 사용자는 체크가 영영 완료되지 않는다.
2. **자동 적용**: 캐시가 새 버전을 알 때만 `TryAutoUpdate`(autoupdate.go)가 시작 시 다운로드+교체를 시도(60s 타임아웃). 게이팅(`shouldAutoApply`, 네트워크 없음): dev 빌드 / `CCX_AUTO_UPDATE=0|false|off` opt-out / stdin 비-TTY(스크립트 보호) / 캐시의 실패 마커(`auto_update_failed_tag`) / 바이너리 디렉터리 쓰기 불가 — 걸리면 기존 한 줄 알림으로 폴백. 단 dev 빌드는 `MaybeNotify` 단계에서 이미 제외되어 알림 자체가 없다(게이트의 dev 확인은 방어적 이중화). 실패 시 마커를 기록해 같은 태그는 24h(다음 fetch가 캐시를 덮어쓸 때까지) 재시도하지 않는다. 성공해도 re-exec하지 않음 — 새 버전은 다음 실행부터.
3. **수동**: `ccx update`(cmd/ccx/update.go)는 게이팅 없이 5분 타임아웃으로 `Apply` 호출.
4. **교체**: `atomicReplace` — Unix는 `os.Rename`(inode 교체), Windows는 실행 중 PE를 덮어쓸 수 없어 `ccx.exe → ccx.exe.old-<pid>` rename 후 배치. `.old-*` 잔재와 중단된 임시파일(`ccx-dl-*`, `ccx-new-*`, mtime 1h 초과)은 다음 실행의 `CleanupStaleBinary`가 청소. Windows에서 ccx는 claude 세션 내내 생존하므로 `.old`가 세션 종료까지 잠겨 있는 게 정상 — PID 유니크 이름이 다중 세션 충돌을 막는다.

**데몬 프로토콜 계약 동결**: 자동 업데이트로 "구버전 부모 ccx + 신버전 데몬 바이너리" 조합이 상시화된다(부모가 `os.Executable()` 경로를 spawn하는데 그 경로는 이미 새 바이너리). 따라서 hidden 서브커맨드명(`__codex-proxy`, `__openai-chat-proxy`), 데몬 env 키(`CCX_CODEX_UPSTREAM_*` 등), ready 프로토콜(`"ready <port>\n"`)은 **변경 금지, 추가만 허용**.

## Releasing

`v*` 태그를 push하면 **`.github/workflows/release.yml`**이 `go test ./...` 후 goreleaser(`.goreleaser.yaml`)로 `release --clean`을 실행해 5개 OS/arch 아카이브 + `checksums.txt`를 담은 GitHub Release를 만든다(darwin/linux × amd64/arm64, windows/amd64 — windows/arm64는 제외). 인증은 워크플로가 주입하는 `GITHUB_TOKEN`을 쓰므로 CI 쪽 시크릿 설정은 불필요.

**자산 명명 계약(변경 금지)**: `ccx-{버전(v 없이)}-{os}-{arch}.tar.gz`(windows는 `.zip`), 바이너리는 아카이브 루트의 `ccx`(windows `ccx.exe`). 이 규칙은 `install.sh`/`install.ps1`과 자동 업데이터(`internal/update/release.go`의 `assetName`)가 공유하므로 셋 중 하나만 바꾸면 설치/업데이트가 깨진다. goreleaser의 `before.hooks`가 `cp ccx.config.example.json internal/config/`로 임베드 사본을 동기화한다(build.sh와 동일 계약).

로컬에서 태그를 push하는 경로의 GitHub 인증은 **레포 루트의 `.env` 파일**(gitignore됨)에 저장된 `GIT_RELEASE_TOKEN`을 쓴다. osxkeychain이나 remote URL은 건드리지 않고 일회성 credential helper로만 주입한다:

```bash
set -a; . ./.env; set +a
git -c credential.helper= \
    -c "credential.helper=!f() { echo username=x-access-token; echo password=$GIT_RELEASE_TOKEN; }; f" \
    push origin main vX.Y.Z
```

토큰이 만료/회수되면 `.env`만 갱신하면 된다. 401/403 응답이 나오면 사용자에게 갱신 요청. (Windows GCM에 저장된 개인 계정으로는 `channel-spoonai/ccx` push가 403날 수 있으니 이 토큰 경로를 쓸 것.)

## Key Conventions

- 이 프로젝트의 모든 사용자 메시지와 주석은 한국어로 작성
- 프로파일 이름 매칭은 case-insensitive
- `ccx`는 `claude` 실행파일 자체를 수정하지 않음 — 환경변수만으로 동작
