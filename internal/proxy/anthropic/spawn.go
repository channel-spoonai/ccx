package anthropic

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

// DaemonSubcommand는 ccx 자기 자신을 데몬 모드로 다시 실행할 때 쓰는 sentinel 인자.
// 자동 업데이트로 "구버전 부모 + 신버전 데몬" 조합이 생기므로 이름은 변경 금지, 추가만 허용.
const DaemonSubcommand = "__anthropic-proxy"

// 환경변수: 부모가 자식 데몬에 설정을 전달할 때 쓰는 키들. 마찬가지로 변경 금지.
const (
	CCXProxySecretEnv    = "CCX_ANTHROPIC_PROXY_SECRET"
	CCXProxyParentPIDEnv = "CCX_ANTHROPIC_PROXY_PPID"
	CCXUpstreamURLEnv    = "CCX_ANTHROPIC_UPSTREAM_URL"
	CCXUpstreamAuthEnv   = "CCX_ANTHROPIC_UPSTREAM_AUTH"
	CCXUpstreamAPIKeyEnv = "CCX_ANTHROPIC_UPSTREAM_APIKEY"

	// CCXNormalizeSystemEnv는 "false"/"0"이면 중간 role:"system" 정규화를 끈다.
	CCXNormalizeSystemEnv = "CCX_ANTHROPIC_NORMALIZE_SYSTEM"
)

type SpawnInput struct {
	UpstreamBaseURL string
	UpstreamAuth    string
	UpstreamAPIKey  string
	NormalizeSystem bool
}

type SpawnedDaemon struct {
	Process      *os.Process
	Port         int
	SharedSecret string
}

func (s *SpawnedDaemon) Address() string {
	return fmt.Sprintf("http://127.0.0.1:%d", s.Port)
}

// SpawnDaemon은 ccx 자기 자신을 자식으로 띄운 뒤 ready 메시지를 받아 핸들을 반환한다.
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
		CCXNormalizeSystemEnv+"="+strconv.FormatBool(in.NormalizeSystem),
	)
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

	return &SpawnedDaemon{Process: cmd.Process, Port: port, SharedSecret: secret}, nil
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
		parts := strings.Fields(strings.TrimSpace(line))
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
