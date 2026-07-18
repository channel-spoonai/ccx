package codex

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// withMockOAuth는 OpenAI /oauth/token 엔드포인트를 모킹한다.
// httpClient의 Transport를 일시적으로 교체하지 않고, 대신 테스트 서버 URL로
// 요청을 가게 하기 위해 RoundTripper를 swap한다.
func withMockOAuth(t *testing.T, handler http.HandlerFunc) (*httptest.Server, func()) {
	t.Helper()
	srv := httptest.NewServer(handler)
	orig := httpClient.Transport
	httpClient.Transport = &rewriteTransport{base: orig, target: srv.URL}
	cleanup := func() {
		httpClient.Transport = orig
		srv.Close()
	}
	return srv, cleanup
}

// rewriteTransport는 모든 요청의 호스트를 mock server로 돌려보낸다 (issuer는 그대로 유지).
type rewriteTransport struct {
	base   http.RoundTripper
	target string
}

func (r *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	u, _ := url.Parse(r.target)
	req2 := req.Clone(req.Context())
	req2.URL.Scheme = u.Scheme
	req2.URL.Host = u.Host
	req2.Host = u.Host
	base := r.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(req2)
}

func TestManager_GetAccessToken_NotAuthenticated(t *testing.T) {
	withTempHome(t)
	m := NewManager()
	_, err := m.GetAccessToken(context.Background())
	if err != ErrNotAuthenticated {
		t.Fatalf("expected ErrNotAuthenticated, got %v", err)
	}
}

func TestManager_GetAccessToken_FreshTokenReturnsImmediately(t *testing.T) {
	withTempHome(t)
	auth := &StoredAuth{
		AccessToken:  "fresh",
		RefreshToken: "r",
		ExpiresAt:    time.Now().Add(time.Hour),
	}
	if err := SaveAuth(auth); err != nil {
		t.Fatal(err)
	}
	m := NewManager()
	tok, err := m.GetAccessToken(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if tok != "fresh" {
		t.Errorf("got %q, want fresh", tok)
	}
}

func TestManager_AutoRefreshNearExpiry(t *testing.T) {
	withTempHome(t)
	expired := &StoredAuth{
		AccessToken:  "old",
		RefreshToken: "rtk",
		ExpiresAt:    time.Now().Add(2 * time.Minute), // 마진 5분 안쪽
	}
	if err := SaveAuth(expired); err != nil {
		t.Fatal(err)
	}

	var refreshCount int32
	_, cleanup := withMockOAuth(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&refreshCount, 1)
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "grant_type=refresh_token") {
			t.Errorf("expected refresh_token grant, got %s", body)
		}
		if !strings.Contains(string(body), "refresh_token=rtk") {
			t.Errorf("existing refresh_token was not forwarded: %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"new","refresh_token":"rtk2","expires_in":3600}`))
	})
	defer cleanup()

	m := NewManager()
	tok, err := m.GetAccessToken(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if tok != "new" {
		t.Errorf("got %q, want new (token should have been refreshed)", tok)
	}
	if atomic.LoadInt32(&refreshCount) != 1 {
		t.Errorf("refresh call count: got %d, want 1", refreshCount)
	}

	// 디스크에도 새 토큰이 저장됐는지 확인.
	stored, _ := LoadAuth()
	if stored == nil || stored.AccessToken != "new" || stored.RefreshToken != "rtk2" {
		t.Errorf("disk token was not updated: %+v", stored)
	}
}

func TestManager_ConcurrentGetCallsRefreshOnce(t *testing.T) {
	withTempHome(t)
	expired := &StoredAuth{
		AccessToken:  "old",
		RefreshToken: "rtk",
		ExpiresAt:    time.Now().Add(time.Minute),
	}
	_ = SaveAuth(expired)

	var refreshCount int32
	_, cleanup := withMockOAuth(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&refreshCount, 1)
		// 짧은 지연으로 동시성 노출.
		time.Sleep(50 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"new","refresh_token":"rtk","expires_in":3600}`))
	})
	defer cleanup()

	m := NewManager()
	var wg sync.WaitGroup
	tokens := make([]string, 10)
	errs := make([]error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tok, err := m.GetAccessToken(context.Background())
			tokens[i] = tok
			errs[i] = err
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("call %d: %v", i, err)
		}
		if tokens[i] != "new" {
			t.Errorf("call %d: got %q, want new", i, tokens[i])
		}
	}
	if got := atomic.LoadInt32(&refreshCount); got != 1 {
		t.Errorf("concurrent calls refreshed %d times (single-flight broken, want 1)", got)
	}
}

func TestManager_PersistInitial_SavesAndCaches(t *testing.T) {
	withTempHome(t)
	m := NewManager()
	if err := m.PersistInitial(TokenResponse{
		AccessToken:  "a",
		RefreshToken: "r",
		ExpiresIn:    3600,
	}); err != nil {
		t.Fatal(err)
	}
	// 캐시에서 즉시 가져와야 함 (디스크 read 없이).
	tok, err := m.GetAccessToken(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if tok != "a" {
		t.Errorf("got %q, want a", tok)
	}
	stored, _ := LoadAuth()
	if stored == nil || stored.AccessToken != "a" {
		t.Errorf("not saved to disk: %+v", stored)
	}
}
