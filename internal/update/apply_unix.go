//go:build !windows

package update

import (
	"os"
	"path/filepath"
)

// atomicReplace는 같은 파일시스템 내에서 inode 교체로 실행 중인 바이너리도 안전하게 갱신.
// POSIX rename(2) 보장에 의존.
func atomicReplace(target, newFile string) error {
	return os.Rename(newFile, target)
}

// CleanupStaleBinary는 Unix에서 .old 잔재가 없지만(rename이 inode 교체),
// Ctrl-C로 중단된 업데이트의 임시파일(ccx-dl-*, ccx-new-*)은 남을 수 있어 청소한다.
func CleanupStaleBinary() {
	exe, err := resolveSelf()
	if err != nil {
		return
	}
	cleanupStaleTemps(filepath.Dir(exe))
}
