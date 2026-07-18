//go:build windows

// Package procutil은 프록시 데몬들이 공유하는 프로세스 생존 판정 유틸리티.
package procutil

import "syscall"

// PROCESS_QUERY_LIMITED_INFORMATION — syscall 패키지에 상수가 없어 직접 정의.
const processQueryLimitedInformation = 0x1000

// Alive는 Windows에서 OpenProcess + WaitForSingleObject(0ms)로 프로세스 생존을 판정한다.
//
// unix식 Signal(0)은 Windows에서 항상 "not supported" 에러를 내 영원히 살아있다고
// 오판한다(데몬이 부모 종료를 감지 못 해 누수). 또한 종료된 프로세스라도 다른 프로세스가
// 핸들을 쥐고 있으면 OpenProcess가 성공하므로, 핸들 획득 여부가 아니라 signaled 상태
// (= 종료됨)까지 확인해야 한다.
//
// 매 호출 PID로 핸들을 새로 열기 때문에 PID 재사용 오판 가능성이 있다 — 반복 polling에는
// 핸들을 보유하는 Watcher를 사용할 것.
func Alive(pid int) bool {
	h, err := syscall.OpenProcess(syscall.SYNCHRONIZE|processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		// 접근 거부는 존재한다는 뜻. 그 외(INVALID_PARAMETER 등)는 종료로 판정.
		return err == syscall.ERROR_ACCESS_DENIED
	}
	defer syscall.CloseHandle(h)
	return aliveByHandle(h)
}

func aliveByHandle(h syscall.Handle) bool {
	ev, err := syscall.WaitForSingleObject(h, 0)
	if err != nil {
		// 판정 불가 — 보수적으로 살아있다고 보고 다음 polling에 재시도.
		return true
	}
	// WAIT_TIMEOUT = 아직 실행 중. WAIT_OBJECT_0(signaled) = 종료됨.
	return ev == uint32(syscall.WAIT_TIMEOUT)
}

// Watcher는 특정 PID를 반복 polling하는 핸들. 생성 시 프로세스 핸들을 한 번 열어 보유한다 —
// Windows는 열린 핸들이 있는 동안 해당 PID를 재활용하지 않으므로, 부모 사망과 polling tick
// 사이에 같은 PID가 새 프로세스에 재할당되어 영원히 살아있다고 오판하는 레이스가 원천 차단된다.
type Watcher struct {
	pid    int
	handle syscall.Handle // 0이면 열기 실패 — per-call Alive 폴백
}

// NewWatcher는 pid 감시자를 만든다. 감시 시작 시점에 대상이 살아있을 때 호출해야
// 핸들 보유가 성립한다. 열기 실패(ACCESS_DENIED 등) 시에도 유효한 Watcher를 반환하며
// per-call Alive로 폴백한다.
func NewWatcher(pid int) *Watcher {
	h, err := syscall.OpenProcess(syscall.SYNCHRONIZE|processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		return &Watcher{pid: pid}
	}
	return &Watcher{pid: pid, handle: h}
}

// Alive는 감시 대상이 아직 살아있는지 판정한다.
func (w *Watcher) Alive() bool {
	if w.handle == 0 {
		return Alive(w.pid)
	}
	return aliveByHandle(w.handle)
}

// Close는 보유한 프로세스 핸들을 해제한다. 멱등.
func (w *Watcher) Close() {
	if w.handle != 0 {
		_ = syscall.CloseHandle(w.handle)
		w.handle = 0
	}
}
