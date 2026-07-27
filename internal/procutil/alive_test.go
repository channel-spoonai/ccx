package procutil

import (
	"os"
	"os/exec"
	"runtime"
	"testing"
)

func helperCmd() *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.Command("cmd", "/c", "exit 0")
	}
	return exec.Command("true")
}

func TestAlive_Self(t *testing.T) {
	if !Alive(os.Getpid()) {
		t.Fatal("현재 프로세스가 dead로 판정됨")
	}
}

func TestAlive_ExitedProcess(t *testing.T) {
	cmd := helperCmd()
	if err := cmd.Start(); err != nil {
		t.Skipf("헬퍼 프로세스 시작 실패: %v", err)
	}
	pid := cmd.Process.Pid
	if err := cmd.Wait(); err != nil {
		t.Fatalf("헬퍼 프로세스 종료 대기 실패: %v", err)
	}
	if Alive(pid) {
		t.Fatalf("종료된 pid %d가 alive로 판정됨", pid)
	}
}

func TestWatcher_Self(t *testing.T) {
	w := NewWatcher(os.Getpid())
	defer w.Close()
	if !w.Alive() {
		t.Fatal("현재 프로세스 watcher가 dead로 판정됨")
	}
}

// 살아있는 동안 watcher를 만들고 종료 후 dead로 판정되는지 — 핸들 보유 경로의 핵심 검증.
// Windows에서는 보유 핸들 덕에 PID가 재활용되지 않아 결정적으로 동작한다.
func TestWatcher_DetectsExit(t *testing.T) {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", "ping -n 1 127.0.0.1 > NUL")
	} else {
		cmd = exec.Command("sleep", "0.2")
	}
	if err := cmd.Start(); err != nil {
		t.Skipf("헬퍼 프로세스 시작 실패: %v", err)
	}
	w := NewWatcher(cmd.Process.Pid)
	defer w.Close()
	if !w.Alive() {
		t.Error("실행 중인 헬퍼가 dead로 판정됨")
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("헬퍼 프로세스 종료 대기 실패: %v", err)
	}
	if w.Alive() {
		t.Error("종료된 헬퍼가 alive로 판정됨")
	}
}

func TestWatcher_ExitedBeforeCreate(t *testing.T) {
	cmd := helperCmd()
	if err := cmd.Start(); err != nil {
		t.Skipf("헬퍼 프로세스 시작 실패: %v", err)
	}
	pid := cmd.Process.Pid
	if err := cmd.Wait(); err != nil {
		t.Fatalf("헬퍼 프로세스 종료 대기 실패: %v", err)
	}
	w := NewWatcher(pid)
	defer w.Close()
	if w.Alive() {
		t.Fatalf("이미 종료된 pid %d의 watcher가 alive로 판정됨", pid)
	}
}
