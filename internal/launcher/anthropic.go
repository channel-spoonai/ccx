package launcher

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/channel-spoonai/ccx/internal/config"
	proxy "github.com/channel-spoonai/ccx/internal/proxy/anthropic"
)

// AuthAnthropic은 Profile.Auth 값. Anthropic 호환 업스트림에 요청을 **번역 없이** 중계하는
// 로컬 패스스루 프록시를 띄운다. openai-chat과 달리 페이로드 형식은 그대로다.
//
// 존재 이유는 하나 — Claude Code가 anthropic-beta `mid-conversation-system-2026-04-07`로
// 대화 중간에 role:"system" 메시지를 턴마다 추가하는데, Qwen 계열 chat template은 중간 system을
// 거부(`System message must be at the beginning.`)하므로 서버가 재배치한다. 그 결과가 턴마다
// 달라져 프롬프트 앞부분이 흔들리고 직전 턴의 KV 스냅샷이 접두가 아니게 된다.
// 프록시가 이 메시지를 role:"user"로 정규화하면 프롬프트가 다시 append-only가 된다.
// (MTPLX 실측: 턴 재사용률 85.1% → 100.0%.)
const AuthAnthropic = "anthropic"

// prepareAnthropic은 패스스루 프록시를 띄우고 BaseURL/AuthToken을 주입한 profile copy를 준다.
// 정규화는 기본 ON. profile.env에 CCX_ANTHROPIC_NORMALIZE_SYSTEM=false 로 끌 수 있다.
func prepareAnthropic(p *config.Profile) (*config.Profile, error) {
	upstream := ResolveSecret(p.BaseURL)
	if upstream == "" {
		return nil, errors.New("profile.baseUrl is empty — set it to the upstream Anthropic-compatible server (e.g. http://localhost:8000)")
	}

	normalize := true
	if v, ok := p.Env[proxy.CCXNormalizeSystemEnv]; ok {
		rv := ResolveSecret(v)
		if rv == "false" || rv == "0" || rv == "off" {
			normalize = false
		}
	}

	sd, err := proxy.SpawnDaemon(proxy.SpawnInput{
		UpstreamBaseURL: upstream,
		UpstreamAuth:    ResolveSecret(p.AuthToken),
		UpstreamAPIKey:  ResolveSecret(p.APIKey),
		NormalizeSystem: normalize,
		SessionHeader:   strings.TrimSpace(p.SessionHeader),
	}, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to spawn anthropic proxy: %w", err)
	}

	p2 := *p
	p2.BaseURL = sd.Address()
	p2.AuthToken = sd.SharedSecret
	p2.APIKey = ""
	return &p2, nil
}

func printAnthropicBanner(addr, upstream string, normalize bool) {
	note := ""
	if normalize {
		note = " (mid-conversation system → user)"
	}
	fmt.Printf("\x1B[36m[ccx]\x1B[0m Anthropic passthrough proxy: %s → %s%s\n", addr, upstream, note)
}

// anthropicNormalizeEnabled는 배너 출력용 — prepareAnthropic과 동일한 판정.
func anthropicNormalizeEnabled(p *config.Profile) bool {
	if v, ok := p.Env[proxy.CCXNormalizeSystemEnv]; ok {
		rv := ResolveSecret(v)
		if rv == "false" || rv == "0" || rv == "off" {
			return false
		}
	}
	return true
}
