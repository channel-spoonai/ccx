package anthropic

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	tr "github.com/channel-spoonai/ccx/internal/translate/anthropic"
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

// 세션 헤더는 "이 세션 요청은 직렬화된다"는 약속이라, 겹쳐 보내면 업스트림이 409로 거절한다
// (MTPLX engine_session.generation_slot). Claude Code는 서브에이전트로 동시 요청을 내므로
// 겹치는 쪽에서는 헤더를 빼야 한다 — 그래야 업스트림이 암묵 세션으로 갈라 처리한다.
func TestProxyDropsSessionHeaderWhenConcurrent(t *testing.T) {
	release := make(chan struct{})
	var mu sync.Mutex
	var seen []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Header.Get("X-Session-Id"))
		first := len(seen) == 1
		mu.Unlock()
		if first {
			<-release // 첫 요청을 붙잡아 둘째와 겹치게 만든다
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer up.Close()

	s, err := Start(ServerOptions{UpstreamBaseURL: up.URL, SessionHeader: "x-session-id"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Shutdown(t.Context()) }()

	hdr := map[string]string{"x-session-id": "ccx-abc"}
	done := make(chan struct{})
	go func() {
		defer close(done)
		resp := post(t, s, "/v1/messages", withMidSystem, hdr)
		_ = resp.Body.Close()
	}()

	// 첫 요청이 업스트림에 도달할 때까지 기다렸다가 둘째를 보낸다.
	for i := 0; i < 100; i++ {
		mu.Lock()
		n := len(seen)
		mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	resp := post(t, s, "/v1/messages", withMidSystem, hdr)
	_ = resp.Body.Close()
	close(release)
	<-done

	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 {
		t.Fatalf("업스트림 요청 %d건, want 2", len(seen))
	}
	if !strings.HasPrefix(seen[0], "ccx-abc-") {
		t.Errorf("첫 요청은 대화별로 좁힌 세션 id를 유지해야 한다: %q", seen[0])
	}
	if seen[1] != "" {
		t.Errorf("겹친 요청은 세션 id를 빼야 한다: %q", seen[1])
	}
}

// 슬롯은 응답 스트림이 끝나면 풀려야 한다 — 안 그러면 이후 모든 턴이 헤더 없이 나가
// 캐시 재사용이 통째로 사라진다.
func TestProxyReleasesSessionSlotAfterResponse(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Header.Get("X-Session-Id"))
		mu.Unlock()
		_, _ = w.Write([]byte(`{}`))
	}))
	defer up.Close()

	s, err := Start(ServerOptions{UpstreamBaseURL: up.URL, SessionHeader: "x-session-id"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Shutdown(t.Context()) }()

	for i := 0; i < 3; i++ {
		resp := post(t, s, "/v1/messages", withMidSystem, map[string]string{"x-session-id": "ccx-abc"})
		_, _ = io.ReadAll(resp.Body)
		_ = resp.Body.Close()
	}
	mu.Lock()
	defer mu.Unlock()
	for i, v := range seen {
		if !strings.HasPrefix(v, "ccx-abc-") || v != seen[0] {
			t.Errorf("순차 요청 %d의 세션 id = %q, want %q 로 모두 동일", i, v, seen[0])
		}
	}
}

// 메인 대화와 서브에이전트는 시스템 프롬프트가 달라 서로 다른 세션 id를 받아야 한다.
// 그래야 한쪽이 다른 쪽의 committed 스트림을 덮어쓰지 않고, 동시에 떠도 겹치지 않는다.
func TestProxyScopesSessionPerConversation(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Header.Get("X-Session-Id"))
		mu.Unlock()
		_, _ = w.Write([]byte(`{}`))
	}))
	defer up.Close()

	s, err := Start(ServerOptions{UpstreamBaseURL: up.URL, SessionHeader: "x-session-id"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Shutdown(t.Context()) }()

	main := `{"system":[{"type":"text","text":"You are Claude Code."}],"messages":[{"role":"user","content":"작업"}]}`
	sub := `{"system":[{"type":"text","text":"You are a subagent."}],"messages":[{"role":"user","content":"조사"}]}`
	hdr := map[string]string{"x-session-id": "ccx-abc"}
	for _, b := range []string{main, sub, main} {
		resp := post(t, s, "/v1/messages", b, hdr)
		_, _ = io.ReadAll(resp.Body)
		_ = resp.Body.Close()
	}

	mu.Lock()
	defer mu.Unlock()
	if seen[0] == seen[1] {
		t.Errorf("메인과 서브에이전트가 같은 세션 id를 받았다: %q", seen[0])
	}
	if seen[0] != seen[2] {
		t.Errorf("같은 대화의 다음 턴이 다른 id를 받았다: %q vs %q", seen[0], seen[2])
	}
	for _, v := range seen {
		if !strings.HasPrefix(v, "ccx-abc-") {
			t.Errorf("런치 id 접두가 유지돼야 한다: %q", v)
		}
	}
}

// 업스트림이 그래도 409를 내면(다른 클라이언트가 같은 id를 쓰거나 앞선 요청이 비정상 종료돼
// 플래그가 남은 경우) 헤더를 빼고 한 번만 다시 시도한다.
func TestProxyRetriesOnceWithoutHeaderOn409(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sid := r.Header.Get("X-Session-Id")
		mu.Lock()
		seen = append(seen, sid)
		mu.Unlock()
		if sid != "" {
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":"session is already in flight"}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer up.Close()

	s, err := Start(ServerOptions{UpstreamBaseURL: up.URL, SessionHeader: "x-session-id"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Shutdown(t.Context()) }()

	resp := post(t, s, "/v1/messages", withMidSystem, map[string]string{"x-session-id": "ccx-abc"})
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200 (재시도가 성공해야 한다)", resp.StatusCode)
	}
	if !strings.Contains(string(body), `"ok":true`) {
		t.Errorf("body = %q", body)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 || !strings.HasPrefix(seen[0], "ccx-abc-") || seen[1] != "" {
		t.Errorf("요청 순서가 [id 포함, 미포함]이어야 하는데 %v", seen)
	}
}

// effort 매핑이 붙은 프록시. 기본 표(low=off, high=xhigh)를 쓴다.
func startProxyWithEffort(t *testing.T, upstream string) *Server {
	t.Helper()
	s, err := Start(ServerOptions{
		UpstreamBaseURL: upstream,
		UpstreamAuth:    "upstream-token",
		EffortMap:       tr.DefaultEffortMap(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Shutdown(t.Context()) })
	return s
}

// Claude Code는 세션 중 /effort로 값을 바꾸면 다음 요청부터 바뀐 값을 싣는다. 프록시는
// 요청마다 표를 다시 적용하므로 사고 깊이가 대화 도중에도 따라 움직인다.
func TestProxyMapsEffortPerRequest(t *testing.T) {
	var got capture
	up := newUpstream(t, &got, func(w http.ResponseWriter) { _, _ = w.Write([]byte(`{"ok":true}`)) })
	defer up.Close()
	s := startProxyWithEffort(t, up.URL)

	const tmpl = `{"model":"m","output_config":{"effort":"%s"},"thinking":{"type":"adaptive"},"messages":[]}`

	resp := post(t, s, "/v1/messages", strings.Replace(tmpl, "%s", "high", 1), nil)
	_ = resp.Body.Close()
	var high map[string]json.RawMessage
	if err := json.Unmarshal(got.body, &high); err != nil {
		t.Fatalf("본문 파싱 실패: %v", err)
	}
	if string(high["reasoning_effort"]) != `"medium"` {
		t.Errorf("high는 medium으로 실려야 하는데 %s", high["reasoning_effort"])
	}

	resp = post(t, s, "/v1/messages", strings.Replace(tmpl, "%s", "low", 1), nil)
	_ = resp.Body.Close()
	var low map[string]json.RawMessage
	if err := json.Unmarshal(got.body, &low); err != nil {
		t.Fatalf("본문 파싱 실패: %v", err)
	}
	if string(low["thinking"]) != `{"type":"disabled"}` {
		t.Errorf("low는 thinking을 꺼야 하는데 %s", low["thinking"])
	}
	if _, ok := low["reasoning_effort"]; ok {
		t.Errorf("thinking을 끈 요청에 reasoning_effort가 실렸다: %s", got.body)
	}
}

// 매핑을 끄면 본문은 바이트 그대로 나간다.
func TestProxyLeavesEffortAloneWhenDisabled(t *testing.T) {
	var got capture
	up := newUpstream(t, &got, func(w http.ResponseWriter) { _, _ = w.Write([]byte(`{}`)) })
	defer up.Close()
	s := startProxy(t, up.URL, false)

	const body = `{"model":"m","output_config":{"effort":"low"},"messages":[]}`
	resp := post(t, s, "/v1/messages", body, nil)
	defer resp.Body.Close()

	if string(got.body) != body {
		t.Errorf("매핑을 껐는데 본문이 바뀌었다:\n%s", got.body)
	}
}
