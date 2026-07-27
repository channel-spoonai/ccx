// Package openaichat은 Anthropic /v1/messages 요청을 받아 OpenAI Chat Completions API로
// 변환·forwarding하는 로컬 프록시 서버를 제공한다.
//
// codex 패키지(OpenAI Responses + ChatGPT OAuth)와 달리:
//   - 인증은 정적 (Bearer {profile.authToken} 또는 빈 헤더)
//   - upstream URL은 사용자가 profile.baseUrl로 직접 지정
//   - 변환 페이로드는 OpenAI Chat Completions 공식 스펙
//
// 이 두 가지 차이만으로 lightning-mlx / vLLM / LocalAI / LM Studio (chat) 등 OpenAI 호환 서버를
// Claude Code에 라우팅할 수 있다.
package openaichat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"

	tr "github.com/channel-spoonai/ccx/internal/translate/openaichat"
)

func debugEnabled() bool { return os.Getenv("CCX_OPENAICHAT_DEBUG") != "" }

// upstreamClient는 OpenAI 호환 백엔드로 요청을 보낼 때 사용하는 HTTP 클라이언트.
// 스트리밍 응답이 길게 이어지므로 전체 timeout은 두지 않고 ctx로 제어.
var upstreamClient = &http.Client{}

// ForwardError는 upstream의 비정상 응답을 감싼다.
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
	UpstreamBaseURL string // 예: http://127.0.0.1:8010
	UpstreamAuth    string // Authorization: Bearer 값. 비어있으면 헤더 자체 생략.
	UpstreamAPIKey  string // x-api-key 또는 OpenAI 호환의 Bearer. authToken과 상호 배타.
}

// enableThinkingUnsupported는 upstream이 enable_thinking을 400으로 거부한 모델 집합.
// NVIDIA NIM은 같은 엔드포인트라도 모델마다 서빙 백엔드가 달라 수용 여부가 갈린다
// (gpt-oss는 통과, nemotron-3/deepseek-v4/minimax-m3는 "Validation: Unsupported
// parameter(s)"로 400). 프로파일 단위 설정으로는 opus만 거부당하는 조합을 다룰 수
// 없어 모델 단위로 기억한다. 데몬 프로세스 생명주기 동안만 유지.
var enableThinkingUnsupported sync.Map // model(string) → struct{}

// Forward는 변환된 ChatRequest를 upstream으로 POST하고 응답 body 스트림을 돌려준다.
//
// 401은 재시도하지 않는다 — OAuth가 아니라 정적 토큰이므로 한 번 401이면 사용자가
// profile을 고쳐야 한다. 반면 enable_thinking(비표준 확장) 때문에 400이 나면 그
// 필드만 빼고 한 번 재시도한다 — 엄격한 업스트림에서도 요청이 통과해야 하고,
// 이 플래그가 필요한 lightning-mlx 쪽 동작은 그대로 두어야 하기 때문.
func Forward(ctx context.Context, body *tr.ChatRequest, opts ForwardOptions) (io.ReadCloser, error) {
	if body.EnableThinking != nil {
		if _, bad := enableThinkingUnsupported.Load(body.Model); bad {
			body = withoutEnableThinking(body)
		}
	}

	rc, err := forwardOnce(ctx, body, opts)
	if err == nil || body.EnableThinking == nil || !rejectsEnableThinking(err) {
		return rc, err
	}
	enableThinkingUnsupported.Store(body.Model, struct{}{})
	if debugEnabled() {
		fmt.Fprintf(os.Stderr, "[ccx openaichat-proxy] %s rejected enable_thinking — retrying without it\n", body.Model)
	}
	return forwardOnce(ctx, withoutEnableThinking(body), opts)
}

// withoutEnableThinking은 얕은 복사본에서 플래그만 지운다. 슬라이스 필드는
// 공유하지만 forwardOnce가 요청 본문을 수정하지 않으므로 안전하다.
func withoutEnableThinking(body *tr.ChatRequest) *tr.ChatRequest {
	clone := *body
	clone.EnableThinking = nil
	return &clone
}

// rejectsEnableThinking은 400 + 본문에 필드명이 언급된 경우만 참. 다른 400
// (잘못된 모델 ID, 컨텍스트 초과 등)을 무의미하게 재시도하지 않기 위함.
func rejectsEnableThinking(err error) bool {
	var fe *ForwardError
	if !errors.As(err, &fe) || fe.Status != http.StatusBadRequest {
		return false
	}
	return strings.Contains(fe.Detail, "enable_thinking")
}

func forwardOnce(ctx context.Context, body *tr.ChatRequest, opts ForwardOptions) (io.ReadCloser, error) {
	endpoint, err := chatCompletionsURL(opts.UpstreamBaseURL)
	if err != nil {
		return nil, err
	}

	buf, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if body.Stream {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}
	if opts.UpstreamAuth != "" {
		req.Header.Set("Authorization", "Bearer "+opts.UpstreamAuth)
	}
	if opts.UpstreamAPIKey != "" {
		req.Header.Set("x-api-key", opts.UpstreamAPIKey)
		// OpenAI 호환 일부 게이트웨이는 Bearer만 받으므로 둘 다 채워주는 편이 안전.
		if opts.UpstreamAuth == "" {
			req.Header.Set("Authorization", "Bearer "+opts.UpstreamAPIKey)
		}
	}

	if debugEnabled() {
		fmt.Fprintf(os.Stderr, "[ccx openaichat-proxy] POST %s model=%s msgs=%d tools=%d stream=%t\n",
			endpoint, body.Model, len(body.Messages), len(body.Tools), body.Stream)
	}

	resp, err := upstreamClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("upstream request failed: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		retryAfter := resp.Header.Get("Retry-After")
		_ = resp.Body.Close()
		if debugEnabled() {
			fmt.Fprintf(os.Stderr, "[ccx openaichat-proxy] upstream %d: %s\n", resp.StatusCode, string(detail))
		}
		return nil, &ForwardError{
			Status:     resp.StatusCode,
			Detail:     string(detail),
			RetryAfter: retryAfter,
		}
	}
	return resp.Body, nil
}

// chatCompletionsURL은 baseUrl에 /v1/chat/completions 를 append한다.
//
// 처리 규칙:
//   - baseUrl이 이미 /v1/chat/completions로 끝나면 그대로 사용
//   - baseUrl이 /v1로 끝나면 /chat/completions만 추가
//   - 그 외에는 /v1/chat/completions 추가
//   - 끝의 슬래시는 정리
//
// 이 정책은 LM Studio 프로파일의 baseUrl="http://localhost:1234"가 자연스럽게
// http://localhost:1234/v1/chat/completions 로 resolve되도록 한다.
func chatCompletionsURL(baseURL string) (string, error) {
	if baseURL == "" {
		return "", fmt.Errorf("upstream baseUrl is empty")
	}
	u := strings.TrimRight(baseURL, "/")
	switch {
	case strings.HasSuffix(u, "/v1/chat/completions"):
		return u, nil
	case strings.HasSuffix(u, "/v1"):
		return u + "/chat/completions", nil
	default:
		return u + "/v1/chat/completions", nil
	}
}
