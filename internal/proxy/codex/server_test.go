package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	auth "github.com/channel-spoonai/ccx/internal/auth/codex"
)

// withTempHome은 auth 패키지가 테스트 동안 임시 경로를 쓰도록 환경변수를 돌려준다.
func withTempHome(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
}

// seedAuth는 디스크에 fresh 토큰을 심는다 (Manager가 자동으로 로드).
func seedAuth(t *testing.T, accessToken string) {
	t.Helper()
	if err := auth.SaveAuth(&auth.StoredAuth{
		AccessToken:  accessToken,
		RefreshToken: "rtk",
		ExpiresAt:    time.Now().Add(time.Hour),
		AccountID:    "acct_test",
	}); err != nil {
		t.Fatal(err)
	}
}

// rerouteUpstream은 forward.go의 upstreamClient Transport를 swap해
// 모든 요청이 mockServer로 가도록 한다. 테스트 종료 시 원복.
func rerouteUpstream(t *testing.T, target string) {
	t.Helper()
	orig := upstreamClient.Transport
	u, _ := url.Parse(target)
	upstreamClient.Transport = &rewriteRT{base: orig, host: u.Host, scheme: u.Scheme}
	t.Cleanup(func() { upstreamClient.Transport = orig })
}

type rewriteRT struct {
	base   http.RoundTripper
	host   string
	scheme string
}

func (r *rewriteRT) RoundTrip(req *http.Request) (*http.Response, error) {
	req2 := req.Clone(req.Context())
	req2.URL.Scheme = r.scheme
	req2.URL.Host = r.host
	req2.Host = r.host
	base := r.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(req2)
}

// codexSSE는 mock Codex 서버용 SSE 응답 builder.
func codexSSE(events ...string) string {
	var b strings.Builder
	for _, e := range events {
		b.WriteString(e)
		b.WriteString("\n\n")
	}
	return b.String()
}

func ev(typ, data string) string {
	return "event: " + typ + "\ndata: " + data
}

func TestServer_HealthCheck(t *testing.T) {
	withTempHome(t)
	s, err := Start(ServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Shutdown(context.Background())

	resp, err := http.Get("http://" + s.listener.Addr().String() + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("status: %d", resp.StatusCode)
	}
}

func TestServer_RejectsWithoutSharedSecret(t *testing.T) {
	withTempHome(t)
	seedAuth(t, "atk")
	s, _ := Start(ServerOptions{SharedSecret: "secret123"})
	defer s.Shutdown(context.Background())

	req, _ := http.NewRequest("POST", "http://"+s.listener.Addr().String()+"/v1/messages",
		strings.NewReader(`{"model":"gpt-5.4","messages":[]}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Errorf("got %d, want 401", resp.StatusCode)
	}
}

func TestServer_AcceptsBearerSharedSecret(t *testing.T) {
	withTempHome(t)
	seedAuth(t, "atk")

	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, codexSSE(
			ev("response.completed", `{"type":"response.completed","response":{}}`),
		))
	}))
	defer mock.Close()
	rerouteUpstream(t, mock.URL)

	s, _ := Start(ServerOptions{SharedSecret: "topsecret"})
	defer s.Shutdown(context.Background())

	req, _ := http.NewRequest("POST", "http://"+s.listener.Addr().String()+"/v1/messages",
		strings.NewReader(`{"model":"gpt-5.4","messages":[],"stream":true}`))
	req.Header.Set("Authorization", "Bearer topsecret")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("got %d: %s", resp.StatusCode, body)
	}
}

func TestServer_StreamingRoundTrip(t *testing.T) {
	withTempHome(t)
	seedAuth(t, "atk")

	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 헤더 검증.
		if r.Header.Get("Authorization") != "Bearer atk" {
			t.Errorf("Authorization 헤더: %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("ChatGPT-Account-Id") != "acct_test" {
			t.Errorf("ChatGPT-Account-Id: %q", r.Header.Get("ChatGPT-Account-Id"))
		}
		if r.Header.Get("originator") != "claude-code-proxy" {
			t.Errorf("originator: %q", r.Header.Get("originator"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, codexSSE(
			ev("response.output_item.added", `{"type":"response.output_item.added","output_index":0,"item":{"type":"message"}}`),
			ev("response.output_text.delta", `{"type":"response.output_text.delta","output_index":0,"delta":"hi"}`),
			ev("response.output_item.done", `{"type":"response.output_item.done","output_index":0,"item":{"type":"message"}}`),
			ev("response.completed", `{"type":"response.completed","response":{"usage":{"input_tokens":3,"output_tokens":1}}}`),
		))
	}))
	defer mock.Close()
	rerouteUpstream(t, mock.URL)

	s, _ := Start(ServerOptions{})
	defer s.Shutdown(context.Background())

	body, _ := json.Marshal(map[string]any{
		"model":    "gpt-5.4",
		"messages": []map[string]any{{"role": "user", "content": "hi"}},
		"stream":   true,
	})
	resp, err := http.Post("http://"+s.listener.Addr().String()+"/v1/messages",
		"application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Errorf("Content-Type: %q", ct)
	}
	all, _ := io.ReadAll(resp.Body)
	got := string(all)
	for _, want := range []string{
		"event: message_start",
		`"type":"text_delta"`,
		"event: message_stop",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("응답에 %q 누락:\n%s", want, got)
		}
	}
}

func TestServer_NonStreamingReturnsJSON(t *testing.T) {
	withTempHome(t)
	seedAuth(t, "atk")
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, codexSSE(
			ev("response.output_item.added", `{"type":"response.output_item.added","output_index":0,"item":{"type":"message"}}`),
			ev("response.output_text.delta", `{"type":"response.output_text.delta","output_index":0,"delta":"answer"}`),
			ev("response.output_item.done", `{"type":"response.output_item.done","output_index":0,"item":{"type":"message"}}`),
			ev("response.completed", `{"type":"response.completed","response":{}}`),
		))
	}))
	defer mock.Close()
	rerouteUpstream(t, mock.URL)

	s, _ := Start(ServerOptions{})
	defer s.Shutdown(context.Background())

	body := strings.NewReader(`{"model":"gpt-5.4","messages":[]}`)
	resp, err := http.Post("http://"+s.listener.Addr().String()+"/v1/messages", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type: %q", ct)
	}
	var out struct {
		Type    string `json:"type"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out.Type != "message" || len(out.Content) != 1 || out.Content[0].Text != "answer" {
		t.Errorf("응답 구조 잘못됨: %+v", out)
	}
}

func TestServer_StripsContextSuffix(t *testing.T) {
	withTempHome(t)
	seedAuth(t, "atk")
	var seenModel string
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Model string `json:"model"` }
		_ = json.NewDecoder(r.Body).Decode(&body)
		seenModel = body.Model
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, codexSSE(ev("response.completed", `{"type":"response.completed","response":{}}`)))
	}))
	defer mock.Close()
	rerouteUpstream(t, mock.URL)

	s, _ := Start(ServerOptions{})
	defer s.Shutdown(context.Background())

	body := strings.NewReader(`{"model":"gpt-5.4[1m]","messages":[]}`)
	_, _ = http.Post("http://"+s.listener.Addr().String()+"/v1/messages", "application/json", body)
	if seenModel != "gpt-5.4" {
		t.Errorf("[1m] 접미사가 strip되지 않음: %q", seenModel)
	}
}

func TestServer_RateLimitFromUpstream(t *testing.T) {
	withTempHome(t)
	seedAuth(t, "atk")
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "5")
		w.WriteHeader(429)
		_, _ = w.Write([]byte("slow down"))
	}))
	defer mock.Close()
	rerouteUpstream(t, mock.URL)

	s, _ := Start(ServerOptions{})
	defer s.Shutdown(context.Background())

	body := strings.NewReader(`{"model":"gpt-5.4","messages":[]}`)
	resp, err := http.Post("http://"+s.listener.Addr().String()+"/v1/messages", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 429 {
		t.Errorf("got %d, want 429", resp.StatusCode)
	}
	if resp.Header.Get("Retry-After") != "5" {
		t.Errorf("Retry-After 누락: %q", resp.Header.Get("Retry-After"))
	}
}

func TestServer_CountTokensReturnsEstimate(t *testing.T) {
	withTempHome(t)
	s, _ := Start(ServerOptions{})
	defer s.Shutdown(context.Background())

	body := strings.NewReader(`{"model":"gpt-5.4","messages":[{"role":"user","content":"hello world"}]}`)
	resp, err := http.Post("http://"+s.listener.Addr().String()+"/v1/messages/count_tokens",
		"application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]int
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out["input_tokens"] < 1 {
		t.Errorf("input_tokens > 0 이어야 함: %+v", out)
	}
}

func TestServer_NotAuthenticatedReturns401(t *testing.T) {
	withTempHome(t)
	// 토큰 파일을 고의로 만들지 않음.
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("upstream으로 가지 말아야 함")
	}))
	defer mock.Close()
	rerouteUpstream(t, mock.URL)

	s, _ := Start(ServerOptions{})
	defer s.Shutdown(context.Background())
	body := strings.NewReader(`{"model":"gpt-5.4","messages":[]}`)
	resp, err := http.Post("http://"+s.listener.Addr().String()+"/v1/messages", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Errorf("got %d, want 401", resp.StatusCode)
	}
}

func TestStripContextSuffix(t *testing.T) {
	cases := map[string]string{
		"gpt-5.4":      "gpt-5.4",
		"gpt-5.4[1m]":  "gpt-5.4",
		"gpt-5.4[1M]":  "gpt-5.4",
		"gpt-5.4[200k]": "gpt-5.4",
		"gpt-5.4[other]": "gpt-5.4[other]",
		"":              "",
	}
	for in, want := range cases {
		if got := stripContextSuffix(in); got != want {
			t.Errorf("stripContextSuffix(%q) = %q, want %q", in, got, want)
		}
	}
}

