//go:build windows

package update

import "os"

// atomicReplace: Windows는 실행 중인 PE를 덮어쓸 수 없지만 rename은 가능.
// target → target.old 로 옮긴 뒤 새 파일을 target 이름으로 이동.
// 실패 시 .old를 복원.
func atomicReplace(target, newFile string) error {
	old := target + ".old"
	_ = os.Remove(old) // 이전 잔재 청소
	if err := os.Rename(target, old); err != nil {
		return err
	}
	if err := os.Rename(newFile, target); err != nil {
		_ = os.Rename(old, target)
		return err
	}
	// .old는 사용 중이라 지금 못 지움. CleanupStaleBinary가 다음 실행에서 처리.
	return nil
}

// CleanupStaleBinary는 Windows에서 main 진입 직후 호출되어
// 이전 ccx update 사이클이 남긴 ccx.exe.old 잔재를 best-effort로 청소한다.
func CleanupStaleBinary() {
	exe, err := resolveSelfPath()
	if err != nil {
		return
	}
	_ = os.Remove(exe + ".old")
}
