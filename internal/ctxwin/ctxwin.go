// Package ctxwin은 프로파일에 선언된 모델별 컨텍스트 윈도우를 해석해
// Claude Code가 올바른 윈도우를 인식하도록 모델 ID와 환경변수를 정규화한다.
//
// Claude Code는 모델 ID 패턴 하드코딩으로 윈도우를 추론하고 커스텀 ID는
// 200K로 가정한다. 공식 오버라이드 수단은 두 가지뿐이다(실측 검증 완료):
//   - "[1m]" suffix — 1M으로 인식되며 Claude Code가 API 전송 전 strip한다.
//     "[200k]" 같은 다른 suffix는 인식하지 않고 업스트림에 리터럴로 보낸다.
//   - CLAUDE_CODE_AUTO_COMPACT_WINDOW — 인식 용량을 설정하되 모델 추론
//     윈도우로 캡되므로 하향 조정만 가능하다.
//
// 실제 윈도우 W의 전달 공식:
//
//	W ≥ 1M          → "[1m]" 부착
//	200K < W < 1M   → "[1m]" 부착 + AUTO_COMPACT_WINDOW=W (하향 캡)
//	W == 200K       → 표기 제거만 (기본 추정과 일치)
//	W < 200K        → 표기 제거 + AUTO_COMPACT_WINDOW=W
//
// 단 200K<W<1M 구간의 "[1m]"+ACW는 분리 불가능한 짝이다 — [1m]만 붙고
// ACW 하향이 따라오지 못하면 Claude Code가 1M으로 과대 인식해 W 초과
// 요청이 업스트림에서 하드 실패한다(모든 부착 지점에서 이 짝을 보장할 것).
package ctxwin

import (
	"os"
	"strconv"
	"strings"

	"github.com/channel-spoonai/ccx/internal/config"
)

// EnvKey는 Claude Code가 인식 컨텍스트 용량으로 읽는 환경변수.
const EnvKey = "CLAUDE_CODE_AUTO_COMPACT_WINDOW"

// AutoEnv — "0"/"false"/"off"(대소문자 무시)면 컨텍스트 자동 해석 비활성.
// 이때도 ccx 전용 suffix 표기의 업스트림 유출만은 막는다 (stripOnly 참조).
const AutoEnv = "CCX_CONTEXT_AUTO"

// authCodexOAuth는 launcher.AuthCodexOAuth와 같은 리터럴. launcher가 ctxwin을
// import하므로 여기서 역참조하면 순환이라 재선언한다 (config 스키마 값이라 동결).
const authCodexOAuth = "codex-oauth"

// chatgptBackendCap — ChatGPT 구독 백엔드의 실측 컨텍스트 한도. 모델 스펙이
// 1M이어도 백엔드가 272K로 캡하므로(openai/codex#32806) 어떤 소스의 값이든
// codex-oauth 프로파일에서는 이 이상을 선언하지 않는다.
const chatgptBackendCap = 272_000

// defaultWindow는 Claude Code가 커스텀 모델 ID에 가정하는 윈도우.
const defaultWindow = 200_000

// Tier는 배너 표시용 해석 결과 한 줄.
type Tier struct {
	Label  string // "opus" | "sonnet" | "haiku" | "model"
	Model  string // 재작성 후 모델 ID
	Window int    // 해석된 실제 윈도우
	Source string // "suffix" | "catalog"
}

// Resolution은 Apply가 무엇을 왜 했는지 배너에 알려주기 위한 요약.
type Resolution struct {
	Tiers       []Tier
	AutoCompact int  // 계산해 주입한 값 (0 = 주입 없음)
	UserSet     bool // 사용자가 profile.env/ambient로 이미 설정해 계산값을 양보
	HaikuBelow  int  // haiku 윈도우가 세션 유효 윈도우보다 작을 때 그 값 (0 = 정상)
}

// Apply는 프로파일의 모델 ID들을 전달 공식대로 재작성하고 필요 시
// CLAUDE_CODE_AUTO_COMPACT_WINDOW를 Env에 주입한 copy를 반환한다.
// 원본 프로파일은 수정하지 않는다. 컨텍스트 정보가 전혀 없으면 입력을
// 그대로 반환한다 (Resolution은 nil).
//
// AUTO_COMPACT_WINDOW는 프로세스 전역 단일값이므로 opus/sonnet/model 중
// 최소 필요값을 채택한다. haiku는 백그라운드 단발 호출용이라 제외한다 —
// 소형 haiku 하나가 메인 세션 전체를 캡하는 것을 막기 위함이며, haiku가
// 더 작으면 Resolution.HaikuBelow로 경고만 남긴다. 같은 이유로 haiku는
// ACW 하향을 짝지어 줄 수 없어 W≥1M일 때만 "[1m]"을 받는다 (패키지 주석의
// 짝 불변식 — 200K<W<1M haiku에 [1m]을 붙이면 1M 과대 인식이 된다).
//
// 사용자 명시값이 항상 이긴다: profile.env 또는 ambient 프로세스 env에
// EnvKey가 있으면 계산값을 주입하지 않는다 (둘 다 있으면 BuildEnv의 p.Env
// 루프가 마지막이라 profile.env가 최종값). 이때 사용자 값이 어떤 티어의
// 실제 윈도우보다 크면 그 티어의 [1m] 부착도 생략한다 — 200K 추정이 안전.
func Apply(p *config.Profile) (*config.Profile, *Resolution) {
	if p == nil {
		return p, nil
	}
	if disabled() {
		return stripOnly(p)
	}

	out := *p
	if p.Models != nil {
		m := *p.Models
		out.Models = &m
	}
	// Env는 주입 가능성이 있어 deep-copy — shallow면 원본 config의 맵이 오염된다.
	out.Env = make(map[string]string, len(p.Env)+1)
	for k, v := range p.Env {
		out.Env[k] = v
	}

	// 1차: 티어별 윈도우 해석. 재작성은 최종 ACW가 확정된 뒤에만 한다.
	type tierState struct {
		label  string
		slot   *string
		base   string
		window int
		source string
	}
	var tiers []tierState
	collect := func(label string, slot *string) {
		raw := *slot
		if raw == "" {
			return
		}
		// env:VAR 참조는 해석 후 파싱. 미설정(빈 값)이면 원본을 남겨
		// launcher의 unresolvedEnvRefs 경고가 그대로 동작하게 둔다.
		id := config.ResolveSecret(raw)
		if id == "" {
			return
		}
		base, w, ok := ParseSuffix(id)
		source := "suffix"
		if !ok {
			if strings.Contains(id, "[") {
				return // 해석 불가능한 bracket 표기 — 손대지 않음 (이중 suffix 방지)
			}
			base = id
			w, ok = CatalogLookup(id)
			source = "catalog"
			if !ok {
				return // 미상 — Claude Code 기본 추정에 맡김
			}
		}
		if out.Auth == authCodexOAuth && w > chatgptBackendCap {
			w = chatgptBackendCap
		}
		tiers = append(tiers, tierState{label, slot, base, w, source})
	}
	if out.Models != nil {
		collect("opus", &out.Models.Opus)
		collect("sonnet", &out.Models.Sonnet)
		collect("haiku", &out.Models.Haiku)
	}
	collect("model", &out.Model)

	if len(tiers) == 0 {
		return p, nil
	}

	res := &Resolution{}
	var needs, mains []int
	haikuWindow := 0
	for _, t := range tiers {
		if t.label == "haiku" {
			haikuWindow = t.window
			continue
		}
		mains = append(mains, t.window)
		if n := acwNeed(t.window); n > 0 {
			needs = append(needs, n)
		}
	}

	// 사용자 명시값 탐지. profile.env가 ambient보다 우선 (BuildEnv 적용 순서와 일치).
	userSet, userVal := false, 0
	if v, ok := p.Env[EnvKey]; ok {
		userSet = true
		userVal, _ = strconv.Atoi(config.ResolveSecret(v))
	} else if v, ok := os.LookupEnv(EnvKey); ok {
		userSet = true
		userVal, _ = strconv.Atoi(v)
	}
	res.UserSet = userSet

	computed := minOf(needs)
	effective := computed
	if userSet {
		effective = userVal // 파싱 실패 시 0 — 아래 [1m] 가드가 보수적으로 동작
	}

	// 2차: 재작성. [1m]은 "최종 ACW ≤ 티어 실제 윈도우"가 보장될 때만 부착.
	// 계산값 경로는 computed = min(needs) ≤ 각 티어 W라 항상 성립한다.
	for _, t := range tiers {
		rewritten := t.base
		switch {
		case t.window >= 1_000_000:
			rewritten = t.base + "[1m]"
		case t.window > defaultWindow:
			if t.label != "haiku" && effective > 0 && effective <= t.window {
				rewritten = t.base + "[1m]"
			}
		}
		if rewritten != config.ResolveSecret(*t.slot) {
			*t.slot = rewritten
		}
		res.Tiers = append(res.Tiers, Tier{Label: t.label, Model: rewritten, Window: t.window, Source: t.source})
	}

	if !userSet && computed > 0 {
		out.Env[EnvKey] = strconv.Itoa(computed)
		res.AutoCompact = computed
	}

	// haiku가 세션 유효 윈도우의 절반에도 못 미치면 백그라운드 호출이 잘릴 수
	// 있음을 경고. "haiku가 다소 작은" 구성은 일반 패턴(Anthropic 기본 구성도
	// 그렇다)이라 2배 이상 격차일 때만 경고해 상시 경고 피로를 피한다.
	// 사용자가 ACW를 직접 설정한 경우는 판단을 존중해 경고하지 않는다.
	if !userSet && haikuWindow > 0 {
		eff := res.AutoCompact
		if eff == 0 {
			eff = minOf(mains)
		}
		if eff > 0 && haikuWindow*2 <= eff {
			res.HaikuBelow = haikuWindow
		}
	}

	return &out, res
}

// stripOnly는 kill-switch 상태의 최소 동작 — [1m] 부착·ACW 주입·카탈로그를
// 전부 생략하되, Claude Code가 인식하지 못하는 ccx 전용 suffix가 업스트림에
// 리터럴로 새는 것만은 막는다. 공식 표기인 "[1m]"은 보존한다.
func stripOnly(p *config.Profile) (*config.Profile, *Resolution) {
	out := *p
	if p.Models != nil {
		m := *p.Models
		out.Models = &m
	}
	changed := false
	strip := func(slot *string) {
		id := config.ResolveSecret(*slot)
		if id == "" {
			return
		}
		base, _, ok := ParseSuffix(id)
		if !ok || strings.ToLower(id[len(base):]) == "[1m]" {
			return
		}
		*slot = base
		changed = true
	}
	if out.Models != nil {
		strip(&out.Models.Opus)
		strip(&out.Models.Sonnet)
		strip(&out.Models.Haiku)
	}
	strip(&out.Model)
	if !changed {
		return p, nil
	}
	return &out, nil
}

// acwNeed는 윈도우 W를 전달하는 데 필요한 AUTO_COMPACT_WINDOW 값 (0 = 불필요).
func acwNeed(w int) int {
	if w == defaultWindow || w >= 1_000_000 {
		return 0
	}
	return w
}

// ParseSuffix는 "GLM-4.7[200k]" → ("GLM-4.7", 200000, true)로 해석한다.
// 정수+k/m(대소문자 무관)만 인식한다 — 프록시의 stripContextSuffix가 제거하는
// 표기와 같은 계열. 미인식 시 (원본, 0, false).
func ParseSuffix(model string) (base string, window int, ok bool) {
	i := strings.LastIndex(model, "[")
	if i <= 0 || !strings.HasSuffix(model, "]") {
		return model, 0, false
	}
	inner := strings.ToLower(model[i+1 : len(model)-1])
	if len(inner) < 2 {
		return model, 0, false
	}
	mult := 0
	switch inner[len(inner)-1] {
	case 'k':
		mult = 1_000
	case 'm':
		mult = 1_000_000
	default:
		return model, 0, false
	}
	n, err := strconv.Atoi(inner[:len(inner)-1])
	if err != nil || n <= 0 {
		return model, 0, false
	}
	return model[:i], n * mult, true
}

func minOf(vals []int) int {
	min := 0
	for _, v := range vals {
		if v > 0 && (min == 0 || v < min) {
			min = v
		}
	}
	return min
}

func disabled() bool {
	switch strings.ToLower(os.Getenv(AutoEnv)) {
	case "0", "false", "off":
		return true
	}
	return false
}
