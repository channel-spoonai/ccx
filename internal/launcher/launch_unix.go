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
	// 형태([1m] + AUTO_COMPACT_WINDOW)로 정규화 — 4개 auth 경로 공통.
	p, ctxRes := ctxwin.Apply(p)

	if p.Auth == AuthCodexOAuth {
		prepared, err := prepareCodexOAuth(p)
		if err != nil {
			return err
		}
		printBanner(prepared, ctxRes)
		printCodexOAuthBanner(prepared.BaseURL, "")
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
		printOpenAIResponsesBanner(prepared.BaseURL, endpoint)
		argv := append([]string{binary}, args...)
		return syscall.Exec(binary, argv, BuildEnv(prepared))
	}

	if p.Auth == AuthOpenAIChat {
		upstreamURL := ResolveSecret(p.BaseURL)
		prepared, err := prepareOpenAIChat(p)
		if err != nil {
			return err
		}
		printBanner(prepared, ctxRes)
		printOpenAIChatBanner(prepared.BaseURL, upstreamURL)
		argv := append([]string{binary}, args...)
		return syscall.Exec(binary, argv, BuildEnv(prepared))
	}

	env := BuildEnv(p)
	printBanner(p, ctxRes)
	argv := append([]string{binary}, args...)
	return syscall.Exec(binary, argv, env)
}
