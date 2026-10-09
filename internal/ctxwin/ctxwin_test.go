package ctxwin

import (
	"os"
	"testing"

	"github.com/channel-spoonai/ccx/internal/config"
)

// neutralize는 CI/개발자 머신에 해당 env가 설정돼 있어도 테스트가 hermetic하도록
// 잠시 제거하고 종료 시 복원한다.
func neutralize(t *testing.T, key string) {
	t.Helper()
	if old, ok := os.LookupEnv(key); ok {
		os.Unsetenv(key)
		t.Cleanup(func() { os.Setenv(key, old) })
	}
}

func hermetic(t *testing.T) {
	t.Helper()
	neutralize(t, EnvKey)
	neutralize(t, MaxTokensEnv)
	neutralize(t, AutoEnv)
}

func TestParseSuffix(t *testing.T) {
	cases := []struct {
		in     string
		base   string
		window int
		ok     bool
	}{
		{"gpt-5.6-sol[1m]", "gpt-5.6-sol", 1_000_000, true},
		{"gpt-5.4[1M]", "gpt-5.4", 1_000_000, true},
		{"local[200k]", "local", 200_000, true},
		{"kimi-k2.5[262K]", "kimi-k2.5", 262_000, true},
		{"GLM-4.7", "GLM-4.7", 0, false},
		{"model[x]", "model[x]", 0, false},
		{"model[k]", "model[k]", 0, false},
		{"model[0k]", "model[0k]", 0, false},
		{"model[-5k]", "model[-5k]", 0, false},
		{"[1m]", "[1m]", 0, false},
		{"a[1m]b", "a[1m]b", 0, false},
		{"provider/model:free", "provider/model:free", 0, false},
	}
	for _, c := range cases {
		base, w, ok := ParseSuffix(c.in)
		if base != c.base || w != c.window || ok != c.ok {
			t.Errorf("ParseSuffix(%q) = (%q, %d, %v), want (%q, %d, %v)",
				c.in, base, w, ok, c.base, c.window, c.ok)
		}
	}
}

func TestApplyDeliveryFormula(t *testing.T) {
	hermetic(t)
	cases := []struct {
		name      string
		model     string
		wantModel string
		wantMax   string // "" = 주입 없음
		wantACW   string // "" = 주입 없음
	}{
		{"1M 이상은 [1m]만", "foo[1m]", "foo[1m]", "", ""},
		{"정확히 200K는 표기 제거만", "foo[200k]", "foo", "", ""},
		{"200K 미만은 제거 + MAX + ACW", "local[128k]", "local", "128000", "128000"},
		// [1m]을 달면 statusline이 1M을 표시한다 — MAX만으로 윈도우를 전달해야 한다
		{"200K~1M은 제거 + MAX", "gpt-x[272k]", "gpt-x", "272000", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := &config.Profile{Name: "t", Models: &config.Models{Sonnet: c.model}}
			out, res := Apply(p)
			if out.Models.Sonnet != c.wantModel {
				t.Errorf("sonnet = %q, want %q", out.Models.Sonnet, c.wantModel)
			}
			if got := out.Env[MaxTokensEnv]; got != c.wantMax {
				t.Errorf("MAX = %q, want %q", got, c.wantMax)
			}
			if got := out.Env[EnvKey]; got != c.wantACW {
				t.Errorf("ACW = %q, want %q", got, c.wantACW)
			}
			if res == nil || len(res.Tiers) != 1 || res.Tiers[0].Source != "suffix" {
				t.Errorf("resolution = %+v, want 1 suffix tier", res)
			}
		})
	}
}

func TestApplyCatalogFallback(t *testing.T) {
	hermetic(t)
	p := &config.Profile{Name: "glm", Models: &config.Models{
		Opus:   "GLM-4.7",
		Sonnet: "GLM-4.7",
		Haiku:  "GLM-4.5-Air",
	}}
	out, res := Apply(p)
	// 200K 카탈로그 매칭은 재작성·주입 없음. haiku 131072는 세션(200K)의 절반
	// 이상이라 경고하지 않는다 (일반적인 소형-haiku 구성에 경고 피로 방지).
	if out.Models.Opus != "GLM-4.7" || out.Models.Haiku != "GLM-4.5-Air" {
		t.Errorf("models rewritten unexpectedly: %+v", out.Models)
	}
	if _, ok := out.Env[EnvKey]; ok {
		t.Errorf("ACW injected for 200K models: %v", out.Env)
	}
	if _, ok := out.Env[MaxTokensEnv]; ok {
		t.Errorf("MAX injected for 200K models: %v", out.Env)
	}
	if res == nil || len(res.Tiers) != 3 {
		t.Fatalf("want 3 catalog tiers, got %+v", res)
	}
	if res.HaikuBelow != 0 {
		t.Errorf("HaikuBelow = %d, want 0 (131072*2 > 200000)", res.HaikuBelow)
	}
}

func TestApplyCatalogLargeModel(t *testing.T) {
	hermetic(t)
	p := &config.Profile{Name: "ds", Models: &config.Models{Sonnet: "deepseek-v4-pro"}}
	out, _ := Apply(p)
	if out.Models.Sonnet != "deepseek-v4-pro[1m]" {
		t.Errorf("sonnet = %q, want deepseek-v4-pro[1m]", out.Models.Sonnet)
	}
	if _, ok := out.Env[EnvKey]; ok {
		t.Errorf("1M model should not need ACW: %v", out.Env)
	}
	if _, ok := out.Env[MaxTokensEnv]; ok {
		t.Errorf("1M model should not need MAX: %v", out.Env)
	}
}

func TestApplyMinRuleExcludesHaiku(t *testing.T) {
	hermetic(t)
	p := &config.Profile{Name: "t", Models: &config.Models{
		Opus:   "big[1m]",
		Sonnet: "mid[128k]",
		Haiku:  "tiny[64k]",
	}}
	out, res := Apply(p)
	if got := out.Env[EnvKey]; got != "128000" {
		t.Errorf("ACW = %q, want 128000 (haiku 64k must be excluded)", got)
	}
	if got := out.Env[MaxTokensEnv]; got != "128000" {
		t.Errorf("MAX = %q, want 128000 (haiku 64k must be excluded)", got)
	}
	if res.HaikuBelow != 64_000 {
		t.Errorf("HaikuBelow = %d, want 64000", res.HaikuBelow)
	}
}

func TestApplyCodexBackendCap(t *testing.T) {
	hermetic(t)
	p := &config.Profile{Name: "codex", Auth: "codex-oauth", Models: &config.Models{
		Opus:   "gpt-5.6-sol[1m]",
		Sonnet: "gpt-5.6-terra[1m]",
		Haiku:  "gpt-5.6-luna",
	}}
	out, _ := Apply(p)
	if got := out.Env[MaxTokensEnv]; got != "272000" {
		t.Errorf("MAX = %q, want 272000 (ChatGPT backend cap)", got)
	}
	if _, ok := out.Env[EnvKey]; ok {
		t.Errorf("ACW not needed above 200K: %v", out.Env)
	}
	// 캡이 걸려 1M 미만이 되면 [1m]을 떼야 한다 — 남기면 1M으로 과대 인식된다
	if out.Models.Opus != "gpt-5.6-sol" {
		t.Errorf("opus = %q, want gpt-5.6-sol ([1m] dropped under cap)", out.Models.Opus)
	}
	if out.Models.Haiku != "gpt-5.6-luna" {
		t.Errorf("haiku = %q, want gpt-5.6-luna (no [1m])", out.Models.Haiku)
	}
}

// haiku가 200K<W<1M인데 메인 티어가 전부 1M인 구성 — 메인은 [1m]이 MAX보다 우선하므로
// MAX를 읽는 커스텀 티어가 haiku뿐이라 haiku 윈도우를 싣는다. [1m]은 붙이면 안 된다.
func TestApplyHaikuMidWindowNotOverstated(t *testing.T) {
	hermetic(t)
	p := &config.Profile{Name: "t", Models: &config.Models{
		Opus:   "kimi-k3",
		Sonnet: "kimi-k3",
		Haiku:  "kimi-k2.5",
	}}
	out, res := Apply(p)
	if out.Models.Haiku != "kimi-k2.5" {
		t.Errorf("haiku = %q, want kimi-k2.5 (no [1m])", out.Models.Haiku)
	}
	if got := out.Env[MaxTokensEnv]; got != "262144" {
		t.Errorf("MAX = %q, want 262144 (haiku is the only custom tier reading it)", got)
	}
	if _, ok := out.Env[EnvKey]; ok {
		t.Errorf("no ACW expected: %v", out.Env)
	}
	if res.HaikuBelow != 262_144 {
		t.Errorf("HaikuBelow = %d, want 262144", res.HaikuBelow)
	}
}

// 메인 티어 중 하나라도 1M 미만이면 haiku가 MAX를 정하면 안 된다 — 메인 세션을 캡한다.
func TestApplyMaxIgnoresHaikuWhenMainCustom(t *testing.T) {
	hermetic(t)
	p := &config.Profile{Name: "t", Models: &config.Models{
		Opus:   "big[1m]",
		Sonnet: "local[200k]",
		Haiku:  "small[128k]",
	}}
	out, _ := Apply(p)
	if _, ok := out.Env[MaxTokensEnv]; ok {
		t.Errorf("MAX must stay unset (sonnet 200K is the default): %v", out.Env)
	}
}

func TestApplyUserMaxWins(t *testing.T) {
	hermetic(t)
	p := &config.Profile{Name: "t",
		Models: &config.Models{Sonnet: "local[262k]"},
		Env:    map[string]string{MaxTokensEnv: "250000"},
	}
	out, res := Apply(p)
	if got := out.Env[MaxTokensEnv]; got != "250000" {
		t.Errorf("MAX = %q, want user value 250000", got)
	}
	if !res.MaxUserSet || res.MaxContext != 0 {
		t.Errorf("res = %+v, want MaxUserSet=true MaxContext=0", res)
	}
	if out.Models.Sonnet != "local" {
		t.Errorf("sonnet = %q, want local", out.Models.Sonnet)
	}

	neutralize(t, MaxTokensEnv)
	t.Setenv(MaxTokensEnv, "100000")
	p2 := &config.Profile{Name: "t", Models: &config.Models{Sonnet: "local[262k]"}}
	out2, res2 := Apply(p2)
	if _, ok := out2.Env[MaxTokensEnv]; ok || !res2.MaxUserSet {
		t.Errorf("must not override ambient MAX: env=%v res=%+v", out2.Env, res2)
	}
}

// 사용자 ACW는 더 이상 [1m] 부착 여부를 좌우하지 않는다 — 200K~1M은 [1m]을 달지 않는다.
func TestApplyUserACWDoesNotAttach1M(t *testing.T) {
	neutralize(t, AutoEnv)
	neutralize(t, MaxTokensEnv)
	t.Setenv(EnvKey, "200000")
	p := &config.Profile{Name: "t", Models: &config.Models{Sonnet: "local[262k]"}}
	out, res := Apply(p)
	if out.Models.Sonnet != "local" {
		t.Errorf("sonnet = %q, want local", out.Models.Sonnet)
	}
	if got := out.Env[MaxTokensEnv]; got != "262000" {
		t.Errorf("MAX = %q, want 262000", got)
	}
	if !res.UserSet {
		t.Errorf("res = %+v, want UserSet", res)
	}
}

func TestApplyProfileEnvWins(t *testing.T) {
	hermetic(t)
	p := &config.Profile{Name: "t",
		Models: &config.Models{Sonnet: "local[128k]"},
		Env:    map[string]string{EnvKey: "99999"},
	}
	out, res := Apply(p)
	if got := out.Env[EnvKey]; got != "99999" {
		t.Errorf("ACW = %q, want user value 99999", got)
	}
	if !res.UserSet || res.AutoCompact != 0 {
		t.Errorf("res = %+v, want UserSet=true AutoCompact=0", res)
	}
	// 모델 ID 정규화는 사용자 ACW와 무관하게 수행 (비인식 suffix 유출 방지)
	if out.Models.Sonnet != "local" {
		t.Errorf("sonnet = %q, want local", out.Models.Sonnet)
	}
}

func TestApplyAmbientEnvWins(t *testing.T) {
	neutralize(t, AutoEnv)
	t.Setenv(EnvKey, "150000")
	p := &config.Profile{Name: "t", Models: &config.Models{Sonnet: "local[128k]"}}
	out, res := Apply(p)
	if _, ok := out.Env[EnvKey]; ok {
		t.Errorf("must not override ambient env: %v", out.Env)
	}
	if !res.UserSet {
		t.Errorf("res = %+v, want UserSet=true", res)
	}
}

func TestApplyKillSwitch(t *testing.T) {
	neutralize(t, EnvKey)
	t.Setenv(AutoEnv, "off")
	// suffix 박제된 프로파일이 kill-switch에서 그대로 통과하면 업스트림에
	// 리터럴 suffix가 유출되므로, 최소 동작으로 strip만은 수행해야 한다.
	p := &config.Profile{Name: "t", Models: &config.Models{
		Opus:   "gpt-5.6-sol[1m]",
		Sonnet: "local[128k]",
		Haiku:  "GLM-4.7",
	}}
	out, res := Apply(p)
	if res != nil {
		t.Errorf("kill switch must not resolve context: %+v", res)
	}
	if out.Models.Sonnet != "local" {
		t.Errorf("sonnet = %q, want local (ccx suffix stripped)", out.Models.Sonnet)
	}
	if out.Models.Opus != "gpt-5.6-sol[1m]" {
		t.Errorf("opus = %q, want [1m] preserved (official notation)", out.Models.Opus)
	}
	if out.Models.Haiku != "GLM-4.7" {
		t.Errorf("haiku = %q, want untouched (no catalog in kill-switch)", out.Models.Haiku)
	}
	if _, ok := out.Env[EnvKey]; ok {
		t.Errorf("kill switch must not inject ACW: %v", out.Env)
	}
	if _, ok := out.Env[MaxTokensEnv]; ok {
		t.Errorf("kill switch must not inject MAX: %v", out.Env)
	}
	if p.Models.Sonnet != "local[128k]" {
		t.Errorf("original mutated: %q", p.Models.Sonnet)
	}

	// strip할 게 없으면 입력 그대로 반환
	p2 := &config.Profile{Name: "t", Models: &config.Models{Sonnet: "GLM-4.7"}}
	out2, res2 := Apply(p2)
	if out2 != p2 || res2 != nil {
		t.Errorf("nothing to strip must return input unchanged")
	}
}

func TestApplyDoesNotMutateOriginal(t *testing.T) {
	hermetic(t)
	p := &config.Profile{Name: "t",
		Models: &config.Models{Sonnet: "local[128k]"},
		Env:    map[string]string{"OTHER": "1"},
	}
	out, _ := Apply(p)
	if p.Models.Sonnet != "local[128k]" {
		t.Errorf("original model mutated: %q", p.Models.Sonnet)
	}
	if _, ok := p.Env[EnvKey]; ok {
		t.Errorf("original env map mutated: %v", p.Env)
	}
	if out.Env["OTHER"] != "1" {
		t.Errorf("existing env lost: %v", out.Env)
	}
}

func TestApplyUnknownModelUntouched(t *testing.T) {
	hermetic(t)
	p := &config.Profile{Name: "t", Models: &config.Models{Sonnet: "mystery-model"}}
	out, res := Apply(p)
	if out != p || res != nil {
		t.Errorf("unknown model must return input unchanged, got %+v %+v", out, res)
	}
}

func TestApplyEnvReference(t *testing.T) {
	hermetic(t)
	t.Setenv("CCX_TEST_CTX_MODEL", "foo[128k]")
	p := &config.Profile{Name: "t", Model: "env:CCX_TEST_CTX_MODEL"}
	out, _ := Apply(p)
	if out.Model != "foo" {
		t.Errorf("model = %q, want materialized foo", out.Model)
	}
	if got := out.Env[EnvKey]; got != "128000" {
		t.Errorf("ACW = %q, want 128000", got)
	}

	// 미설정 참조는 원본 유지 (launcher의 unresolved 경고 보존)
	os.Unsetenv("CCX_TEST_CTX_ABSENT")
	p2 := &config.Profile{Name: "t", Model: "env:CCX_TEST_CTX_ABSENT"}
	out2, _ := Apply(p2)
	if out2.Model != "env:CCX_TEST_CTX_ABSENT" {
		t.Errorf("unset ref must stay raw, got %q", out2.Model)
	}
}

func guardHermetic(t *testing.T) {
	t.Helper()
	hermetic(t)
	neutralize(t, Disable1MEnv)
	neutralize(t, PinModelEnv)
}

func TestGuard1M(t *testing.T) {
	guardHermetic(t)

	tiers := func(opus, sonnet, haiku string) *config.Profile {
		return &config.Profile{Models: &config.Models{Opus: opus, Sonnet: sonnet, Haiku: haiku}}
	}
	none := ModelChoice{}
	cases := []struct {
		name   string
		p      *config.Profile
		choice ModelChoice
		want   Guard
	}{
		{"미상 ID (MTPLX)", tiers("p0ly31-qwen3.8-flash-next", "p0ly31-qwen3.8-flash-next", "p0ly31-qwen3.8-flash-next"), none, Guard{Disable1M: true}},
		{"200K 카탈로그", tiers("glm-4.7", "glm-4.7", "glm-4.5"), none, Guard{Disable1M: true}},
		{"200K 미만은 1M 끄기 (경고 조건 아님)", tiers("local[128k]", "local[128k]", "local[128k]"), none, Guard{Disable1M: true}},
		{"model 필드만", &config.Profile{Model: "local"}, none, Guard{Disable1M: true}},
		{"한 티어라도 [1m]", tiers("gpt-5.6-sol", "glm-4.7", "glm-4.7"), none, Guard{}},
		{"대문자 [1M] suffix", tiers("local[1M]", "local", "local"), none, Guard{}},
		{"모델 미설정", &config.Profile{}, none, Guard{}},
		// 200K 초과 윈도우에 1M 끄기를 쓰면 Claude Code가 매 시작마다 경고한다 — 시작 모델 고정
		{"262k, 고른 모델 없음", tiers("kimi-k2.5", "kimi-k2.5", "kimi-k2.5"), none, Guard{PinModel: "opus"}},
		{"262k, settings 모델 유지", tiers("local[262k]", "local[262k]", "local[262k]"), ModelChoice{Settings: "sonnet"}, Guard{PinModel: "sonnet"}},
		{"262k, settings가 [1m]이면 1M 끄기", tiers("local[262k]", "local[262k]", "local[262k]"), ModelChoice{Settings: "opus[1m]"}, Guard{Disable1M: true}},
		{"262k, CLI --model이 [1m] 없음", tiers("local[262k]", "local[262k]", "local[262k]"), ModelChoice{CLI: "sonnet", Settings: "opus[1m]"}, Guard{}},
		{"262k, CLI --model이 [1m]", tiers("local[262k]", "local[262k]", "local[262k]"), ModelChoice{CLI: "opus[1m]"}, Guard{Disable1M: true}},
		{"262k, profile.model 지정", &config.Profile{Model: "local[262k]"}, none, Guard{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			applied, _ := Apply(c.p)
			out, got := Guard1M(applied, c.choice)
			if got != c.want {
				t.Fatalf("Guard1M = %+v, want %+v", got, c.want)
			}
			if (out.Env[Disable1MEnv] == "1") != got.Disable1M {
				t.Fatalf("%s 주입 불일치: %v", Disable1MEnv, out.Env)
			}
			if out.Env[PinModelEnv] != got.PinModel {
				t.Fatalf("%s = %q, want %q", PinModelEnv, out.Env[PinModelEnv], got.PinModel)
			}
		})
	}
}

func TestGuard1MRespectsUser(t *testing.T) {
	guardHermetic(t)

	env := map[string]string{Disable1MEnv: "0"}
	p := &config.Profile{Model: "local", Env: env}
	if _, got := Guard1M(p, ModelChoice{}); got != (Guard{}) {
		t.Fatal("profile.env 명시값을 덮으면 안 됨")
	}

	// profile.env의 ANTHROPIC_MODEL은 이미 고른 시작 모델 — 고정하면 안 됨
	p2 := &config.Profile{Models: &config.Models{Opus: "local"},
		Env: map[string]string{MaxTokensEnv: "262144", PinModelEnv: "sonnet"}}
	if _, got := Guard1M(p2, ModelChoice{}); got != (Guard{}) {
		t.Fatalf("profile.env ANTHROPIC_MODEL을 덮으면 안 됨: %+v", got)
	}

	t.Setenv(Disable1MEnv, "0")
	if _, got := Guard1M(&config.Profile{Model: "local"}, ModelChoice{}); got != (Guard{}) {
		t.Fatal("ambient 명시값을 덮으면 안 됨")
	}
}

func TestGuard1MAmbientModel(t *testing.T) {
	guardHermetic(t)
	p := &config.Profile{Models: &config.Models{Opus: "local"}, Env: map[string]string{MaxTokensEnv: "262144"}}

	t.Setenv(PinModelEnv, "sonnet")
	if _, got := Guard1M(p, ModelChoice{}); got != (Guard{}) {
		t.Fatalf("ambient ANTHROPIC_MODEL([1m] 없음)이면 아무것도 안 해야 함: %+v", got)
	}
	t.Setenv(PinModelEnv, "opus[1m]")
	if _, got := Guard1M(p, ModelChoice{}); got != (Guard{Disable1M: true}) {
		t.Fatalf("ambient ANTHROPIC_MODEL이 [1m]이면 1M을 꺼야 함: %+v", got)
	}
}

func TestGuard1MDoesNotMutateInput(t *testing.T) {
	guardHermetic(t)

	env := map[string]string{"API_TIMEOUT_MS": "1"}
	p := &config.Profile{Model: "local", Env: env}
	out, got := Guard1M(p, ModelChoice{})
	if !got.Disable1M || out == p {
		t.Fatal("copy에 주입해야 함")
	}
	if _, leaked := env[Disable1MEnv]; leaked {
		t.Fatal("원본 Env 맵이 오염됨")
	}
}

func TestGuard1MKillSwitch(t *testing.T) {
	guardHermetic(t)
	t.Setenv(AutoEnv, "0")
	if _, got := Guard1M(&config.Profile{Model: "local"}, ModelChoice{}); got != (Guard{}) {
		t.Fatal("CCX_CONTEXT_AUTO=0이면 주입하면 안 됨")
	}
}

func TestGuardSettingsModel(t *testing.T) {
	guardHermetic(t)

	qwen := &config.Profile{Models: &config.Models{Opus: "qwen3.8-flash-next[128k]", Sonnet: "qwen3.8-flash-next[128k]", Haiku: "qwen3.8-flash-next[128k]"}}
	cases := []struct {
		name   string
		p      *config.Profile
		choice ModelChoice
		want   SettingsPin
	}{
		{"정식 opus ID → opus", qwen, ModelChoice{Settings: "claude-opus-4-8"}, SettingsPin{From: "claude-opus-4-8", To: "opus"}},
		{"[1m] 붙은 ID도 대상", qwen, ModelChoice{Settings: "claude-opus-4-8[1m]"}, SettingsPin{From: "claude-opus-4-8[1m]", To: "opus"}},
		{"sonnet 계열은 sonnet", qwen, ModelChoice{Settings: "claude-sonnet-5"}, SettingsPin{From: "claude-sonnet-5", To: "sonnet"}},
		{"haiku 계열은 haiku", qwen, ModelChoice{Settings: "claude-haiku-5-5"}, SettingsPin{From: "claude-haiku-5-5", To: "haiku"}},
		{"계열 미상은 opus", qwen, ModelChoice{Settings: "claude-fable-5-1"}, SettingsPin{From: "claude-fable-5-1", To: "opus"}},
		{"별칭은 그대로", qwen, ModelChoice{Settings: "opus"}, SettingsPin{}},
		{"settings 없음", qwen, ModelChoice{}, SettingsPin{}},
		{"CLI --model 우선", qwen, ModelChoice{CLI: "sonnet", Settings: "claude-opus-4-8"}, SettingsPin{}},
		{"프로파일이 그 ID를 서빙", &config.Profile{Models: &config.Models{Opus: "claude-opus-4-8"}}, ModelChoice{Settings: "claude-opus-4-8"}, SettingsPin{}},
		{"모델 미설정 (순정 Claude)", &config.Profile{}, ModelChoice{Settings: "claude-opus-4-8"}, SettingsPin{}},
		{"profile.model로 이미 고름", &config.Profile{Model: "local", Models: &config.Models{Opus: "local"}}, ModelChoice{Settings: "claude-opus-4-8"}, SettingsPin{}},
		{"profile.env ANTHROPIC_MODEL로 이미 고름", &config.Profile{Models: &config.Models{Opus: "local"}, Env: map[string]string{PinModelEnv: "opus"}}, ModelChoice{Settings: "claude-opus-4-8"}, SettingsPin{}},
	}
	for _, c := range cases {
		out, got := GuardSettingsModel(c.p, c.choice)
		if got != c.want {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
			continue
		}
		if got.To != "" && out.Env[PinModelEnv] != got.To {
			t.Errorf("%s: ANTHROPIC_MODEL=%q, want %q", c.name, out.Env[PinModelEnv], got.To)
		}
		if got.To != "" && c.p.Env[PinModelEnv] != "" {
			t.Errorf("%s: 입력 프로파일이 바뀌면 안 됨", c.name)
		}
	}
}

func TestGuardSettingsModelAmbient(t *testing.T) {
	guardHermetic(t)
	t.Setenv(PinModelEnv, "sonnet")
	p := &config.Profile{Models: &config.Models{Opus: "local"}}
	if _, got := GuardSettingsModel(p, ModelChoice{Settings: "claude-opus-4-8"}); got != (SettingsPin{}) {
		t.Fatalf("셸의 ANTHROPIC_MODEL을 덮으면 안 됨: %+v", got)
	}
}

// 고정값은 Guard1M이 "이미 고른 시작 모델"로 읽는다 — 200K 초과 윈도우에서 settings 원본을
// 다시 ANTHROPIC_MODEL에 고정해 되돌리면 안 된다.
func TestGuardSettingsModelThenGuard1M(t *testing.T) {
	guardHermetic(t)
	choice := ModelChoice{Settings: "claude-opus-4-8"}
	p := &config.Profile{Models: &config.Models{Opus: "local"}, Env: map[string]string{MaxTokensEnv: "262144"}}
	p, pin := GuardSettingsModel(p, choice)
	p, g := Guard1M(p, choice)
	if pin.To != "opus" || g != (Guard{}) || p.Env[PinModelEnv] != "opus" {
		t.Fatalf("pin=%+v guard=%+v ANTHROPIC_MODEL=%q", pin, g, p.Env[PinModelEnv])
	}

	// 200K 이하면 1M 끄기는 그대로 걸린다
	q := &config.Profile{Models: &config.Models{Opus: "local[128k]"}}
	q, _ = GuardSettingsModel(q, choice)
	if _, g := Guard1M(q, choice); !g.Disable1M {
		t.Fatalf("128k 프로파일은 1M 끄기가 유지돼야 함: %+v", g)
	}
}
