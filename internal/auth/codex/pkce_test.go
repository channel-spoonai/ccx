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
		t.Fatalf("verifier length out of RFC 7636 range: %d", len(pkce.Verifier))
	}
	// challenge는 SHA-256(verifier)의 base64url.
	sum := sha256.Sum256([]byte(pkce.Verifier))
	want := base64.RawURLEncoding.EncodeToString(sum[:])
	if pkce.Challenge != want {
		t.Fatalf("challenge does not match SHA-256(verifier)")
	}
}

func TestGeneratePKCE_NoPadding(t *testing.T) {
	pkce, err := GeneratePKCE()
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(pkce.Verifier, "=+/") {
		t.Errorf("verifier contains non-base64url characters: %q", pkce.Verifier)
	}
	if strings.ContainsAny(pkce.Challenge, "=+/") {
		t.Errorf("challenge contains non-base64url characters: %q", pkce.Challenge)
	}
}

func TestGeneratePKCE_Unique(t *testing.T) {
	a, _ := GeneratePKCE()
	b, _ := GeneratePKCE()
	if a.Verifier == b.Verifier {
		t.Fatal("consecutive calls produced the same verifier")
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
		t.Errorf("authorize URL does not start with issuer: %s", u)
	}
}

func TestGenerateState_Unique(t *testing.T) {
	a, _ := GenerateState()
	b, _ := GenerateState()
	if a == b {
		t.Fatal("consecutive states are identical")
	}
	if len(a) < 43 {
		t.Errorf("state length too short: %d", len(a))
	}
}
