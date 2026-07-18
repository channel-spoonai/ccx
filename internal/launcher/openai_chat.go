package launcher

import (
	"errors"
	"fmt"
	"time"

	"github.com/channel-spoonai/ccx/internal/config"
	proxy "github.com/channel-spoonai/ccx/internal/proxy/openaichat"
)

// AuthOpenAIChat은 Profile.Auth 필드 값. ccx가 로컬 OpenAI Chat Completions 변환 프록시를
// 띄워 Anthropic /v1/messages 요청을 OpenAI 표준 페이로드로 변환한다.
//
// profile에는 baseUrl(upstream)과 authToken/apiKey(있다면)을 그대로 두며,
// 변환 프록시가 부모로부터 받아 forwarding한다. Claude Code 측은 변환된 로컬 프록시 주소를
// ANTHROPIC_BASE_URL로 받는다.
const AuthOpenAIChat = "openai-chat"

// prepareOpenAIChat은 openai-chat 프로파일을 위해 백그라운드 프록시를 띄우고
// BaseURL/AuthToken을 자동 주입한 profile copy를 돌려준다.
//
// enable_thinking 디폴트는 false. Claude Code는 OpenAI reasoning_content를 UI에 표시할 수단이
// 없고, 일부 reasoning 모델(lightning-mlx Qwen3 등)은 streaming 시 token budget을 reasoning에
// 모두 소진해 실제 content가 거의 안 나오는 사양 차이가 있다. 사용자가 reasoning을 명시적으로
// 켜려면 profile.env에 `CCX_OPENAICHAT_ENABLE_THINKING=true` 를 추가하면 된다.
func prepareOpenAIChat(p *config.Profile) (*config.Profile, error) {
	upstream := ResolveSecret(p.BaseURL)
	if upstream == "" {
		return nil, errors.New("profile.baseUrl is empty — set it to the upstream OpenAI-compatible server (e.g. http://127.0.0.1:8010)")
	}
	upstreamAuth := ResolveSecret(p.AuthToken)
	upstreamAPIKey := ResolveSecret(p.APIKey)

	// 디폴트 false. profile.env에서 동일 키를 override할 수 있다.
	enableThinking := false
	if v, ok := p.Env[proxy.CCXEnableThinkingEnv]; ok {
		rv := ResolveSecret(v)
		if rv == "true" || rv == "1" {
			enableThinking = true
		}
	}

	sd, err := proxy.SpawnDaemon(proxy.SpawnInput{
		UpstreamBaseURL: upstream,
		UpstreamAuth:    upstreamAuth,
		UpstreamAPIKey:  upstreamAPIKey,
		EnableThinking:  &enableThinking,
	}, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to spawn openai-chat proxy: %w", err)
	}

	p2 := *p
	p2.BaseURL = sd.Address()
	p2.AuthToken = sd.SharedSecret
	p2.APIKey = ""
	return &p2, nil
}

func printOpenAIChatBanner(addr, upstream string) {
	fmt.Printf("\x1B[36m[ccx]\x1B[0m OpenAI-chat proxy: %s → %s\n", addr, upstream)
}
