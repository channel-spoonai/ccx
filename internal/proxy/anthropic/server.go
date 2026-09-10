// Package anthropic은 Anthropic /v1/messages 를 업스트림에 그대로 중계하는 로컬 프록시다.
// openaichat/codex 프록시와 달리 페이로드를 번역하지 않는다 — 프리픽스 캐시를 깨는
// 형태만 정규화하고 나머지는 바이트 그대로 흘려보낸다.
package anthropic

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tr "github.com/channel-spoonai/ccx/internal/translate/anthropic"
)

type ServerOptions struct {
	Listener        net.Listener
	SharedSecret    string
	UpstreamBaseURL string
	UpstreamAuth    string
	UpstreamAPIKey  string
	IdleTimeout     time.Duration

	// NormalizeSystem이 true면 messages 안의 role:"system"을 role:"user"로 바꾼다.
	NormalizeSystem bool

	// SessionHeader는 ccx가 찍는 세션 어피니티 헤더 이름 (비어 있으면 동시성 조정 안 함).
	SessionHeader string
}

type Server struct {
	httpSrv         *http.Server
	listener        net.Listener
	sharedSecret    string
	upstreamBaseURL string
	upstreamAuth    string
	upstreamAPIKey  string
	normalizeSystem bool
	sessionHeader   string
	inFlightMu      sync.Mutex
	inFlight        map[string]bool
	lastActive      atomic.Int64
	stop            chan struct{}
	stopOnce        sync.Once
	idleTimeout     time.Duration
}

func Start(opts ServerOptions) (*Server, error) {
	listener := opts.Listener
	if listener == nil {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return nil, fmt.Errorf("proxy listen failed: %w", err)
		}
		listener = l
	}
	s := &Server{
		listener:        listener,
		sharedSecret:    opts.SharedSecret,
		upstreamBaseURL: strings.TrimSuffix(opts.UpstreamBaseURL, "/"),
		upstreamAuth:    opts.UpstreamAuth,
		upstreamAPIKey:  opts.UpstreamAPIKey,
		normalizeSystem: opts.NormalizeSystem,
		sessionHeader:   strings.TrimSpace(opts.SessionHeader),
		inFlight:        map[string]bool{},
		stop:            make(chan struct{}),
		idleTimeout:     opts.IdleTimeout,
	}
	s.lastActive.Store(time.Now().UnixNano())

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/", s.handleProxy)

	s.httpSrv = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 30 * time.Second,
	}

	go func() { _ = s.httpSrv.Serve(listener) }()
	if s.idleTimeout > 0 {
		go s.idleLoop()
	}
	return s, nil
}

func (s *Server) Port() int {
	if a, ok := s.listener.Addr().(*net.TCPAddr); ok {
		return a.Port
	}
	return 0
}

func (s *Server) Shutdown(ctx context.Context) error {
	s.stopOnce.Do(func() { close(s.stop) })
	return s.httpSrv.Shutdown(ctx)
}

func (s *Server) Done() <-chan struct{} { return s.stop }

func (s *Server) touch() { s.lastActive.Store(time.Now().UnixNano()) }

func (s *Server) idleLoop() {
	interval := s.idleTimeout / 2
	if interval > 30*time.Second {
		interval = 30 * time.Second
	}
	if interval < 50*time.Millisecond {
		interval = 50 * time.Millisecond
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-s.stop:
			return
		case now := <-t.C:
			last := time.Unix(0, s.lastActive.Load())
			if now.Sub(last) > s.idleTimeout {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				_ = s.Shutdown(ctx)
				cancel()
				return
			}
		}
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.touch()
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}

func (s *Server) authorize(r *http.Request) bool {
	if s.sharedSecret == "" {
		return true
	}
	got := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(got, prefix) {
		got = r.Header.Get("x-api-key")
		return subtle.ConstantTimeCompare([]byte(got), []byte(s.sharedSecret)) == 1
	}
	tok := strings.TrimPrefix(got, prefix)
	return subtle.ConstantTimeCompare([]byte(tok), []byte(s.sharedSecret)) == 1
}

func writeJSONError(w http.ResponseWriter, status int, errType, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	body, _ := json.Marshal(map[string]any{
		"type":  "error",
		"error": map[string]string{"type": errType, "message": msg},
	})
	_, _ = w.Write(body)
}

// handleProxy는 모든 경로를 업스트림으로 중계한다. 요청 본문은 POST일 때만 읽고,
// 정규화 대상이면 messages를 손본 뒤 보낸다.
func (s *Server) handleProxy(w http.ResponseWriter, r *http.Request) {
	s.touch()
	defer s.touch()

	if !s.authorize(r) {
		writeJSONError(w, http.StatusUnauthorized, "authentication_error", "invalid bearer token")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_request_error", "failed to read request body: "+err.Error())
		return
	}
	if s.normalizeSystem && len(body) > 0 && strings.HasPrefix(r.URL.Path, "/v1/messages") {
		// 정규화 실패는 치명적이지 않다 — 원본을 그대로 보내면 캐시만 손해고 동작은 한다.
		if patched, n, nerr := tr.NormalizeSystemMessages(body); nerr == nil && n > 0 {
			body = patched
		}
	}

	// 세션 id를 대화 단위로 좁힌다. 런치 단위 id 하나를 메인 대화와 서브에이전트가 공유하면
	// 먼저 도착한 쪽이 그 세션을 차지하고 자기 히스토리를 committed 스트림에 커밋해, 다른 쪽의
	// 프리픽스가 어긋난다(실측: 서브에이전트가 메인 id를 차지한 턴 재사용률 38.2%).
	s.scopeSessionToConversation(r, body)

	// 세션 어피니티 헤더는 "이 세션의 요청은 직렬화된다"는 약속으로 읽힌다. Claude Code는
	// 서브에이전트 등으로 동시 요청을 내므로, 이미 생성 중인 세션에 또 찍어 보내면 업스트림이
	// 409(already in flight)로 거절한다. 겹치는 요청에서는 헤더를 빼서 업스트림이 쓰던 대로
	// 프롬프트 프리픽스 추론에 맡긴다 — 그쪽은 동시 요청을 별도 세션으로 갈라 처리한다.
	// 메인 대화는 계속 같은 id를 유지하므로 캐시 재사용은 그대로다.
	stamped := s.claimSession(r)
	if stamped != "" {
		defer s.releaseSession(stamped)
	}

	upstream, err := s.forward(r, body)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, "api_error", "upstream request failed: "+err.Error())
		return
	}
	// 선점 판정과 업스트림의 실제 상태가 어긋날 수 있다(다른 클라이언트가 같은 id를 쓰거나,
	// 앞선 요청이 비정상 종료돼 플래그가 남은 경우). 헤더를 빼고 한 번만 다시 시도한다.
	if upstream.StatusCode == http.StatusConflict && stamped != "" {
		_ = upstream.Body.Close()
		r.Header.Del(s.sessionHeader)
		if retry, rerr := s.forward(r, body); rerr == nil {
			upstream = retry
		} else {
			writeJSONError(w, http.StatusBadGateway, "api_error", "upstream retry failed: "+rerr.Error())
			return
		}
	}
	defer upstream.Body.Close()

	for k, vs := range upstream.Header {
		if skipResponseHeader(k) {
			continue
		}
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(upstream.StatusCode)

	// SSE가 버퍼링되면 Claude Code의 스트리밍 UI가 멈춘 것처럼 보인다 — 청크마다 flush.
	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 32*1024)
	for {
		n, rerr := upstream.Body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if rerr != nil {
			return
		}
	}
}

// scopeSessionToConversation은 ccx가 찍은 런치 id에 대화 해시를 붙여 대화별 id로 만든다.
// 대화를 식별할 수 없으면 런치 id를 그대로 둔다 — 좁히지 못할 뿐 동작은 한다.
func (s *Server) scopeSessionToConversation(r *http.Request, body []byte) {
	if s.sessionHeader == "" || len(body) == 0 {
		return
	}
	base := strings.TrimSpace(r.Header.Get(s.sessionHeader))
	if base == "" {
		return
	}
	if key := tr.ConversationKey(body); key != "" {
		r.Header.Set(s.sessionHeader, base+"-"+key)
	}
}

// claimSession은 이 요청이 세션 슬롯을 선점했으면 그 값을, 못 했으면 ""를 돌려준다.
// 선점 실패 시 요청에서 헤더를 지워 업스트림이 암묵 세션으로 처리하게 한다.
// 생성이 없는 count_tokens는 슬롯을 잡지 않는다.
func (s *Server) claimSession(r *http.Request) string {
	if s.sessionHeader == "" || strings.HasSuffix(r.URL.Path, "count_tokens") {
		return ""
	}
	v := strings.TrimSpace(r.Header.Get(s.sessionHeader))
	if v == "" {
		return ""
	}
	s.inFlightMu.Lock()
	defer s.inFlightMu.Unlock()
	if s.inFlight[v] {
		r.Header.Del(s.sessionHeader)
		return ""
	}
	s.inFlight[v] = true
	return v
}

func (s *Server) releaseSession(v string) {
	s.inFlightMu.Lock()
	delete(s.inFlight, v)
	s.inFlightMu.Unlock()
}

// skipResponseHeader는 net/http가 스스로 관리하는 hop-by-hop 헤더를 거른다.
func skipResponseHeader(name string) bool {
	switch strings.ToLower(name) {
	case "content-length", "transfer-encoding", "connection", "keep-alive":
		return true
	}
	return false
}
