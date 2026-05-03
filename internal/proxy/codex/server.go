package codex

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

	auth "github.com/channel-spoonai/ccx/internal/auth/codex"
	tr "github.com/channel-spoonai/ccx/internal/translate/codex"
)

// ServerOptions는 프록시 서버 설정.
type ServerOptions struct {
	// Listener가 nil이면 127.0.0.1:0 (랜덤 포트)에 바인드한다.
	Listener net.Listener

	// SharedSecret이 비어있지 않으면 모든 요청에 Bearer 토큰 매칭을 강제.
	// Phase 5에서 launcher가 랜덤 시크릿을 만들어 ANTHROPIC_AUTH_TOKEN으로 주입한다.
	SharedSecret string

	// AuthManager가 없으면 디스크 기반 토큰을 사용하는 새 매니저를 만든다.
	AuthManager *auth.Manager

	// IdleTimeout이 0보다 크면 마지막 요청 후 이 시간 동안 추가 요청이 없으면 종료한다.
	// 데몬이 부모 프로세스 사망 감지를 놓쳤을 때의 안전장치.
	IdleTimeout time.Duration
}

// Server는 살아있는 프록시 인스턴스.
type Server struct {
	httpSrv      *http.Server
	listener     net.Listener
	mgr          *auth.Manager
	sharedSecret string
	lastActive   atomic.Int64 // unix nanos
	stop         chan struct{}
	stopOnce     sync.Once
	idleTimeout  time.Duration
}

// Start는 옵션대로 서버를 띄우고 즉시 반환한다. listener에서 OS가 할당한 포트를 .Port()로 조회.
func Start(opts ServerOptions) (*Server, error) {
	listener := opts.Listener
	if listener == nil {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return nil, fmt.Errorf("프록시 listen 실패: %w", err)
		}
		listener = l
	}
	mgr := opts.AuthManager
	if mgr == nil {
		mgr = auth.NewManager()
	}

	s := &Server{
		listener:     listener,
		mgr:          mgr,
		sharedSecret: opts.SharedSecret,
		stop:         make(chan struct{}),
		idleTimeout:  opts.IdleTimeout,
	}
	s.lastActive.Store(time.Now().UnixNano())

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/v1/messages", s.handleMessages)
	mux.HandleFunc("/v1/messages/count_tokens", s.handleCountTokens)

	s.httpSrv = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 30 * time.Second,
		// 응답이 SSE 스트림이라 길어질 수 있어 WriteTimeout은 두지 않는다.
	}

	go func() {
		_ = s.httpSrv.Serve(listener)
	}()
	if s.idleTimeout > 0 {
		go s.idleLoop()
	}
	return s, nil
}

// Port는 listener의 실제 포트.
func (s *Server) Port() int {
	if a, ok := s.listener.Addr().(*net.TCPAddr); ok {
		return a.Port
	}
	return 0
}

// Shutdown은 graceful 종료. 멱등 — 여러 번 호출해도 안전.
func (s *Server) Shutdown(ctx context.Context) error {
	s.stopOnce.Do(func() { close(s.stop) })
	return s.httpSrv.Shutdown(ctx)
}

// Done은 서버가 종료되면(외부 Shutdown 또는 idle exit) 닫히는 채널을 반환.
// 데몬이 자체 종료 신호 대기 시 이 채널을 listen한다.
func (s *Server) Done() <-chan struct{} { return s.stop }

func (s *Server) idleLoop() {
	// idleTimeout/2를 polling 간격으로 쓰되 30초 이상은 가지 않는다.
	// 짧은 timeout(테스트 등)에서도 즉각적이고, 일상 운영에서는 빈번하지 않다.
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
				_ = s.Shutdown(ctx) // stop 채널까지 닫아 RunDaemon을 깨움
				cancel()
				return
			}
		}
	}
}

// touch는 마지막 활성 시각을 갱신.
func (s *Server) touch() { s.lastActive.Store(time.Now().UnixNano()) }

// === 공통 ===

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.touch()
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}

// authorize는 SharedSecret이 설정된 경우 Bearer 토큰을 검증.
// constant-time 비교로 timing 공격 차단.
func (s *Server) authorize(r *http.Request) bool {
	if s.sharedSecret == "" {
		return true
	}
	got := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(got, prefix) {
		// claude code는 x-api-key를 보낼 수도 있음.
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

// === /v1/messages ===

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

	sessionID := r.Header.Get("x-claude-code-session-id")
	codexReq, err := tr.TranslateRequest(&req, tr.TranslateOptions{SessionID: sessionID})
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}

	upstream, err := Forward(r.Context(), s.mgr, codexReq, ForwardOptions{SessionID: sessionID})
	if err != nil {
		surfaceForwardError(w, err)
		return
	}
	defer upstream.Close()

	messageID := generateMessageID()
	streaming := req.Stream

	if streaming {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)
		// 스트림 변환 — 에러는 SSE 안의 error 이벤트로 surface된다.
		_ = tr.TranslateStream(upstream, w, tr.StreamOptions{
			MessageID: messageID,
			Model:     req.Model,
		}, nil)
		return
	}

	// 비스트리밍: 끝까지 읽어 합쳐서 단일 JSON으로 반환.
	resp, err := tr.AccumulateResponse(upstream, tr.AccumulateOptions{
		MessageID: messageID,
		Model:     req.Model,
	})
	if err != nil {
		var up *tr.UpstreamError
		if errors.As(err, &up) {
			status := http.StatusBadGateway
			errType := "api_error"
			if up.Kind == tr.ErrorRateLimit {
				status = http.StatusTooManyRequests
				errType = "rate_limit_error"
			}
			writeJSONError(w, status, errType, up.Message)
			return
		}
		writeJSONError(w, http.StatusBadGateway, "api_error", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// surfaceForwardError는 Forward()의 에러를 적절한 HTTP 응답으로 변환.
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
			writeJSONError(w, http.StatusUnauthorized, "authentication_error", "Codex 인증 실패 — `ccx codex login` 다시 실행 필요")
			return
		case 403:
			writeJSONError(w, http.StatusForbidden, "permission_error", "Codex 접근 거부됨: "+fe.Detail)
			return
		}
		writeJSONError(w, http.StatusBadGateway, "api_error", fmt.Sprintf("upstream %d: %s", fe.Status, fe.Detail))
		return
	}
	if errors.Is(err, auth.ErrNotAuthenticated) {
		writeJSONError(w, http.StatusUnauthorized, "authentication_error", err.Error())
		return
	}
	writeJSONError(w, http.StatusBadGateway, "api_error", err.Error())
}

// stripContextSuffix는 "gpt-5.4[1m]" → "gpt-5.4" 처럼 Claude Code의 컨텍스트 윈도우 힌트
// 접미사를 제거. raine과 동일한 방어 코드.
func stripContextSuffix(model string) string {
	if i := strings.LastIndex(model, "["); i > 0 && strings.HasSuffix(model, "]") {
		// e.g. [1m] / [200k] 형태만 잘라낸다.
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

// === /v1/messages/count_tokens ===
//
// Claude Code가 컨텍스트 추정에 사용하는 엔드포인트. 정확한 OpenAI 토크나이저 없이
// 휴리스틱(문자 수 / 4)으로 근사한다 — Claude Code는 이 값을 컨텍스트 windowsize 추정에만
// 사용하므로 ±20% 오차여도 사용에 지장 없다. 향후 정확한 카운트가 필요하면 별도 패키지로 교체.

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

// estimateTokens는 요청 페이로드의 직렬화된 길이를 기반으로 토큰 수를 추정한다.
// 정확한 토크나이저 대신 chars/4 휴리스틱 — OpenAI 영어 평균과 비슷한 비율이지만
// 한국어/CJK는 토큰 수가 더 많아 실제보다 낮게 잡힐 수 있다.
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
