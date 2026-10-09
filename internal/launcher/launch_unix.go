//go:build !windows

package launcher

import (
	"os/exec"
	"syscall"

	"github.com/channel-spoonai/ccx/internal/config"
	"github.com/channel-spoonai/ccx/internal/ctxwin"
)

// Launch는 syscall.Exec로 현재 ccx 프로세스를 claude로 교체한다.
// 자식 프로세스로 띄우면 ccx의 백그라운드 stdin reader goroutine
// (internal/menu/term.go)이 fd 0에서 계속 read하면서 사용자가 입력하는
// 바이트 일부를 가로채어 한글 같은 UTF-8 multi-byte 시퀀스가 split된다.
// 프로세스 교체 시 ccx 상태가 통째로 사라져 race가 원천적으로 사라진다.
func Launch(p *config.Profile, args []string) error {
	binary, err := exec.LookPath(ClaudeCmd)
	if err != nil {
		return errClaudeNotFound
	}

	// 모델 ID의 컨텍스트 suffix/카탈로그 수치를 Claude Code가 인식하는
	// 형태([1m] / MAX_CONTEXT_TOKENS)로 정규화 — 4개 auth 경로 공통.
	p, ctxRes := ctxwin.Apply(p)

	// 로컬 모델 서버면 auto 모드 분류기와 away summary를 끈다 — prepare가 baseUrl을
	// 127.0.0.1 프록시로 바꾸기 전에 원래 호스트로 판정해야 한다.
	p, args, localAdj := applyLocalDefaults(p, args)

	// settings.json의 정식 Claude 모델 ID는 커스텀 업스트림에 없다 — 같은 계열 별칭으로 고정한다.
	choice := modelChoice(args)
	p, localAdj.SettingsPin = ctxwin.GuardSettingsModel(p, choice)

	// 어떤 티어도 1M을 선언하지 않으면 Claude Code 기본 모델 opus[1m]의 1M 과대 인식을 막는다.
	p, localAdj.Guard1M = ctxwin.Guard1M(p, choice)

	if p.Auth == AuthCodexOAuth {
		prepared, err := prepareCodexOAuth(p)
		if err != nil {
			return err
		}
		printBanner(prepared, ctxRes)
		printLocalAdjustments(localAdj)
		printCodexOAuthBanner(prepared.BaseURL, "")
		warnUpstreamTLSIgnored(p)
		argv := append([]string{binary}, args...)
		return syscall.Exec(binary, argv, BuildEnv(prepared))
	}

	if p.Auth == AuthOpenAIResponses {
		endpoint, err := openAIResponsesEndpoint(p)
		if err != nil {
			return err
		}
		prepared, err := prepareOpenAIResponses(p)
		if err != nil {
			return err
		}
		printBanner(prepared, ctxRes)
		printLocalAdjustments(localAdj)
		printOpenAIResponsesBanner(prepared.BaseURL, endpoint)
		warnUpstreamTLSIgnored(p)
		argv := append([]string{binary}, args...)
		return syscall.Exec(binary, argv, BuildEnv(prepared))
	}

	if p.Auth == AuthAnthropic {
		upstreamURL := ResolveSecret(p.BaseURL)
		normalize := anthropicNormalizeEnabled(p)
		effortMap := anthropicEffortMap(p)
		tlsOpt, err := resolveUpstreamTLS(p)
		if err != nil {
			return err
		}
		fixUsage := anthropicFixUsage(p, tlsOpt)
		prepared, err := prepareAnthropic(p, fixUsage.Enabled, tlsOpt)
		if err != nil {
			return err
		}
		printBanner(prepared, ctxRes)
		printLocalAdjustments(localAdj)
		printAnthropicBanner(prepared.BaseURL, upstreamURL, normalize, effortMap, fixUsage)
		printUpstreamTLS(tlsOpt)
		argv := append([]string{binary}, args...)
		return syscall.Exec(binary, argv, BuildEnv(prepared))
	}

	if p.Auth == AuthOpenAIChat {
		upstreamURL := ResolveSecret(p.BaseURL)
		tlsOpt, err := resolveUpstreamTLS(p)
		if err != nil {
			return err
		}
		prepared, err := prepareOpenAIChat(p, tlsOpt)
		if err != nil {
			return err
		}
		printBanner(prepared, ctxRes)
		printLocalAdjustments(localAdj)
		printOpenAIChatBanner(prepared.BaseURL, upstreamURL)
		printUpstreamTLS(tlsOpt)
		argv := append([]string{binary}, args...)
		return syscall.Exec(binary, argv, BuildEnv(prepared))
	}

	env := BuildEnv(p)
	printBanner(p, ctxRes)
	printLocalAdjustments(localAdj)
	warnUpstreamTLSIgnored(p)
	argv := append([]string{binary}, args...)
	return syscall.Exec(binary, argv, env)
}
