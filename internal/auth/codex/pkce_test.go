package codex

import (
	"crypto/sha256"
	"encoding/base64"
	"net/url"
	"strings"
	"testing"
)

func TestGeneratePKCE_VerifierAndChallengeMatch(t *testing.T) {
	pkce, err := GeneratePKCE()
	if err != nil {
		t.Fatal(err)
	}
	if len(pkce.Verifier) < 43 || len(pkce.Verifier) > 128 {
		t.Fatalf("verifier 길이 RFC 7636 범위 벗어남: %d", len(pkce.Verifier))
	}
	// challenge는 SHA-256(verifier)의 base64url.
	sum := sha256.Sum256([]byte(pkce.Verifier))
	want := base64.RawURLEncoding.EncodeToString(sum[:])
	if pkce.Challenge != want {
		t.Fatalf("challenge가 SHA-256(verifier)와 다름")
	}
}

func TestGeneratePKCE_NoPadding(t *testing.T) {
	pkce, err := GeneratePKCE()
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(pkce.Verifier, "=+/") {
		t.Errorf("verifier에 base64url 외 문자 포함: %q", pkce.Verifier)
	}
	if strings.ContainsAny(pkce.Challenge, "=+/") {
		t.Errorf("challenge에 base64url 외 문자 포함: %q", pkce.Challenge)
	}
}

func TestGeneratePKCE_Unique(t *testing.T) {
	a, _ := GeneratePKCE()
	b, _ := GeneratePKCE()
	if a.Verifier == b.Verifier {
		t.Fatal("연속 호출이 같은 verifier 생성")
	}
}

func TestBuildAuthorizeURL_AllRequiredParams(t *testing.T) {
	pkce := PKCECodes{Verifier: "v", Challenge: "c"}
	const redirectURI = "http://localhost:54321/auth/callback"
	u := BuildAuthorizeURL(pkce, "S", redirectURI)
	parsed, err := url.Parse(u)
	if err != nil {
		t.Fatal(err)
	}
	q := parsed.Query()

	want := map[string]string{
		"response_type":              "code",
		"client_id":                  ClientID,
		"redirect_uri":               redirectURI,
		"scope":                      "openid profile email offline_access api.connectors.read api.connectors.invoke",
		"code_challenge":             "c",
		"code_challenge_method":      "S256",
		"id_token_add_organizations": "true",
		"codex_cli_simplified_flow":  "true",
		"state":                      "S",
		"originator":                 Originator,
	}
	for k, v := range want {
		if got := q.Get(k); got != v {
			t.Errorf("param %s: got %q, want %q", k, got, v)
		}
	}
	if !strings.HasPrefix(u, Issuer+"/oauth/authorize?") {
		t.Errorf("authorize URL이 issuer로 시작하지 않음: %s", u)
	}
}

func TestGenerateState_Unique(t *testing.T) {
	a, _ := GenerateState()
	b, _ := GenerateState()
	if a == b {
		t.Fatal("연속 state가 동일")
	}
	if len(a) < 43 {
		t.Errorf("state 길이 너무 짧음: %d", len(a))
	}
}
