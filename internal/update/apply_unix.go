//go:build !windows

package update

import "os"

// atomicReplace는 같은 파일시스템 내에서 inode 교체로 실행 중인 바이너리도 안전하게 갱신.
// POSIX rename(2) 보장에 의존.
func atomicReplace(target, newFile string) error {
	return os.Rename(newFile, target)
}

// CleanupStaleBinary는 windows 전용 — Unix는 no-op.
func CleanupStaleBinary() {}
