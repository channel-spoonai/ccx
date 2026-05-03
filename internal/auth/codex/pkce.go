package codex

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/url"
)

// PKCECodes는 RFC 7636 PKCE의 verifier/challenge 쌍.
// verifier는 토큰 교환에 다시 사용해야 하므로 콜러가 보관한다.
type PKCECodes struct {
	Verifier  string
	Challenge string
}

// GeneratePKCE는 32바이트 랜덤 verifier와 SHA-256 challenge를 생성한다.
func GeneratePKCE() (PKCECodes, error) {
	verifier, err := randomBase64URL(pkceVerifierBytes)
	if err != nil {
		return PKCECodes{}, fmt.Errorf("PKCE verifier 생성 실패: %w", err)
	}
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	return PKCECodes{Verifier: verifier, Challenge: challenge}, nil
}

// GenerateState는 OAuth state 파라미터(CSRF 방지)를 생성한다.
func GenerateState() (string, error) {
	s, err := randomBase64URL(stateBytes)
	if err != nil {
		return "", fmt.Errorf("state 생성 실패: %w", err)
	}
	return s, nil
}

func randomBase64URL(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// BuildAuthorizeURL은 브라우저로 보낼 인가 URL을 만든다.
//
// redirectURI는 콜백을 받을 로컬 주소(예: `http://localhost:54321/auth/callback`).
// OpenAI의 client_id는 localhost 임의 포트를 허용하도록 등록되어 있어 — 공식 codex CLI도
// 동적 포트를 사용 — 1455 같은 고정 포트가 점유돼도 충돌하지 않도록 동적으로 받는다.
//
// id_token_add_organizations / codex_cli_simplified_flow 는 OpenAI가 Codex CLI에
// 부여한 비공식 플래그로, 이걸 빠뜨리면 ChatGPT 구독 라우팅이 실패할 수 있다 —
// raine 구현에서 검증된 값을 그대로 따른다.
func BuildAuthorizeURL(pkce PKCECodes, state, redirectURI string) string {
	q := url.Values{
		"response_type":              {"code"},
		"client_id":                  {ClientID},
		"redirect_uri":               {redirectURI},
		"scope":                      {"openid profile email offline_access api.connectors.read api.connectors.invoke"},
		"code_challenge":             {pkce.Challenge},
		"code_challenge_method":      {"S256"},
		"id_token_add_organizations": {"true"},
		"codex_cli_simplified_flow":  {"true"},
		"state":                      {state},
		"originator":                 {Originator},
	}
	return Issuer + "/oauth/authorize?" + q.Encode()
}
