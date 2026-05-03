package codex

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

// makeJWT는 서명되지 않은 더미 JWT를 만든다. 우리 코드는 서명 검증을 하지 않으므로 충분.
func makeJWT(payload map[string]any) string {
	header, _ := json.Marshal(map[string]string{"alg": "none", "typ": "JWT"})
	body, _ := json.Marshal(payload)
	enc := base64.RawURLEncoding
	return enc.EncodeToString(header) + "." + enc.EncodeToString(body) + ".signature"
}

func TestExtractAccountID_FromIDTokenFlatClaim(t *testing.T) {
	idTok := makeJWT(map[string]any{"chatgpt_account_id": "acct_123"})
	got := ExtractAccountID(TokenResponse{IDToken: idTok})
	if got != "acct_123" {
		t.Errorf("got %q, want acct_123", got)
	}
}

func TestExtractAccountID_FromNamespaceClaim(t *testing.T) {
	idTok := makeJWT(map[string]any{
		"https://api.openai.com/auth": map[string]string{"chatgpt_account_id": "acct_ns"},
	})
	got := ExtractAccountID(TokenResponse{IDToken: idTok})
	if got != "acct_ns" {
		t.Errorf("got %q, want acct_ns", got)
	}
}

func TestExtractAccountID_FallsBackToOrganizations(t *testing.T) {
	idTok := makeJWT(map[string]any{
		"organizations": []map[string]string{{"id": "org_xyz"}},
	})
	got := ExtractAccountID(TokenResponse{IDToken: idTok})
	if got != "org_xyz" {
		t.Errorf("got %q, want org_xyz", got)
	}
}

func TestExtractAccountID_FallsBackToAccessToken(t *testing.T) {
	accessTok := makeJWT(map[string]any{"chatgpt_account_id": "acct_access"})
	got := ExtractAccountID(TokenResponse{AccessToken: accessTok})
	if got != "acct_access" {
		t.Errorf("got %q, want acct_access", got)
	}
}

func TestExtractAccountID_NoClaims(t *testing.T) {
	got := ExtractAccountID(TokenResponse{AccessToken: "not.a.jwt"})
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestParseJWTClaims_HandlesMalformed(t *testing.T) {
	if _, ok := parseJWTClaims(""); ok {
		t.Error("empty 토큰이 ok=true 반환")
	}
	if _, ok := parseJWTClaims("only.two"); ok {
		t.Error("부분 segment 토큰이 ok=true 반환")
	}
	if _, ok := parseJWTClaims("a.!!notbase64!!.c"); ok {
		t.Error("invalid base64가 ok=true 반환")
	}
}
