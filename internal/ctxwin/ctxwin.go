// Package ctxwin은 프로파일에 선언된 모델별 컨텍스트 윈도우를 해석해
// Claude Code가 올바른 윈도우를 인식하도록 모델 ID와 환경변수를 정규화한다.
//
// Claude Code는 모델 ID 패턴 하드코딩으로 윈도우를 추론하고 커스텀 ID는
// 200K로 가정한다. 오버라이드 수단은 셋이다(2.1.289 실측):
//   - "[1m]" suffix — 1M으로 인식되며 Claude Code가 API 전송 전 strip한다.
//     "[200k]" 같은 다른 suffix는 인식하지 않고 업스트림에 리터럴로 보낸다.
//     [1m]이 붙으면 아래 MAX_CONTEXT_TOKENS보다 우선한다.
//   - CLAUDE_CODE_MAX_CONTEXT_TOKENS — 비-Claude 모델 ID의 윈도우 자체를 바꾼다.
//     statusline의 context_window_size와 /context, 자동 압축 기준이 모두 이 값을
//     따른다. 전역 단일값이라 [1m]이 없는 커스텀 티어 전부에 적용된다.
//   - CLAUDE_CODE_AUTO_COMPACT_WINDOW — 자동 압축 기준만 바꾸며 모델 윈도우로
//     캡되므로 하향 조정만 가능하다. statusline 윈도우는 바꾸지 못한다.
//
// 실제 윈도우 W의 전달 공식:
//
//	W ≥ 1M          → "[1m]" 부착
//	200K < W < 1M   → 표기 제거 + MAX_CONTEXT_TOKENS=W
//	W == 200K       → 표기 제거만 (기본 추정과 일치)
//	W < 200K        → 표기 제거 + MAX_CONTEXT_TOKENS=W + AUTO_COMPACT_WINDOW=W
//
// 200K<W<1M 구간은 한때 "[1m]"+ACW 짝으로 전달했다. 자동 압축은 맞았지만 모델
// 윈도우가 1M으로 잡혀 statusline이 1M을 표시했다. W<200K의 ACW는
// MAX_CONTEXT_TOKENS를 모르는 구버전 Claude Code에서도 압축이 W에서 일어나게
// 하려고 남겨 둔다(200K 초과 구간은 구버전이면 200K 추정으로 떨어져 안전하다).
package ctxwin

import (
	"os"
	"strconv"
	"strings"

	"github.com/channel-spoonai/ccx/internal/config"
)

// EnvKey는 Claude Code가 자동 압축 기준 용량으로 읽는 환경변수.
const EnvKey = "CLAUDE_CODE_AUTO_COMPACT_WINDOW"

// MaxTokensEnv는 Claude Code가 비-Claude 모델 ID의 컨텍스트 윈도우로 읽는 환경변수.
const MaxTokensEnv = "CLAUDE_CODE_MAX_CONTEXT_TOKENS"

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
	MaxContext  int  // 계산해 주입한 MAX_CONTEXT_TOKENS (0 = 주입 없음)
	MaxUserSet  bool // 사용자가 MAX_CONTEXT_TOKENS를 이미 설정해 계산값을 양보
	AutoCompact int  // 계산해 주입한 ACW (0 = 주입 없음)
	UserSet     bool // 사용자가 profile.env/ambient로 ACW를 이미 설정해 계산값을 양보
	HaikuBelow  int  // haiku 윈도우가 세션 유효 윈도우보다 작을 때 그 값 (0 = 정상)
}

// Apply는 프로파일의 모델 ID들을 전달 공식대로 재작성하고 필요 시
// CLAUDE_CODE_MAX_CONTEXT_TOKENS / CLAUDE_CODE_AUTO_COMPACT_WINDOW를 Env에 주입한
// copy를 반환한다. 원본 프로파일은 수정하지 않는다. 컨텍스트 정보가 전혀 없으면
// 입력을 그대로 반환한다 (Resolution은 nil).
//
// 두 값 모두 프로세스 전역 단일값이므로 opus/sonnet/model 중 최솟값을 채택한다.
// haiku는 백그라운드 단발 호출용이라 제외한다 — 소형 haiku 하나가 메인 세션 전체를
// 캡하는 것을 막기 위함이며, haiku가 더 작으면 Resolution.HaikuBelow로 경고만
// 남긴다. 메인 티어가 전부 [1m]이라 MAX_CONTEXT_TOKENS가 비어 있을 때만 haiku
// 윈도우를 거기에 싣는다 — 그때는 그 값을 읽는 커스텀 티어가 haiku뿐이다.
//
// 사용자 명시값이 항상 이긴다: profile.env 또는 ambient 프로세스 env에 키가 있으면
// 그 키의 계산값을 주입하지 않는다 (둘 다 있으면 BuildEnv의 p.Env 루프가 마지막이라
// profile.env가 최종값).
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
	var acwNeeds, maxNeeds, mains []int
	haikuWindow := 0
	for _, t := range tiers {
		if t.label == "haiku" {
			haikuWindow = t.window
			continue
		}
		mains = append(mains, t.window)
		if t.window < 1_000_000 {
			maxNeeds = append(maxNeeds, t.window)
		}
		if t.window < defaultWindow {
			acwNeeds = append(acwNeeds, t.window)
		}
	}

	// 메인 티어 중 200K 정확히가 최솟값이면 기본 추정과 같아 주입할 필요가 없다.
	maxTokens := minOf(maxNeeds)
	if len(maxNeeds) == 0 && haikuWindow > 0 && haikuWindow < 1_000_000 {
		maxTokens = haikuWindow
	}
	if maxTokens == defaultWindow {
		maxTokens = 0
	}

	// 2차: 재작성. 1M 이상만 [1m]을 단다 — 나머지는 MAX_CONTEXT_TOKENS가 윈도우를 전달하고,
	// [1m]이 붙으면 그 값보다 우선해 1M으로 잡힌다.
	for _, t := range tiers {
		rewritten := t.base
		if t.window >= 1_000_000 {
			rewritten = t.base + "[1m]"
		}
		if rewritten != config.ResolveSecret(*t.slot) {
			*t.slot = rewritten
		}
		res.Tiers = append(res.Tiers, Tier{Label: t.label, Model: rewritten, Window: t.window, Source: t.source})
	}

	if userHas(p, MaxTokensEnv) {
		res.MaxUserSet = true
	} else if maxTokens > 0 {
		out.Env[MaxTokensEnv] = strconv.Itoa(maxTokens)
		res.MaxContext = maxTokens
	}

	userSet := userHas(p, EnvKey)
	res.UserSet = userSet
	if computed := minOf(acwNeeds); !userSet && computed > 0 {
		out.Env[EnvKey] = strconv.Itoa(computed)
		res.AutoCompact = computed
	}

	// haiku가 세션 유효 윈도우의 절반에도 못 미치면 백그라운드 호출이 잘릴 수
	// 있음을 경고. "haiku가 다소 작은" 구성은 일반 패턴(Anthropic 기본 구성도
	// 그렇다)이라 2배 이상 격차일 때만 경고해 상시 경고 피로를 피한다.
	// 사용자가 ACW를 직접 설정한 경우는 판단을 존중해 경고하지 않는다.
	if !userSet && haikuWindow > 0 {
		if eff := minOf(mains); eff > 0 && haikuWindow*2 <= eff {
			res.HaikuBelow = haikuWindow
		}
	}

	return &out, res
}

// Disable1MEnv는 Claude Code의 1M 컨텍스트 인식을 통째로 끄는 환경변수.
const Disable1MEnv = "CLAUDE_CODE_DISABLE_1M_CONTEXT"

// PinModelEnv는 Claude Code가 시작 모델로 읽는 환경변수.
const PinModelEnv = "ANTHROPIC_MODEL"

// pinAlias는 고를 모델이 없을 때 고정하는 값 — Claude Code 기본값 "opus[1m]"에서 [1m]만 뺀 것.
const pinAlias = "opus"

// Guard는 Guard1M이 기본 모델 opus[1m]의 1M 과대 인식을 막으려고 한 일.
type Guard struct {
	Disable1M bool   // CLAUDE_CODE_DISABLE_1M_CONTEXT=1 주입
	PinModel  string // ANTHROPIC_MODEL로 고정한 값 ("" = 고정 안 함)
}

// ModelChoice는 Claude Code가 시작 모델을 정하는 데 쓰는, ccx 프로파일 밖의 사용자 지정.
type ModelChoice struct {
	CLI      string // --model 인자
	Settings string // settings.json(user < project < local)의 model
}

// Guard1M은 어떤 티어도 1M을 선언하지 않은 프로파일에서 Claude Code의 기본 모델
// "opus[1m]"이 1M으로 잡히지 않게 막은 copy를 반환한다. Apply 이후에 호출해야 한다
// (최종 모델 ID의 "[1m]" 유무로 판정).
//
// Claude Code 2.1.283의 기본 모델은 "opus[1m]"이라, 모델을 고르지 않으면
// ANTHROPIC_DEFAULT_OPUS_MODEL 뒤에 "[1m]"을 스스로 붙여 1M으로 해석한다(실측:
// /model → "<id>[1m] (default)"). "[1m]"은 MAX_CONTEXT_TOKENS보다 우선하므로
// statusline의 context_window_size가 1M이 되고 자동 압축도 1M 기준이 된다.
//
// 막는 방법은 둘이다. 기본은 CLAUDE_CODE_DISABLE_1M_CONTEXT=1 — 어떤 경로로 [1m]이
// 붙어도 무시된다. 그런데 Claude Code는 이 키를 "200K 상한을 지키겠다"는 뜻으로 읽어,
// 모델 윈도우가 200K를 넘으면(MAX_CONTEXT_TOKENS로 262K를 선언한 경우 등) 시작할
// 때마다 "the 200K limit isn't enforced" 경고를 띄운다(2.1.289 실측). 그래서 윈도우가
// 200K를 넘을 때는 대신 시작 모델을 [1m] 없는 값으로 ANTHROPIC_MODEL에 고정한다.
// 사용자가 이미 [1m] 없는 모델을 골랐다면 아무것도 하지 않고, [1m]이 든 모델을
// 골랐다면 경고를 감수하고 1M을 끈다 — 실제 윈도우를 넘는 과대 인식보다 낫다.
// 세션 중 /model로 1M 옵션을 고르는 것까지는 고정으로 막지 못한다.
//
// 한 티어라도 "[1m]"을 달고 있으면 건드리지 않는다 — 그 티어는 정말 1M이다. 모델을
// 하나도 지정하지 않은 프로파일(순정 Claude 모델 경로)과 사용자가 키를 직접 둔
// 경우(profile.env/ambient)도 제외한다.
func Guard1M(p *config.Profile, choice ModelChoice) (*config.Profile, Guard) {
	if p == nil || disabled() || userHas(p, Disable1MEnv) {
		return p, Guard{}
	}
	var ids []string
	if p.Models != nil {
		ids = append(ids, p.Models.Opus, p.Models.Sonnet, p.Models.Haiku)
	}
	ids = append(ids, p.Model)
	any := false
	for _, raw := range ids {
		id := config.ResolveSecret(raw)
		if id == "" {
			continue
		}
		any = true
		if has1M(id) {
			return p, Guard{}
		}
	}
	if !any {
		return p, Guard{}
	}

	key, val := Disable1MEnv, "1"
	if userInt(p, MaxTokensEnv) > defaultWindow {
		pin, chosen := pinTarget(p, choice)
		switch {
		case chosen && !has1M(pin):
			return p, Guard{} // 사용자가 [1m] 없는 모델을 이미 골랐다
		case !chosen:
			key, val = PinModelEnv, pin
		}
	}

	out := *p
	out.Env = make(map[string]string, len(p.Env)+1)
	for k, v := range p.Env {
		out.Env[k] = v
	}
	out.Env[key] = val
	if key == PinModelEnv {
		return &out, Guard{PinModel: val}
	}
	return &out, Guard{Disable1M: true}
}

// pinTarget은 Claude Code가 시작 모델로 쓸 값을 우선순위(--model > ANTHROPIC_MODEL >
// settings)대로 찾는다. chosen=true면 그 값이 이미 시작 모델로 확정돼 고정할 수 없다는
// 뜻이고, false면 settings 값(없으면 pinAlias)을 ANTHROPIC_MODEL로 고정할 후보로 돌려준다.
// settings 값도 같은 값으로 고정하는 이유: Claude Code가 settings의 "opus"를
// "opus[1m]"으로 마이그레이션하는 경로가 있어, env로 붙잡아 두는 편이 안전하다.
func pinTarget(p *config.Profile, choice ModelChoice) (string, bool) {
	if m := strings.TrimSpace(choice.CLI); m != "" {
		return m, true
	}
	if v, ok := p.Env[PinModelEnv]; ok {
		return config.ResolveSecret(v), true
	}
	if m := config.ResolveSecret(p.Model); m != "" {
		return m, true
	}
	if m, ok := os.LookupEnv(PinModelEnv); ok && strings.TrimSpace(m) != "" {
		return m, true
	}
	if m := strings.TrimSpace(choice.Settings); m != "" {
		if has1M(m) {
			return m, true // [1m]을 직접 고른 설정은 바꾸지 않는다 — 1M 끄기로 폴백
		}
		return m, false
	}
	return pinAlias, false
}

// SettingsPin은 GuardSettingsModel이 settings.json의 Claude 모델 ID를 별칭으로 바꾼 내역.
type SettingsPin struct {
	From string // settings.json의 model (예: "claude-opus-4-8")
	To   string // ANTHROPIC_MODEL로 고정한 별칭 ("" = 고정 안 함)
}

// GuardSettingsModel은 settings.json의 model이 정식 Claude 모델 ID일 때 같은 계열 별칭을
// ANTHROPIC_MODEL로 고정한 copy를 반환한다. Guard1M보다 먼저 호출한다 — 고정값을 Guard1M이
// "사용자가 고른 시작 모델"로 읽어야 한다.
//
// Claude Code는 settings의 별칭(opus/sonnet/haiku)만 ANTHROPIC_DEFAULT_*_MODEL로 해석하고,
// "claude-opus-4-8" 같은 정식 ID는 그대로 업스트림에 보낸다. 커스텀 모델 프로파일의 서버는
// 그 ID를 모르므로 "model may not exist"로 막힌다(2026-10 vast.ai 실측, --model opus면 통과).
// 사용자가 시작 모델을 따로 골랐거나(--model / ANTHROPIC_MODEL / profile.model), 그 ID를
// 프로파일 티어로 직접 쓰는 경우는 건드리지 않는다.
func GuardSettingsModel(p *config.Profile, choice ModelChoice) (*config.Profile, SettingsPin) {
	if p == nil || p.Models == nil {
		return p, SettingsPin{}
	}
	settings := strings.TrimSpace(choice.Settings)
	base := strings.ToLower(settingsBase(settings))
	if !strings.HasPrefix(base, "claude-") {
		return p, SettingsPin{}
	}
	if strings.TrimSpace(choice.CLI) != "" || userHas(p, PinModelEnv) || config.ResolveSecret(p.Model) != "" {
		return p, SettingsPin{}
	}
	any := false
	for _, raw := range []string{p.Models.Opus, p.Models.Sonnet, p.Models.Haiku} {
		id := config.ResolveSecret(raw)
		if id == "" {
			continue
		}
		any = true
		if strings.ToLower(settingsBase(id)) == base {
			return p, SettingsPin{} // 프로파일이 그 ID를 직접 서빙한다
		}
	}
	if !any {
		return p, SettingsPin{} // 순정 Claude 모델 경로
	}

	alias := pinAlias
	for _, family := range []string{"sonnet", "haiku"} {
		if strings.Contains(base, family) {
			alias = family
		}
	}
	out := *p
	out.Env = make(map[string]string, len(p.Env)+1)
	for k, v := range p.Env {
		out.Env[k] = v
	}
	out.Env[PinModelEnv] = alias
	return &out, SettingsPin{From: settings, To: alias}
}

// settingsBase는 "[1m]"·"[262k]" 같은 대괄호 표기를 뗀 모델 ID.
func settingsBase(id string) string {
	if i := strings.LastIndex(id, "["); i > 0 && strings.HasSuffix(id, "]") {
		return id[:i]
	}
	return id
}

func has1M(id string) bool {
	return strings.Contains(strings.ToLower(id), "[1m]")
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

// userHas는 사용자가 key를 profile.env나 ambient env로 직접 지정했는지 본다.
func userHas(p *config.Profile, key string) bool {
	if _, ok := p.Env[key]; ok {
		return true
	}
	_, ok := os.LookupEnv(key)
	return ok
}

// userInt는 userHas와 같은 우선순위(profile.env > ambient)로 정수값을 읽는다 (없거나 파싱 실패 = 0).
func userInt(p *config.Profile, key string) int {
	v, ok := p.Env[key]
	if ok {
		v = config.ResolveSecret(v)
	} else {
		v = os.Getenv(key)
	}
	n, _ := strconv.Atoi(strings.TrimSpace(v))
	return n
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
