//go:build !windows

package launcher

import (
	"os/exec"
	"syscall"

	"github.com/channel-spoonai/ccx/internal/config"
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

	if p.Auth == AuthCodexOAuth {
		prepared, err := prepareCodexOAuth(p)
		if err != nil {
			return err
		}
		printBanner(prepared)
		printCodexOAuthBanner(prepared.BaseURL, "")
		argv := append([]string{binary}, args...)
		return syscall.Exec(binary, argv, BuildEnv(prepared))
	}

	env := BuildEnv(p)
	printBanner(p)
	argv := append([]string{binary}, args...)
	return syscall.Exec(binary, argv, env)
}
