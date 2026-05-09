package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// listenCallback은 1455를 우선 시도하고, 실패하면 1457로 fallback한다.
// OpenAI가 두 포트만 redirect_uri로 허용하기 때문에 :0(동적 포트)는 사용할 수 없다.
func listenCallback() (net.Listener, int, error) {
	for _, port := range []int{OAuthPrimaryPort, OAuthFallbackPort} {
		l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			return l, port, nil
		}
	}
	return nil, 0, fmt.Errorf("OAuth callback ports %d and %d are both in use. "+
		"Run `lsof -iTCP:%d -sTCP:LISTEN` to find and stop the process, or "+
		"use `ccx codex login --device` for the device-code flow",
		OAuthPrimaryPort, OAuthFallbackPort, OAuthPrimaryPort)
}

// httpClient는 OAuth 엔드포인트로 요청을 보낼 때 사용하는 공유 클라이언트.
// 타임아웃을 짧게 잡아 네트워크 이슈에서 사용자가 무한 대기하지 않도록 함.
var httpClient = &http.Client{Timeout: 30 * time.Second}

// ExchangeCodeForTokens는 authorization_code grant로 액세스 토큰을 받는다.
// redirectURI는 인가 URL을 만들 때 사용한 그 값과 정확히 동일해야 한다 — OpenAI가
// 토큰 교환 시 redirect_uri 일치를 검증한다.
func ExchangeCodeForTokens(ctx context.Context, code, redirectURI string, pkce PKCECodes) (TokenResponse, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {ClientID},
		"code_verifier": {pkce.Verifier},
	}
	return postTokenForm(ctx, form)
}

// RefreshTokens는 refresh_token grant로 토큰을 갱신한다.
func RefreshTokens(ctx context.Context, refreshToken string) (TokenResponse, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {ClientID},
	}
	return postTokenForm(ctx, form)
}

func postTokenForm(ctx context.Context, form url.Values) (TokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", Issuer+"/oauth/token", strings.NewReader(form.Encode()))
	if err != nil {
		return TokenResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := httpClient.Do(req)
	if err != nil {
		return TokenResponse{}, fmt.Errorf("token endpoint request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return TokenResponse{}, fmt.Errorf("token exchange failed (%d): %s", resp.StatusCode, string(body))
	}
	var tok TokenResponse
	if err := json.Unmarshal(body, &tok); err != nil {
		return TokenResponse{}, fmt.Errorf("failed to parse token response: %w", err)
	}
	if tok.AccessToken == "" {
		return TokenResponse{}, errors.New("token response missing access_token")
	}
	return tok, nil
}

// BrowserLoginPrinter는 사용자에게 인가 URL을 어떻게 보여줄지 결정.
// 기본은 stdout 출력 + 가능하면 OS의 기본 브라우저 자동 오픈.
type BrowserLoginPrinter func(authURL string)

// RunBrowserLogin은 PKCE 플로우를 끝까지 수행해 토큰을 반환한다.
//
// 콜백 포트는 1455 우선, 점유되어 있으면 1457 fallback. OpenAI client_id의 redirect_uri
// 화이트리스트가 이 두 포트뿐이라 임의 포트는 OpenAI가 unknown_error로 거부한다.
// 둘 다 점유면 lsof 안내와 함께 에러를 반환한다 — `ccx codex login --device` 가 우회로.
//
// 5분 timeout, state 검증 포함.
func RunBrowserLogin(ctx context.Context, printURL BrowserLoginPrinter) (TokenResponse, error) {
	pkce, err := GeneratePKCE()
	if err != nil {
		return TokenResponse{}, err
	}
	state, err := GenerateState()
	if err != nil {
		return TokenResponse{}, err
	}

	listener, port, err := listenCallback()
	if err != nil {
		return TokenResponse{}, err
	}
	redirectURI := fmt.Sprintf("http://localhost:%d/auth/callback", port)
	authURL := BuildAuthorizeURL(pkce, state, redirectURI)

	type result struct {
		tok TokenResponse
		err error
	}
	done := make(chan result, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/auth/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if e := q.Get("error"); e != "" {
			http.Error(w, "Auth failed: "+e, http.StatusBadRequest)
			done <- result{err: fmt.Errorf("OAuth denied: %s", e)}
			return
		}
		code := q.Get("code")
		if code == "" || q.Get("state") != state {
			http.Error(w, "Invalid callback", http.StatusBadRequest)
			done <- result{err: errors.New("invalid OAuth callback (state mismatch or missing code)")}
			return
		}
		// 토큰 교환은 timeout이 짧아도 되도록 별도 context.
		exCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		tok, err := ExchangeCodeForTokens(exCtx, code, redirectURI, pkce)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			done <- result{err: err}
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><html><body style="font-family:sans-serif;text-align:center;padding:48px">
<h1>Authentication successful</h1><p>You can close this window and return to the terminal.</p></body></html>`))
		done <- result{tok: tok}
	})

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		_ = srv.Serve(listener)
	}()
	defer func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = srv.Shutdown(shutCtx)
		cancel()
	}()

	if printURL != nil {
		printURL(authURL)
	} else {
		fmt.Printf("\nOpen the following URL in a browser to authenticate:\n\n  %s\n\n", authURL)
	}

	timeout := time.NewTimer(5 * time.Minute)
	defer timeout.Stop()

	select {
	case <-ctx.Done():
		return TokenResponse{}, ctx.Err()
	case <-timeout.C:
		return TokenResponse{}, errors.New("OAuth timed out after 5 minutes")
	case r := <-done:
		return r.tok, r.err
	}
}
