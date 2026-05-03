package codex

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
// `ccx __codex-proxy ...` 형태로 호출되며 사용자에게 노출되지 않는다.
const DaemonSubcommand = "__codex-proxy"

// CCXProxySecretEnv는 부모가 자식 데몬에 shared secret을 전달할 때 쓰는 환경변수 이름.
const CCXProxySecretEnv = "CCX_PROXY_SECRET"

// CCXProxyParentPIDEnv는 자식 데몬이 polling할 부모 PID.
const CCXProxyParentPIDEnv = "CCX_PROXY_PPID"

// SpawnedDaemon는 부모가 자식 데몬을 spawn했을 때 핸들.
type SpawnedDaemon struct {
	Process      *os.Process
	Port         int
	SharedSecret string
}

// Address는 ANTHROPIC_BASE_URL로 쓸 URL.
func (s *SpawnedDaemon) Address() string {
	return fmt.Sprintf("http://127.0.0.1:%d", s.Port)
}

// SpawnDaemon은 ccx 자기 자신을 자식 프로세스로 fork한 뒤 ready 메시지를 받아
// SpawnedDaemon 핸들을 반환한다. 부모는 이후 syscall.Exec(claude)로 전환할 수 있다 —
// PID는 보존되므로 자식의 ppid polling이 자연스럽게 claude를 watch한다.
//
// readyTimeout 안에 자식이 "ready <port>\n" 을 출력하지 못하면 자식을 죽이고 에러.
func SpawnDaemon(readyTimeout time.Duration) (*SpawnedDaemon, error) {
	self, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("self path 조회 실패: %w", err)
	}
	secret, err := newSharedSecret()
	if err != nil {
		return nil, err
	}

	cmd := exec.Command(self, DaemonSubcommand)
	cmd.Env = append(os.Environ(),
		CCXProxySecretEnv+"="+secret,
		CCXProxyParentPIDEnv+"="+strconv.Itoa(os.Getpid()),
	)
	// 자식 stderr는 부모로 그대로 흘려서 디버그 메시지가 잡히도록 함.
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe 생성 실패: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("자식 프록시 spawn 실패: %w", err)
	}

	// "ready PORT\n" 한 줄을 timeout 안에 받는다.
	port, err := readReady(stdout, readyTimeout)
	if err != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		return nil, err
	}

	// ready를 받은 뒤에는 자식 stdout pipe를 닫는다 — 자식은 더 이상 쓰지 않는다.
	// (자식이 SIGPIPE를 받아도 Go 런타임이 EPIPE로 무시 처리)
	_ = stdout.Close()

	// 자식을 detach: 부모가 syscall.Exec(claude)로 사라져도 wait 대기가 의미 없으므로
	// Process는 그대로 두고 Release만 한다.
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
			done <- result{err: fmt.Errorf("자식 ready 출력 읽기 실패: %w", err)}
			return
		}
		line = strings.TrimSpace(line)
		// 형식: "ready <port>"
		parts := strings.Fields(line)
		if len(parts) != 2 || parts[0] != "ready" {
			done <- result{err: fmt.Errorf("자식 ready 메시지 형식 오류: %q", line)}
			return
		}
		port, err := strconv.Atoi(parts[1])
		if err != nil || port <= 0 {
			done <- result{err: fmt.Errorf("자식 ready 포트 파싱 실패: %q", line)}
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
		return 0, errors.New("자식 프록시가 timeout 안에 ready 신호를 보내지 않음")
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
// main.go가 진입 직후 호출해 데몬 코드로 분기.
func IsDaemonInvocation(args []string) bool {
	return len(args) >= 2 && args[1] == DaemonSubcommand
}
