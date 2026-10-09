//go:build windows

package launcher

import (
	"errors"
	"os"
	"os/exec"

	"github.com/channel-spoonai/ccx/internal/config"
	"github.com/channel-spoonai/ccx/internal/ctxwin"
)

// Launch는 Windows에서 claude를 자식 프로세스로 실행하고 종료 코드를 전파한다.
// Windows에는 syscall.Exec 등가물이 없어 프로세스 교체가 불가능하다.
// 같은 메뉴 reader goroutine이 콘솔 input handle을 공유하지만, Windows
// 콘솔의 키 이벤트 큐 동작상 Unix만큼 race가 두드러지지 않는다.
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
		return runChildClaude(binary, args, BuildEnv(prepared))
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
		return runChildClaude(binary, args, BuildEnv(prepared))
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
		return runChildClaude(binary, args, BuildEnv(prepared))
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
		return runChildClaude(binary, args, BuildEnv(prepared))
	}

	env := BuildEnv(p)
	printBanner(p, ctxRes)
	printLocalAdjustments(localAdj)
	warnUpstreamTLSIgnored(p)
	return runChildClaude(binary, args, env)
}

func runChildClaude(binary string, args []string, env []string) error {
	cmd := exec.Command(binary, args...)
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.ExitCode())
		}
		return err
	}
	return nil
}
