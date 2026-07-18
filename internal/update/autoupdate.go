package update

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/term"
)

const (
	// AutoUpdateEnv — "0"/"false"/"off"(대소문자 무시)면 시작 시 자동 업데이트 비활성.
	// 미설정/빈 값/그 외 값은 활성(기본값). 수동 `ccx update`에는 영향 없음.
	AutoUpdateEnv = "CCX_AUTO_UPDATE"

	// AutoApplyTimeout — 시작 흐름을 오래 막지 않도록 수동 `ccx update`(5분)보다 짧게.
	AutoApplyTimeout = 60 * time.Second
)

// isTerminal은 테스트에서 오버라이드하는 훅. 스크립트/CI의 무인 `-xSet` 호출에서
// 예고 없는 지연·출력이 파이프라인을 깨뜨리지 않도록 stdin TTY에서만 자동 적용한다
// (internal/menu/term.go의 메뉴 TTY 판정과 동일 기준).
var isTerminal = func() bool { return term.IsTerminal(int(os.Stdin.Fd())) }

// autoUpdateDisabled는 CCX_AUTO_UPDATE opt-out 판정.
func autoUpdateDisabled() bool {
	switch strings.ToLower(os.Getenv(AutoUpdateEnv)) {
	case "0", "false", "off":
		return true
	}
	return false
}

// shouldAutoApply는 자동 적용 게이팅만 판단한다 — 네트워크 접근 없음.
// 쓰기 권한 선검사가 여기 있는 이유: root 소유 경로(/usr/local/bin 등) 설치에서
// 매 실행 API 호출 후 실패하는 잔소리를 만들지 않기 위해 다운로드는커녕 API 호출
// 전에 조용히 걸러낸다. reason은 테스트/디버깅용.
func shouldAutoApply(current, tag string) (ok bool, reason string) {
	if IsDevBuild(current) {
		return false, "dev build"
	}
	if autoUpdateDisabled() {
		return false, "disabled by " + AutoUpdateEnv
	}
	if !isTerminal() {
		return false, "stdin is not a TTY"
	}
	if c := LoadCache(); c != nil && c.AutoUpdateFailedTag != "" && c.AutoUpdateFailedTag == tag {
		return false, "previous auto-update for this tag failed"
	}
	target, err := resolveSelf()
	if err != nil {
		return false, "cannot resolve executable path"
	}
	if err := checkWritable(filepath.Dir(target)); err != nil {
		return false, "no write permission"
	}
	return true, ""
}

// TryAutoUpdate는 시작 시점 자동 업데이트를 시도한다.
// 전제: 호출측이 MaybeNotify로 fresh 캐시에서 새 버전 tag를 확보했을 때만 호출.
//
// 반환 true = 호출측이 updateNotice 알림을 지워도 됨:
//   - 바이너리 교체 성공 (새 버전은 다음 실행부터), 또는
//   - API가 "이미 최신"이라 답해 캐시가 낡은 것으로 판명 (릴리즈 롤백 등).
//
// 반환 false = 게이팅에 걸렸거나 실패 — 기존 알림 폴백 유지.
// 에러를 리턴하지 않는 시그니처가 "launch를 절대 막지 않는다"는 계약이다.
func TryAutoUpdate(ctx context.Context, current, tag string, out io.Writer) bool {
	if ok, _ := shouldAutoApply(current, tag); !ok {
		return false
	}

	fmt.Fprintf(out, "[ccx] New version %s available — updating automatically (set %s=0 to disable)\n", tag, AutoUpdateEnv)

	applied, err := Apply(ctx, current, out)
	if err != nil {
		fmt.Fprintf(out, "[ccx] auto-update failed: %v — run `ccx update` to retry manually\n", err)
		markAutoUpdateFailed(tag)
		return false
	}
	if !applied {
		// 캐시는 새 버전이라 했지만 API는 이미 최신이라 답함 — 캐시가 낡았다
		// (릴리즈 롤백 등). Apply의 no-op 분기가 캐시를 무효화했으므로 알림만 지운다.
		return true
	}

	fmt.Fprintf(out, "[ccx] Updated to %s — takes effect from the next run.\n", tag)
	return true
}
