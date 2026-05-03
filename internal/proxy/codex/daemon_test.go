package codex

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	auth "github.com/channel-spoonai/ccx/internal/auth/codex"
)

// TestRunDaemon_ReadyWriterReceivesPort은 데몬이 ready 메시지를 한 줄 출력하는지 본다.
func TestRunDaemon_ReadyWriterReceivesPort(t *testing.T) {
	withTempHome(t)

	var ready bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- RunDaemon(DaemonOptions{
			ParentPID:    -1, // polling 비활성
			SharedSecret: "",
			IdleTimeout:  100 * time.Millisecond,
			ReadyWriter:  &ready,
		})
	}()

	// IdleTimeout이 작으니 곧 종료해야 함. ready 메시지는 그 전에 들어가야 함.
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunDaemon: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RunDaemon이 idle timeout 안에 종료하지 않음")
	}

	got := ready.String()
	if !strings.HasPrefix(got, "ready ") || !strings.HasSuffix(got, "\n") {
		t.Errorf("ready 메시지 형식 오류: %q", got)
	}
	parts := strings.Fields(strings.TrimSpace(got))
	if len(parts) != 2 {
		t.Fatalf("ready 메시지가 2개 토큰이 아님: %q", got)
	}
	if port, err := strconv.Atoi(parts[1]); err != nil || port <= 0 {
		t.Errorf("ready 포트 파싱 실패: %q", got)
	}
}

func TestRunDaemon_ParentDeathTriggersShutdown(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PID polling은 unix 전용 가정")
	}
	withTempHome(t)

	// "곧 죽을 부모"를 만든다 — 짧게 sleep 후 종료하는 자식 프로세스.
	parent := exec.Command("sleep", "0.3")
	if err := parent.Start(); err != nil {
		t.Fatal(err)
	}
	ppid := parent.Process.Pid
	// zombie를 reap해야 processAlive가 ESRCH를 받는다 — background wait.
	go func() { _ = parent.Wait() }()

	var ready bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- RunDaemon(DaemonOptions{
			ParentPID:    ppid,
			ReadyWriter:  &ready,
			// IdleTimeout 없음 — 부모 사망만이 종료 트리거가 되어야 함.
		})
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("RunDaemon 종료 에러: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("부모 사망 후 데몬이 5초 안에 종료되지 않음")
	}
}

func TestProcessAlive(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("processAlive는 unix signal 기반")
	}
	// 자기 자신은 살아있어야 함.
	if !processAlive(os.Getpid()) {
		t.Error("processAlive(self) = false")
	}
	// 거의 확실히 존재하지 않는 PID.
	if processAlive(99999999) {
		t.Error("processAlive(huge pid) = true")
	}
}

// TestSpawnDaemon_EndToEnd는 ccx 바이너리를 빌드한 뒤 SpawnDaemon으로 실행해
// 실제 자식 데몬과 핸드셰이크하는 통합 테스트.
func TestSpawnDaemon_EndToEnd(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("ccx 빌드는 unix-only path에서 검증")
	}
	withTempHome(t)
	seedDaemonAuth(t)

	// 이 테스트만을 위해 ccx 바이너리를 임시 빌드.
	binDir := t.TempDir()
	binPath := filepath.Join(binDir, "ccx-test")
	build := exec.Command("go", "build", "-o", binPath, "./cmd/ccx")
	build.Dir = repoRoot(t)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("ccx 빌드 실패: %v\n%s", err, out)
	}

	// SpawnDaemon은 os.Executable()을 self로 쓰므로, 직접 호출하지 않고 바이너리를 직접 spawn.
	// 단 secret/ppid 환경변수와 ready 메시지 파싱은 spawn.go 로직과 동일.
	cmd := exec.Command(binPath, DaemonSubcommand)
	cmd.Env = append(os.Environ(),
		CCXProxySecretEnv+"=testsec",
		CCXProxyParentPIDEnv+"="+strconv.Itoa(os.Getpid()),
		"XDG_CONFIG_HOME="+os.Getenv("XDG_CONFIG_HOME"), // seed된 토큰 위치 공유
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		_, _ = cmd.Process.Wait()
	}()

	port, err := readReady(stdout, 5*time.Second)
	if err != nil {
		t.Fatalf("ready 파싱 실패: %v", err)
	}

	// 헬스체크 호출.
	resp, err := http.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/healthz")
	if err != nil {
		t.Fatalf("healthz 요청 실패: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("healthz status: %d", resp.StatusCode)
	}

	// 잘못된 secret으로 메시지 호출 → 401.
	req, _ := http.NewRequest("POST", "http://127.0.0.1:"+strconv.Itoa(port)+"/v1/messages",
		strings.NewReader(`{"model":"gpt-5.4","messages":[]}`))
	req.Header.Set("Authorization", "Bearer wrong")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Errorf("잘못된 secret status: %d, want 401", resp.StatusCode)
	}
}

// repoRoot는 test 디렉토리에서 ccx 모듈 루트로 올라간다. cmd/ccx 빌드 시 -C 가 필요.
func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// internal/proxy/codex → 3단계 위.
	return filepath.Clean(filepath.Join(wd, "..", "..", ".."))
}

// seedDaemonAuth는 데몬이 'not authenticated' 에러로 죽지 않도록 디스크에 토큰을 심어둔다.
// healthz/잘못된 secret 검증에는 토큰이 실제로 필요 없지만, 원래 흐름의 안전망.
func seedDaemonAuth(t *testing.T) {
	t.Helper()
	if err := auth.SaveAuth(&auth.StoredAuth{
		AccessToken:  "atk",
		RefreshToken: "rtk",
		ExpiresAt:    time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
}

func TestReadReady_TimeoutSurfaced(t *testing.T) {
	// 절대 안 보내는 reader.
	pr, pw := io.Pipe()
	defer pw.Close()
	defer pr.Close()
	_, err := readReady(pr, 50*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Errorf("timeout 에러여야 함: %v", err)
	}
}

func TestReadReady_MalformedRejected(t *testing.T) {
	r := strings.NewReader("not-ready 12345\n")
	_, err := readReady(r, 1*time.Second)
	if err == nil || !strings.Contains(err.Error(), "형식 오류") {
		t.Errorf("형식 오류로 거부되어야 함: %v", err)
	}
}

func TestReadReady_Valid(t *testing.T) {
	r := strings.NewReader("ready 18765\n")
	got, err := readReady(r, 1*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if got != 18765 {
		t.Errorf("got %d", got)
	}
}

// Shutdown 시 컨텍스트 deadline이 만료돼도 graceful 종료가 잘 끝나는지.
func TestServer_Shutdown_NoDeadlock(t *testing.T) {
	withTempHome(t)
	s, _ := Start(ServerOptions{})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("unexpected shutdown error: %v", err)
	}
}
