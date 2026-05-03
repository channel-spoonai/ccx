package codex

import (
	"encoding/base64"
	"encoding/json"
	"strings"
)

// idTokenClaims는 OpenAI가 발급하는 id_token에서 우리가 쓰는 필드만 정의.
type idTokenClaims struct {
	ChatGPTAccountID string `json:"chatgpt_account_id"`
	Organizations    []struct {
		ID string `json:"id"`
	} `json:"organizations"`
	Email string `json:"email"`

	// 일부 토큰은 네임스페이스 클레임으로 account_id를 박는다.
	OpenAIAuth *struct {
		ChatGPTAccountID string `json:"chatgpt_account_id"`
	} `json:"https://api.openai.com/auth"`
	OpenAIAuthFlat string `json:"https://api.openai.com/auth.chatgpt_account_id"`
}

// TokenResponse는 OpenAI /oauth/token 의 표준 응답.
type TokenResponse struct {
	IDToken      string `json:"id_token,omitempty"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in,omitempty"`
}

// parseJWTClaims는 JWT의 두 번째 segment(payload)를 base64url 디코드한다.
// 서명 검증은 하지 않는다 — 우리가 직접 받은 토큰이므로 신뢰하고, 단지 claim 추출만.
func parseJWTClaims(token string) (*idTokenClaims, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, false
	}
	// base64url에 패딩이 없을 수 있으므로 RawURLEncoding 사용.
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		// 일부 토큰은 패딩이 붙어 있을 수도 있어 fallback.
		payload, err = base64.URLEncoding.DecodeString(parts[1])
		if err != nil {
			return nil, false
		}
	}
	var c idTokenClaims
	if err := json.Unmarshal(payload, &c); err != nil {
		return nil, false
	}
	return &c, true
}

// ExtractAccountID는 토큰 응답에서 ChatGPT account ID를 뽑는다.
// id_token을 우선 보고, 없으면 access_token이 JWT인 경우 거기서 시도.
func ExtractAccountID(t TokenResponse) string {
	if t.IDToken != "" {
		if c, ok := parseJWTClaims(t.IDToken); ok {
			if id := pickAccountID(c); id != "" {
				return id
			}
		}
	}
	if t.AccessToken != "" {
		if c, ok := parseJWTClaims(t.AccessToken); ok {
			return pickAccountID(c)
		}
	}
	return ""
}

func pickAccountID(c *idTokenClaims) string {
	if c.ChatGPTAccountID != "" {
		return c.ChatGPTAccountID
	}
	if c.OpenAIAuth != nil && c.OpenAIAuth.ChatGPTAccountID != "" {
		return c.OpenAIAuth.ChatGPTAccountID
	}
	if c.OpenAIAuthFlat != "" {
		return c.OpenAIAuthFlat
	}
	if len(c.Organizations) > 0 {
		return c.Organizations[0].ID
	}
	return ""
}
