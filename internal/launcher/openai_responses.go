package launcher

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/channel-spoonai/ccx/internal/config"
	proxy "github.com/channel-spoonai/ccx/internal/proxy/codex"
)

// AuthOpenAIResponses는 Profile.Auth 필드 값. OpenAI API 키로 공식 Responses API를
// 직접 호출한다 — codex 변환 프록시(Anthropic Messages ↔ OpenAI Responses)를 재사용하되
// ChatGPT OAuth 대신 profile.apiKey의 정적 키를 쓴다.
//
// ChatGPT 구독 백엔드는 컨텍스트를 272K로 캡하지만 API 티어는 모델 전체 컨텍스트
// (GPT-5.6 기준 1M+)를 제공한다. 단, 272K 초과 입력은 long-context 요율로 과금된다.
const AuthOpenAIResponses = "openai-responses"

// 기본 업스트림. profile.baseUrl로 호환 게이트웨이 오버라이드 가능.
const defaultOpenAIResponsesEndpoint = "https://api.openai.com/v1/responses"

// openAIResponsesEndpoint는 프로파일에서 실제 업스트림 URL을 계산한다.
// 규칙: baseUrl 미지정 → 기본 엔드포인트 / "/responses"로 끝나면 그대로 /
// "/v1"로 끝나면 "/responses"만 추가 / 그 외 "/v1/responses" 추가.
//
// baseUrl이 지정됐는데 env: 참조가 미해석이면 에러 — 조용히 api.openai.com으로
// 폴백하면 게이트웨이 전용 키가 제3자 엔드포인트로 전송될 수 있다.
func openAIResponsesEndpoint(p *config.Profile) (string, error) {
	if p.BaseURL == "" {
		return defaultOpenAIResponsesEndpoint, nil
	}
	base := ResolveSecret(p.BaseURL)
	if base == "" {
		return "", fmt.Errorf("profile.baseUrl %q resolved to empty — set the referenced environment variable", p.BaseURL)
	}
	endpoint := strings.TrimRight(base, "/")
	switch {
	case strings.HasSuffix(endpoint, "/responses"):
		// full URL 그대로 사용
	case strings.HasSuffix(endpoint, "/v1"):
		endpoint += "/responses"
	default:
		endpoint += "/v1/responses"
	}
	return endpoint, nil
}

// prepareOpenAIResponses는 openai-responses 프로파일을 위해 백그라운드 프록시를 띄우고
// BaseURL/AuthToken을 자동 주입한 profile copy를 돌려준다.
func prepareOpenAIResponses(p *config.Profile) (*config.Profile, error) {
	key := ResolveSecret(p.APIKey)
	if key == "" {
		key = ResolveSecret(p.AuthToken)
	}
	if key == "" {
		return nil, errors.New(`OpenAI API key required — set profile.apiKey (literal key or "env:OPENAI_API_KEY")`)
	}
	endpoint, err := openAIResponsesEndpoint(p)
	if err != nil {
		return nil, err
	}

	sd, err := proxy.SpawnDaemon(proxy.SpawnInput{
		Upstream: proxy.UpstreamConfig{
			Endpoint: endpoint,
			APIKey:   key,
		},
	}, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to spawn OpenAI responses proxy: %w", err)
	}

	p2 := *p
	p2.BaseURL = sd.Address()
	p2.AuthToken = sd.SharedSecret
	p2.APIKey = ""
	return &p2, nil
}

func printOpenAIResponsesBanner(addr, endpoint string) {
	fmt.Printf("\x1B[36m[ccx]\x1B[0m OpenAI responses proxy: %s → %s\n", addr, endpoint)
}
