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
}

type Server struct {
	httpSrv         *http.Server
	listener        net.Listener
	sharedSecret    string
	upstreamBaseURL string
	upstreamAuth    string
	upstreamAPIKey  string
	normalizeSystem bool
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

	upstream, err := s.forward(r, body)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, "api_error", "upstream request failed: "+err.Error())
		return
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

// skipResponseHeader는 net/http가 스스로 관리하는 hop-by-hop 헤더를 거른다.
func skipResponseHeader(name string) bool {
	switch strings.ToLower(name) {
	case "content-length", "transfer-encoding", "connection", "keep-alive":
		return true
	}
	return false
}
