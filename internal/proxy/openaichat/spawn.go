package openaichat

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// DaemonSubcommand는 ccx 자기 자신을 데몬 모드로 다시 실행할 때 사용하는 sentinel 인자.
const DaemonSubcommand = "__openai-chat-proxy"

// 환경변수: 부모가 자식 데몬에 설정을 전달할 때 쓰는 키들.
const (
	CCXProxySecretEnv      = "CCX_OPENAICHAT_PROXY_SECRET"
	CCXProxyParentPIDEnv   = "CCX_OPENAICHAT_PROXY_PPID"
	CCXUpstreamURLEnv      = "CCX_OPENAICHAT_UPSTREAM_URL"
	CCXUpstreamAuthEnv     = "CCX_OPENAICHAT_UPSTREAM_AUTH"
	CCXUpstreamAPIKeyEnv   = "CCX_OPENAICHAT_UPSTREAM_APIKEY"
	CCXEnableThinkingEnv   = "CCX_OPENAICHAT_ENABLE_THINKING" // "true" / "false" — unset이면 필드 미전송
)

// SpawnInput은 부모가 SpawnDaemon에 전달하는 정보.
type SpawnInput struct {
	UpstreamBaseURL string
	UpstreamAuth    string
	UpstreamAPIKey  string

	// EnableThinking은 nil이면 enable_thinking 필드를 보내지 않고, 값이 있으면 그대로 전달.
	// lightning-mlx 등 reasoning 모델은 false 권장 — Claude Code는 reasoning_content를 활용 못 함.
	EnableThinking *bool
}

type SpawnedDaemon struct {
	Process      *os.Process
	Port         int
	SharedSecret string
}

func (s *SpawnedDaemon) Address() string {
	return fmt.Sprintf("http://127.0.0.1:%d", s.Port)
}

// SpawnDaemon은 ccx 자기 자신을 자식으로 fork한 뒤 ready 메시지를 받아
// SpawnedDaemon 핸들을 반환한다. 부모는 이후 syscall.Exec(claude)로 전환할 수 있다.
func SpawnDaemon(in SpawnInput, readyTimeout time.Duration) (*SpawnedDaemon, error) {
	self, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("failed to look up self path: %w", err)
	}
	secret, err := newSharedSecret()
	if err != nil {
		return nil, err
	}

	cmd := exec.Command(self, DaemonSubcommand)
	cmd.Env = append(os.Environ(),
		CCXProxySecretEnv+"="+secret,
		CCXProxyParentPIDEnv+"="+strconv.Itoa(os.Getpid()),
		CCXUpstreamURLEnv+"="+in.UpstreamBaseURL,
		CCXUpstreamAuthEnv+"="+in.UpstreamAuth,
		CCXUpstreamAPIKeyEnv+"="+in.UpstreamAPIKey,
	)
	if in.EnableThinking != nil {
		val := "false"
		if *in.EnableThinking {
			val = "true"
		}
		cmd.Env = append(cmd.Env, CCXEnableThinkingEnv+"="+val)
	}
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to spawn child proxy: %w", err)
	}

	port, err := readReady(stdout, readyTimeout)
	if err != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		return nil, err
	}

	_ = stdout.Close()
	_ = cmd.Process.Release()

	return &SpawnedDaemon{
		Process:      cmd.Process,
		Port:         port,
		SharedSecret: secret,
	}, nil
}

func readReady(r interface{ Read(p []byte) (int, error) }, timeout time.Duration) (int, error) {
	type result struct {
		port int
		err  error
	}
	done := make(chan result, 1)
	go func() {
		br := bufio.NewReader(r)
		line, err := br.ReadString('\n')
		if err != nil {
			done <- result{err: fmt.Errorf("failed to read child ready output: %w", err)}
			return
		}
		line = strings.TrimSpace(line)
		parts := strings.Fields(line)
		if len(parts) != 2 || parts[0] != "ready" {
			done <- result{err: fmt.Errorf("malformed child ready message: %q", line)}
			return
		}
		port, err := strconv.Atoi(parts[1])
		if err != nil || port <= 0 {
			done <- result{err: fmt.Errorf("failed to parse child ready port: %q", line)}
			return
		}
		done <- result{port: port}
	}()

	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case r := <-done:
		return r.port, r.err
	case <-t.C:
		return 0, errors.New("child proxy did not signal ready within timeout")
	}
}

func newSharedSecret() (string, error) {
	var buf [24]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf[:]), nil
}

// IsDaemonInvocation은 os.Args 첫 인자가 hidden 서브명령인지 본다.
func IsDaemonInvocation(args []string) bool {
	return len(args) >= 2 && args[1] == DaemonSubcommand
}
