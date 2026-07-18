//go:build !windows

// Package procutil은 프록시 데몬들이 공유하는 프로세스 생존 판정 유틸리티.
package procutil

import (
	"errors"
	"os"
	"syscall"
)

// Alive는 pid의 프로세스가 살아있는지 확인한다. Unix에서는 signal 0을 보내 판정.
func Alive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		// ESRCH → 프로세스 없음. EPERM은 권한 문제 (있긴 함).
		if errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.ESRCH) {
			return false
		}
		// EPERM 등은 살아있다고 보고 계속 polling.
		return true
	}
	return true
}

// Watcher는 특정 PID를 반복 polling하는 핸들. Unix에서는 상태가 없고 Alive 호출과 동일 —
// Windows 구현이 PID 재사용 오판을 막기 위해 프로세스 핸들을 보유하는 것과 인터페이스를 맞춘다.
type Watcher struct {
	pid int
}

// NewWatcher는 pid 감시자를 만든다. 감시 시작 시점에 대상이 살아있을 때 호출해야
// Windows 쪽 핸들 보유가 성립한다 (Unix에서는 무관).
func NewWatcher(pid int) *Watcher {
	return &Watcher{pid: pid}
}

// Alive는 감시 대상이 아직 살아있는지 판정한다.
func (w *Watcher) Alive() bool {
	return Alive(w.pid)
}

// Close는 보유 자원을 해제한다. Unix에서는 no-op.
func (w *Watcher) Close() {}
