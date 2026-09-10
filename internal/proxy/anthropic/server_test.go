package anthropic

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type capture struct {
	body   []byte
	header http.Header
	path   string
}

func newUpstream(t *testing.T, got *capture, respond func(w http.ResponseWriter)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got.body, got.header, got.path = b, r.Header.Clone(), r.URL.Path
		respond(w)
	}))
}

func startProxy(t *testing.T, upstream string, normalize bool) *Server {
	t.Helper()
	s, err := Start(ServerOptions{
		UpstreamBaseURL: upstream,
		UpstreamAuth:    "upstream-token",
		NormalizeSystem: normalize,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Shutdown(t.Context()) })
	return s
}

func post(t *testing.T, s *Server, path, body string, hdr map[string]string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, s.baseURL()+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func (s *Server) baseURL() string { return "http://" + s.listener.Addr().String() }

const withMidSystem = `{"model":"m","messages":[
	{"role":"user","content":"a"},
	{"role":"system","content":"env"}]}`

func TestProxyNormalizesMidConversationSystem(t *testing.T) {
	var got capture
	up := newUpstream(t, &got, func(w http.ResponseWriter) { _, _ = w.Write([]byte(`{"ok":true}`)) })
	defer up.Close()
	s := startProxy(t, up.URL, true)

	resp := post(t, s, "/v1/messages", withMidSystem, nil)
	defer resp.Body.Close()

	var env struct {
		Messages []struct {
			Role string `json:"role"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(got.body, &env); err != nil {
		t.Fatalf("업스트림이 받은 본문 파싱 실패: %v", err)
	}
	if env.Messages[1].Role != "user" {
		t.Errorf("중간 system이 정규화되지 않았다: %q", env.Messages[1].Role)
	}
	if got.path != "/v1/messages" {
		t.Errorf("업스트림 경로 = %q", got.path)
	}
}

func TestProxyLeavesBodyAloneWhenDisabled(t *testing.T) {
	var got capture
	up := newUpstream(t, &got, func(w http.ResponseWriter) { _, _ = w.Write([]byte(`{}`)) })
	defer up.Close()
	s := startProxy(t, up.URL, false)

	resp := post(t, s, "/v1/messages", withMidSystem, nil)
	defer resp.Body.Close()

	if string(got.body) != withMidSystem {
		t.Errorf("정규화를 껐는데 본문이 바뀌었다:\n%s", got.body)
	}
}

func TestProxyForwardsClientHeadersAndSwapsCredential(t *testing.T) {
	var got capture
	up := newUpstream(t, &got, func(w http.ResponseWriter) { _, _ = w.Write([]byte(`{}`)) })
	defer up.Close()
	s := startProxy(t, up.URL, true)

	resp := post(t, s, "/v1/messages", withMidSystem, map[string]string{
		"anthropic-version": "2023-06-01",
		"anthropic-beta":    "claude-code-20250219",
		"x-session-id":      "ccx-abc",
		"Authorization":     "Bearer local-secret",
	})
	defer resp.Body.Close()

	// 업스트림 동작을 좌우하는 헤더는 보존돼야 한다.
	for k, want := range map[string]string{
		"Anthropic-Version": "2023-06-01",
		"Anthropic-Beta":    "claude-code-20250219",
		"X-Session-Id":      "ccx-abc",
	} {
		if got.header.Get(k) != want {
			t.Errorf("%s = %q, want %q", k, got.header.Get(k), want)
		}
	}
	// 로컬 프록시용 자격증명은 업스트림 것으로 교체돼야 한다.
	if got.header.Get("Authorization") != "Bearer upstream-token" {
		t.Errorf("Authorization = %q", got.header.Get("Authorization"))
	}
}

func TestProxyStreamsResponseThrough(t *testing.T) {
	var got capture
	up := newUpstream(t, &got, func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_start\ndata: {}\n\nevent: message_stop\ndata: {}\n\n"))
	})
	defer up.Close()
	s := startProxy(t, up.URL, true)

	resp := post(t, s, "/v1/messages", withMidSystem, nil)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "message_start") || !strings.Contains(string(body), "message_stop") {
		t.Errorf("SSE가 그대로 전달되지 않았다: %q", body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q", ct)
	}
}

func TestProxyRejectsWrongSecret(t *testing.T) {
	var got capture
	up := newUpstream(t, &got, func(w http.ResponseWriter) { _, _ = w.Write([]byte(`{}`)) })
	defer up.Close()
	s, err := Start(ServerOptions{UpstreamBaseURL: up.URL, SharedSecret: "right"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Shutdown(t.Context()) }()

	resp := post(t, s, "/v1/messages", withMidSystem, map[string]string{"Authorization": "Bearer wrong"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
	if got.body != nil {
		t.Error("인증 실패인데 업스트림으로 전달됐다")
	}
}
