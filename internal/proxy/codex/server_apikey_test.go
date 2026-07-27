package codex

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// API 키 모드: OAuth 토큰 없이 Bearer <apiKey>만 보내고 ChatGPT 전용 헤더는 생략해야 한다.
func TestServer_APIKeyMode_HeadersAndAuth(t *testing.T) {
	withTempHome(t) // 디스크에 OAuth 토큰이 없는 상태 — API 키 모드는 이에 의존하면 안 된다.

	var gotAuth, gotOriginator, gotAccount, gotSession atomic.Value
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth.Store(r.Header.Get("Authorization"))
		gotOriginator.Store(r.Header.Get("originator"))
		gotAccount.Store(r.Header.Get("ChatGPT-Account-Id"))
		gotSession.Store(r.Header.Get("session_id"))
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, codexSSE(
			ev("response.completed", `{"type":"response.completed","response":{}}`),
		))
	}))
	defer mock.Close()

	s, err := Start(ServerOptions{Upstream: UpstreamConfig{Endpoint: mock.URL, APIKey: "sk-test-123"}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Shutdown(context.Background())

	req, _ := http.NewRequest("POST", "http://"+s.listener.Addr().String()+"/v1/messages",
		strings.NewReader(`{"model":"gpt-5.6-sol[1m]","messages":[],"stream":true}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-claude-code-session-id", "sess-abc")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}

	if got := gotAuth.Load(); got != "Bearer sk-test-123" {
		t.Errorf("Authorization = %q, want Bearer sk-test-123", got)
	}
	if got := gotOriginator.Load(); got != "" {
		t.Errorf("originator 헤더가 API 키 모드에서 전송됨: %q", got)
	}
	if got := gotAccount.Load(); got != "" {
		t.Errorf("ChatGPT-Account-Id 헤더가 API 키 모드에서 전송됨: %q", got)
	}
	if got := gotSession.Load(); got != "" {
		t.Errorf("session_id 헤더가 API 키 모드에서 전송됨: %q", got)
	}
}

// API 키 모드의 업스트림 401은 refresh 재시도 없이 한 번만 호출되고,
// API 키 안내 문구로 surface되어야 한다.
func TestServer_APIKeyMode_Upstream401NoRetry(t *testing.T) {
	withTempHome(t)

	var calls atomic.Int32
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(401)
		_, _ = io.WriteString(w, `{"error":{"message":"Incorrect API key provided"}}`)
	}))
	defer mock.Close()

	s, err := Start(ServerOptions{Upstream: UpstreamConfig{Endpoint: mock.URL, APIKey: "sk-bad"}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Shutdown(context.Background())

	req, _ := http.NewRequest("POST", "http://"+s.listener.Addr().String()+"/v1/messages",
		strings.NewReader(`{"model":"gpt-5.6-terra","messages":[]}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("status %d, want 401", resp.StatusCode)
	}
	var body struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Type != "authentication_error" {
		t.Errorf("error.type = %q", body.Error.Type)
	}
	if !strings.Contains(body.Error.Message, "apiKey") {
		t.Errorf("에러 메시지에 apiKey 안내가 없음: %q", body.Error.Message)
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("업스트림 호출 %d회 — API 키 모드는 401 재시도가 없어야 함", n)
	}
}

// API 키 모드의 403은 Codex/ChatGPT를 연상시키지 않는 메시지여야 한다.
func TestServer_APIKeyMode_Upstream403Message(t *testing.T) {
	withTempHome(t)

	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		_, _ = io.WriteString(w, `{"error":{"message":"Project does not have access to model"}}`)
	}))
	defer mock.Close()

	s, err := Start(ServerOptions{Upstream: UpstreamConfig{Endpoint: mock.URL, APIKey: "sk-x"}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Shutdown(context.Background())

	req, _ := http.NewRequest("POST", "http://"+s.listener.Addr().String()+"/v1/messages",
		strings.NewReader(`{"model":"gpt-5.6-terra","messages":[]}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatalf("status %d, want 403", resp.StatusCode)
	}
	var body struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Type != "permission_error" {
		t.Errorf("error.type = %q", body.Error.Type)
	}
	if strings.Contains(body.Error.Message, "Codex") {
		t.Errorf("API 키 모드 403 메시지에 Codex 표기가 포함됨: %q", body.Error.Message)
	}
}
