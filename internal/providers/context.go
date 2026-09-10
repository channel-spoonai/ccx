package providers

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

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

// contextSuffixRe는 ccx가 모델 ID에 박아 두는 컨텍스트 표기.
var contextSuffixRe = regexp.MustCompile(`(?i)\[[0-9]+(\.[0-9]+)?[km]\]$`)

// StripContextSuffix는 모델 ID에서 ccx 컨텍스트 표기를 떼어낸다.
func StripContextSuffix(model string) string {
	return contextSuffixRe.ReplaceAllString(model, "")
}

// ParseContextInput은 사용자가 입력한 컨텍스트 길이를 토큰 수로 바꾼다.
// "262144", "262k", "262K", "1m", "1M" 을 받고 빈 문자열이면 0(=표기 생략).
// 해석할 수 없으면 ok=false — 호출 측이 다시 묻는다.
func ParseContextInput(raw string) (int, bool) {
	s := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(raw, ",", "")))
	if s == "" {
		return 0, true
	}
	mult := 1
	switch {
	case strings.HasSuffix(s, "k"):
		mult, s = 1_000, strings.TrimSuffix(s, "k")
	case strings.HasSuffix(s, "m"):
		mult, s = 1_000_000, strings.TrimSuffix(s, "m")
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f <= 0 {
		return 0, false
	}
	n := int(f * float64(mult))
	if n <= 0 {
		return 0, false
	}
	return n, true
}
