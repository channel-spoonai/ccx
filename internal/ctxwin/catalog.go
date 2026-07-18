package ctxwin

import "strings"

// catalog는 API로 컨텍스트를 조회할 수 없거나 조회가 과한 프로바이더의
// 공식 문서 수치 (2026-07 확인). 소문자 prefix → 윈도우(토큰), 최장 prefix가
// 이긴다. 모델 ID의 suffix 명시가 항상 카탈로그보다 우선한다.
// 신규 모델은 여기 추가 후 릴리즈하면 자동 업데이트로 전파된다.
var catalog = map[string]int{
	// z.ai GLM — docs.z.ai (4.6에서 128K→200K 확장, 4.5 계열은 128K)
	"glm-4.7": 200_000,
	"glm-4.6": 200_000,
	"glm-4.5": 131_072,

	// Moonshot Kimi — platform.kimi.ai/docs/models (k2.x는 256K, k3는 1M)
	"kimi-k3":   1_000_000,
	"kimi-k2.5": 262_144,
	"kimi-k2.6": 262_144,
	"kimi-k2.7": 262_144,

	// DeepSeek V4 — api-docs.deepseek.com (deepseek-chat/reasoner는 2026-07 폐기)
	"deepseek-v4": 1_000_000,

	// MiniMax — platform.minimax.io (204,800 = 200×1024 정확값)
	"minimax-m2-her": 64_000,
	"minimax-m2":     204_800,
	"minimax-m3":     1_000_000,

	// OpenAI GPT-5.x — developers.openai.com 모델 카드 (1,050,000).
	// ChatGPT 구독 백엔드의 272K 캡은 Apply가 auth로 판별해 별도 적용한다.
	"gpt-5.5": 1_050_000,
	"gpt-5.6": 1_050_000,
}

// CatalogLookup은 모델 ID를 소문자화해 최장 prefix 매칭으로 윈도우를 찾는다.
// 예: "MiniMax-M2-her" → minimax-m2-her(64K)가 minimax-m2(204.8K)를 이긴다.
// prefix 뒤는 문자열 끝이거나 구분자여야 한다 — "kimi-k30"이 "kimi-k3"에,
// "GLM-4.5V"가 "glm-4.5"에 매칭되는 오인을 막는다.
func CatalogLookup(modelID string) (int, bool) {
	id := strings.ToLower(strings.TrimSpace(modelID))
	window, bestLen := 0, -1
	for prefix, w := range catalog {
		if len(prefix) <= bestLen || !strings.HasPrefix(id, prefix) {
			continue
		}
		if len(id) > len(prefix) {
			c := id[len(prefix)]
			if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
				continue
			}
		}
		window, bestLen = w, len(prefix)
	}
	return window, bestLen >= 0
}
