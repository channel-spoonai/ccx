package update

import (
	"strconv"
	"strings"
)

// IsDevBuild는 ldflags로 주입되지 않은 개발 빌드를 식별한다.
// goreleaser가 정식 릴리즈에서 주입하는 값(예: "0.4.0")만 자동 업데이트 대상.
func IsDevBuild(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" || v == "dev" || v == "unknown" {
		return true
	}
	// goreleaser snapshot: "0.4.1-next", "0.4.0-next-abcdef"
	if strings.Contains(v, "-next") || strings.HasSuffix(v, "-dirty") {
		return true
	}
	return false
}

// StripV는 "v0.4.0" → "0.4.0", "0.4.0" → "0.4.0".
func StripV(s string) string {
	return strings.TrimPrefix(strings.TrimSpace(s), "v")
}

// Compare는 semver 두 개를 비교한다. -1이면 a<b, 0이면 동일, 1이면 a>b.
// 잘못된 입력은 모두 0으로 취급해 알림이 잘못 뜨지 않도록 보수적으로 처리.
// prerelease(예: "0.4.0-rc.1")는 정식 버전("0.4.0")보다 낮다.
func Compare(a, b string) int {
	am, an, ap, apre, ok := parseSemver(a)
	bm, bn, bp, bpre, ok2 := parseSemver(b)
	if !ok || !ok2 {
		return 0
	}
	if c := cmpInt(am, bm); c != 0 {
		return c
	}
	if c := cmpInt(an, bn); c != 0 {
		return c
	}
	if c := cmpInt(ap, bp); c != 0 {
		return c
	}
	return cmpPre(apre, bpre)
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// cmpPre: 빈 prerelease(정식)는 prerelease가 있는 쪽보다 높다.
// 두 prerelease끼리는 단순 문자열 비교 (rc.1 vs rc.2 등).
func cmpPre(a, b string) int {
	switch {
	case a == "" && b == "":
		return 0
	case a == "":
		return 1
	case b == "":
		return -1
	}
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// parseSemver는 "v?MAJOR.MINOR.PATCH(-PRERELEASE)?" 를 분해한다.
func parseSemver(s string) (major, minor, patch int, pre string, ok bool) {
	s = StripV(s)
	if s == "" {
		return 0, 0, 0, "", false
	}
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i] // build metadata 제거
	}
	core := s
	if i := strings.IndexByte(s, '-'); i >= 0 {
		core = s[:i]
		pre = s[i+1:]
	}
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return 0, 0, 0, "", false
	}
	var err error
	if major, err = strconv.Atoi(parts[0]); err != nil {
		return 0, 0, 0, "", false
	}
	if minor, err = strconv.Atoi(parts[1]); err != nil {
		return 0, 0, 0, "", false
	}
	if patch, err = strconv.Atoi(parts[2]); err != nil {
		return 0, 0, 0, "", false
	}
	return major, minor, patch, pre, true
}
