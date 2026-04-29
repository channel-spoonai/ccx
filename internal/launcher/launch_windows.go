//go:build windows

package launcher

import (
	"errors"
	"os"
	"os/exec"

	"github.com/channel-spoonai/ccx/internal/config"
)

// Launch는 Windows에서 claude를 자식 프로세스로 실행하고 종료 코드를 전파한다.
// Windows에는 syscall.Exec 등가물이 없어 프로세스 교체가 불가능하다.
// 같은 메뉴 reader goroutine이 콘솔 input handle을 공유하지만, Windows
// 콘솔의 키 이벤트 큐 동작상 Unix만큼 race가 두드러지지 않는다.
func Launch(p *config.Profile, args []string) error {
	env := BuildEnv(p)
	printBanner(p)

	binary, err := exec.LookPath(ClaudeCmd)
	if err != nil {
		return errClaudeNotFound
	}

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
