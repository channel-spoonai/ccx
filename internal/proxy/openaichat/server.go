package openaichat

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tr "github.com/channel-spoonai/ccx/internal/translate/openaichat"
)

// ServerOptions는 프록시 서버 설정.
type ServerOptions struct {
	Listener        net.Listener
	SharedSecret    string
	UpstreamBaseURL string
	UpstreamAuth    string
	UpstreamAPIKey  string
	IdleTimeout     time.Duration

	// EnableThinking은 upstream에 보낼 enable_thinking 값.
	// nil이면 필드 자체를 보내지 않음. lightning-mlx 등 reasoning 모델 대응.
	EnableThinking *bool
}

type Server struct {
	httpSrv         *http.Server
	listener        net.Listener
	sharedSecret    string
	upstreamBaseURL string
	upstreamAuth    string
	upstreamAPIKey  string
	enableThinking  *bool
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
		upstreamBaseURL: opts.UpstreamBaseURL,
		upstreamAuth:    opts.UpstreamAuth,
		upstreamAPIKey:  opts.UpstreamAPIKey,
		enableThinking:  opts.EnableThinking,
		stop:            make(chan struct{}),
		idleTimeout:     opts.IdleTimeout,
	}
	s.lastActive.Store(time.Now().UnixNano())

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/v1/messages", s.handleMessages)
	mux.HandleFunc("/v1/messages/count_tokens", s.handleCountTokens)

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

func (s *Server) touch() { s.lastActive.Store(time.Now().UnixNano()) }

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

func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	s.touch()
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST only")
		return
	}
	if !s.authorize(r) {
		writeJSONError(w, http.StatusUnauthorized, "authentication_error", "invalid bearer token")
		return
	}
	var req tr.AnthropicRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_request_error", "invalid JSON: "+err.Error())
		return
	}
	if req.Model == "" {
		writeJSONError(w, http.StatusBadRequest, "invalid_request_error", "missing model")
		return
	}
	req.Model = stripContextSuffix(req.Model)

	chatReq, err := tr.TranslateRequest(&req, tr.TranslateOptions{
		IncludeUsage:   req.Stream,
		EnableThinking: s.enableThinking,
	})
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}

	upstream, err := Forward(r.Context(), chatReq, ForwardOptions{
		UpstreamBaseURL: s.upstreamBaseURL,
		UpstreamAuth:    s.upstreamAuth,
		UpstreamAPIKey:  s.upstreamAPIKey,
	})
	if err != nil {
		surfaceForwardError(w, err)
		return
	}
	defer upstream.Close()

	messageID := generateMessageID()
	if req.Stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)
		var flusher interface{ Flush() }
		if f, ok := w.(http.Flusher); ok {
			flusher = f
		}
		_ = tr.TranslateStream(upstream, w, tr.StreamOptions{
			MessageID: messageID,
			Model:     req.Model,
		}, flusher)
		return
	}

	resp, err := tr.AccumulateResponse(upstream, tr.AccumulateOptions{
		MessageID: messageID,
		Model:     req.Model,
	})
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, "api_error", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func surfaceForwardError(w http.ResponseWriter, err error) {
	var fe *ForwardError
	if errors.As(err, &fe) {
		switch fe.Status {
		case 429:
			if fe.RetryAfter != "" {
				w.Header().Set("Retry-After", fe.RetryAfter)
			}
			writeJSONError(w, http.StatusTooManyRequests, "rate_limit_error", "upstream rate limited: "+fe.Detail)
			return
		case 401:
			writeJSONError(w, http.StatusUnauthorized, "authentication_error", "upstream rejected credentials: "+fe.Detail)
			return
		case 403:
			writeJSONError(w, http.StatusForbidden, "permission_error", "upstream access denied: "+fe.Detail)
			return
		}
		writeJSONError(w, http.StatusBadGateway, "api_error", fmt.Sprintf("upstream %d: %s", fe.Status, fe.Detail))
		return
	}
	writeJSONError(w, http.StatusBadGateway, "api_error", err.Error())
}

// stripContextSuffix는 "model-name[1m]" → "model-name" 처럼 Claude Code의 컨텍스트 윈도우 힌트
// 접미사를 제거. codex 패키지와 동일한 방어 코드.
func stripContextSuffix(model string) string {
	if i := strings.LastIndex(model, "["); i > 0 && strings.HasSuffix(model, "]") {
		suffix := strings.ToLower(model[i:])
		if strings.HasSuffix(suffix, "m]") || strings.HasSuffix(suffix, "k]") {
			return model[:i]
		}
	}
	return model
}

func generateMessageID() string {
	var buf [12]byte
	_, _ = rand.Read(buf[:])
	return "msg_" + base64.RawURLEncoding.EncodeToString(buf[:])
}

func (s *Server) handleCountTokens(w http.ResponseWriter, r *http.Request) {
	s.touch()
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST only")
		return
	}
	if !s.authorize(r) {
		writeJSONError(w, http.StatusUnauthorized, "authentication_error", "invalid bearer token")
		return
	}
	var req tr.AnthropicRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_request_error", "invalid JSON")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]int{
		"input_tokens": estimateTokens(&req),
	})
}

func estimateTokens(req *tr.AnthropicRequest) int {
	buf, err := json.Marshal(req)
	if err != nil {
		return 0
	}
	n := len(buf) / 4
	if n < 1 {
		n = 1
	}
	return n
}
