//go:build windows

package update

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// atomicReplace: Windows는 실행 중인 PE를 덮어쓸 수 없지만 rename은 가능.
// target → target.old-<pid> 로 옮긴 뒤 새 파일을 target 이름으로 이동.
// 실패 시 .old를 복원.
//
// .old 이름을 PID로 유니크화하는 이유: ccx는 claude의 부모로 세션 내내 생존하므로
// 자동 업데이트를 거친 프로세스의 이미지 파일(.old)이 세션 종료까지 잠긴다.
// 고정 이름이면 구 세션이 살아있는 동안 다음 업데이트의 rename이 실패한다.
func atomicReplace(target, newFile string) error {
	old := target + ".old-" + strconv.Itoa(os.Getpid())
	_ = os.Remove(old) // 이전 잔재 청소 (PID 재사용 대비)
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

// CleanupStaleBinary는 main 진입 직후 호출되어 이전 ccx update 사이클이 남긴
// ccx.exe.old* 잔재(레거시 .old 포함)와 중단된 업데이트의 임시파일을
// best-effort로 청소한다. 아직 세션이 살아있어 잠긴 .old는 조용히 실패.
func CleanupStaleBinary() {
	exe, err := resolveSelf()
	if err != nil {
		return
	}
	dir := filepath.Dir(exe)
	oldPrefix := filepath.Base(exe) + ".old"
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), oldPrefix) {
				_ = os.Remove(filepath.Join(dir, e.Name()))
			}
		}
	}
	cleanupStaleTemps(dir)
}
