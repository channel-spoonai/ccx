package update

import (
	"context"
	"sync"
	"time"
)

// MaybeNotify는 ccx 시작 시점에 한 번 호출된다.
//
// 동작:
//   - dev 빌드면 즉시 빈 문자열 반환 (자동 알림 비활성).
//   - 캐시가 fresh(<24h)하면 캐시된 latest와 current 비교 → 새 버전이면 안내 문자열 반환.
//   - 캐시가 stale하거나 없으면 백그라운드 goroutine으로 fetch + cache write.
//     이번 실행은 알림 생략 (다음 실행에 반영). 사용자 동선을 막지 않기 위해.
//   - 반환 문자열 형식: "v0.4.0" — 호출측에서 메뉴 헤더/stderr에 자유롭게 포맷.
//     빈 문자열이면 알림 없음.
func MaybeNotify(current string) string {
	if IsDevBuild(current) {
		return ""
	}

	cache := LoadCache()
	if cache.Fresh(time.Now()) {
		if cache != nil && cache.LatestTag != "" && Compare(cache.LatestTag, current) > 0 {
			return cache.LatestTag
		}
		return ""
	}

	// Stale — 백그라운드 fetch. 이번 실행은 그냥 통과.
	startBackgroundFetch()
	return ""
}

// 같은 프로세스 안에서 중복 호출 방지.
var bgOnce sync.Once

func startBackgroundFetch() {
	bgOnce.Do(func() {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			rel, err := FetchLatest(ctx)
			now := time.Now()
			entry := CacheEntry{CheckedAt: now}
			if err == nil && rel != nil {
				entry.LatestTag = rel.TagName
				entry.LatestURL = rel.HTMLURL
			}
			// API 실패해도 CheckedAt만 갱신 — 24h 동안 재시도 안 해 네트워크 차단 환경 보호.
			_ = SaveCache(entry)
		}()
	})
}
