package codex

import "time"

// Codex(ChatGPT) OAuth 상수.
// raine/claude-code-proxy의 src/providers/codex/auth/constants.ts 와 동일 값을 사용한다.
// 파라미터가 일치해야 OpenAI가 ChatGPT 구독 빌링 경로로 라우팅한다.
const (
	ClientID         = "app_EMoamEEZ73f0CkXaXp7hrann"
	Issuer           = "https://auth.openai.com"
	CodexAPIEndpoint = "https://chatgpt.com/backend-api/codex/responses"

	// OAuth 콜백 포트. OpenAI client_id의 redirect_uri 화이트리스트가 정확히
	// `http://localhost:{1455|1457}/auth/callback` 두 개로만 제한되어 있어 임의 포트는 거부된다.
	// 공식 codex CLI도 1455 우선 → 점유되면 1457 fallback 패턴을 사용한다.
	OAuthPrimaryPort  = 1455
	OAuthFallbackPort = 1457

	// Originator는 OpenAI가 클라이언트를 식별하는 토큰. 차단 회피 차원에서
	// 의도적으로 raine과 동일한 "claude-code-proxy"를 그대로 보낸다 — 자체 값으로 바꾸면
	// 미알려진 originator로 분류되어 즉시 차단될 수 있다.
	Originator = "claude-code-proxy"

	// 만료 5분 전부터 자동 refresh.
	RefreshMargin = 5 * time.Minute

	// PKCE verifier 바이트 길이(base64url 인코딩 후 ~43자).
	pkceVerifierBytes = 32
	stateBytes        = 32
)
