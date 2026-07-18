package providers

import "fmt"

// ContextSuffix는 감지된 컨텍스트 윈도우를 ccx의 모델 ID suffix 표기로 바꾼다.
// launch 시 ctxwin.Apply가 이 표기를 해석해 [1m] 재작성과
// CLAUDE_CODE_AUTO_COMPACT_WINDOW 주입으로 변환한다. k 단위 내림(floor)이라
// 실제보다 크게 선언하는 일이 없다. 1000 미만은 표기 불가라 생략.
func ContextSuffix(window int) string {
	switch {
	case window < 1_000:
		return ""
	case window >= 1_000_000:
		return "[1m]"
	default:
		return fmt.Sprintf("[%dk]", window/1_000)
	}
}
