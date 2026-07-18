// Package codex는 Anthropic /v1/messages 요청을 받아 OpenAI Codex Responses API로
// 변환·forwarding하는 로컬 프록시 서버를 제공한다.
package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	auth "github.com/channel-spoonai/ccx/internal/auth/codex"
	tr "github.com/channel-spoonai/ccx/internal/translate/codex"
)

// debugEnabled는 CCX_CODEX_DEBUG 환경변수가 비어있지 않으면 verbose 로그를 켠다.
// 데몬은 자식 프로세스라 stderr가 부모 ccx의 stderr에 합류한다 — 사용자가 그대로 본다.
func debugEnabled() bool { return os.Getenv("CCX_CODEX_DEBUG") != "" }

// upstreamClient는 Codex 백엔드로 요청을 보낼 때 사용하는 HTTP 클라이언트.
// idleTimeout/streaming 처리를 위해 별도 인스턴스를 둔다.
var upstreamClient = &http.Client{
	// 스트리밍 응답이 길게 이어질 수 있으므로 전체 timeout은 두지 않고,
	// 대신 호출자가 ctx로 제어하도록 한다.
}

// upstreamVersion은 Codex가 식별하는 우리 버전 (User-Agent에 들어감).
// 너무 알려지지 않은 토큰을 쓰면 차단당할 수 있어 raine과 동일한 형식으로 둔다.
var upstreamVersion = "claude-code-proxy/dev"

// SetUpstreamVersion은 빌드 시점에 결정되는 버전 문자열을 외부에서 주입할 때 사용.
// (cmd/ccx/main.go의 ldflags에서 set되는 main.version을 참고)
func SetUpstreamVersion(v string) {
	if v != "" {
		upstreamVersion = v
	}
}

// ForwardError는 Codex 백엔드의 비정상 응답(2xx 아님)을 감싼다.
type ForwardError struct {
	Status     int
	Detail     string
	RetryAfter string
}

func (e *ForwardError) Error() string {
	return fmt.Sprintf("upstream %d: %s", e.Status, e.Detail)
}

// ForwardOptions는 단일 요청의 컨텍스트.
type ForwardOptions struct {
	SessionID string
}

// UpstreamConfig는 forwarding 대상 설정. zero value는 기존 ChatGPT OAuth 모드.
type UpstreamConfig struct {
	// Endpoint가 비어있으면 auth.CodexAPIEndpoint (ChatGPT 백엔드).
	// API 키 모드에서는 보통 https://api.openai.com/v1/responses.
	Endpoint string

	// APIKey가 비어있지 않으면 OpenAI API 키 모드 — OAuth 토큰 매니저와
	// ChatGPT 전용 헤더(originator, ChatGPT-Account-Id, session 트로이카) 대신
	// Authorization: Bearer <key> 만 보낸다.
	APIKey string
}

func (u UpstreamConfig) endpoint() string {
	if u.Endpoint != "" {
		return u.Endpoint
	}
	return auth.CodexAPIEndpoint
}

// APIKeyMode는 정적 API 키 인증을 쓰는지 여부.
func (u UpstreamConfig) APIKeyMode() bool { return u.APIKey != "" }

// Forward는 변환된 ResponsesRequest 를 upstream으로 POST하고 응답 body 스트림을 돌려준다.
//
// OAuth 모드에서 401 응답은 토큰 만료를 의미하므로 한 번에 한해 강제 refresh 후 재시도한다.
// API 키 모드의 401은 재시도 없이 그대로 전달 (키가 잘못된 것 — refresh 개념이 없다).
// 429/403 등 다른 에러는 ForwardError로 감싸 호출자에게 전달.
func Forward(
	ctx context.Context,
	mgr *auth.Manager,
	upstream UpstreamConfig,
	body *tr.ResponsesRequest,
	opts ForwardOptions,
) (io.ReadCloser, error) {
	var resp *http.Response
	if upstream.APIKeyMode() {
		var err error
		resp, err = doForward(ctx, upstream, nil, body, opts)
		if err != nil {
			return nil, err
		}
	} else {
		auth1, err := mgr.Snapshot(ctx)
		if err != nil {
			return nil, err
		}
		resp, err = doForward(ctx, upstream, auth1, body, opts)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode == 401 {
			// 만료 직전에 캐시가 fresh로 판정될 수 있어 한 번에 한해 강제 refresh.
			_ = resp.Body.Close()
			auth2, refErr := mgr.ForceRefresh(ctx)
			if refErr != nil {
				return nil, fmt.Errorf("token refresh after 401 failed: %w", refErr)
			}
			resp, err = doForward(ctx, upstream, auth2, body, opts)
			if err != nil {
				return nil, err
			}
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		retryAfter := resp.Header.Get("Retry-After")
		_ = resp.Body.Close()
		if debugEnabled() {
			fmt.Fprintf(os.Stderr, "[ccx codex-proxy] upstream %d: %s\n", resp.StatusCode, string(detail))
		}
		return nil, &ForwardError{
			Status:     resp.StatusCode,
			Detail:     string(detail),
			RetryAfter: retryAfter,
		}
	}
	return resp.Body, nil
}

func doForward(
	ctx context.Context,
	upstream UpstreamConfig,
	oauth *auth.StoredAuth,
	body *tr.ResponsesRequest,
	opts ForwardOptions,
) (*http.Response, error) {
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", upstream.endpoint(), bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("User-Agent", upstreamVersion)
	if upstream.APIKeyMode() {
		req.Header.Set("Authorization", "Bearer "+upstream.APIKey)
	} else {
		req.Header.Set("Authorization", "Bearer "+oauth.AccessToken)
		req.Header.Set("originator", auth.Originator)
		req.Header.Set("openai-beta", "responses=experimental")
		if oauth.AccountID != "" {
			req.Header.Set("ChatGPT-Account-Id", oauth.AccountID)
		}
		if opts.SessionID != "" {
			// 세션 일관성을 위해 raine과 동일한 헤더 트로이카를 보낸다.
			// ChatGPT 백엔드 전용 — 공식 API에는 보내지 않는다.
			req.Header.Set("session_id", opts.SessionID)
			req.Header.Set("x-client-request-id", opts.SessionID)
			req.Header.Set("x-codex-window-id", opts.SessionID+":0")
		}
	}

	if debugEnabled() {
		effort := "(none)"
		if body.Reasoning != nil && body.Reasoning.Effort != "" {
			effort = body.Reasoning.Effort
		}
		fmt.Fprintf(os.Stderr, "[ccx codex-proxy] POST %s model=%s effort=%s input_items=%d tools=%d session=%q\n",
			upstream.endpoint(), body.Model, effort, len(body.Input), len(body.Tools), opts.SessionID)
	}

	resp, err := upstreamClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("upstream request failed: %w", err)
	}
	if debugEnabled() {
		fmt.Fprintf(os.Stderr, "[ccx codex-proxy] upstream status=%d\n", resp.StatusCode)
	}
	return resp, nil
}

