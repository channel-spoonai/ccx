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

// bgDone은 백그라운드 fetch 완료 시 close된다. MaybeNotify/WaitBackgroundFetch 모두
// main goroutine에서만 호출되므로 별도 동기화 불필요.
var bgDone chan struct{}

func startBackgroundFetch() {
	bgOnce.Do(func() {
		// fetch 시작 전에 CheckedAt을 선기록한다. Unix는 launch의 syscall.Exec가
		// goroutine을 언제든 죽일 수 있는데(WaitBackgroundFetch 상한 2s < fetch ctx 10s),
		// 결과 기록에만 의존하면 행잉 네트워크 환경에서 캐시가 영영 안 써져
		// 24h 백오프가 발동하지 않고 매 실행 대기 지연이 고착된다.
		// fetch가 완주하면 아래에서 결과로 덮어쓴다.
		_ = SaveCache(CacheEntry{CheckedAt: time.Now()})
		done := make(chan struct{})
		bgDone = done
		go func() {
			defer close(done)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			rel, err := FetchLatest(ctx)
			now := time.Now()
			entry := CacheEntry{CheckedAt: now}
			if err == nil && rel != nil {
				entry.LatestTag = rel.TagName
				entry.LatestURL = rel.HTMLURL
			}
			// API 실패 시엔 선기록된 CheckedAt이 그대로 남아 24h 재시도 억제 —
			// 네트워크 차단 환경 보호.
			_ = SaveCache(entry)
		}()
	})
}

// WaitBackgroundFetch는 진행 중인 백그라운드 fetch가 캐시를 쓸 때까지 최대 max 대기한다.
// fetch가 시작되지 않았거나 이미 끝났으면 즉시 반환.
//
// Unix launch는 syscall.Exec로 프로세스 이미지를 통째로 교체하므로, launch 직전에
// 이 함수를 부르지 않으면 fetch goroutine이 캐시를 쓰기 전에 죽어 캐시가 영영
// fresh해지지 않는다 (-xSet 직행 사용자는 알림/자동 업데이트가 트리거되지 않음).
func WaitBackgroundFetch(max time.Duration) {
	done := bgDone
	if done == nil {
		return
	}
	select {
	case <-done:
	case <-time.After(max):
	}
}
