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
		wantACW   string // "" = 주입 없음
	}{
		{"1M 이상은 [1m]만", "foo[1m]", "foo[1m]", ""},
		{"정확히 200K는 표기 제거만", "foo[200k]", "foo", ""},
		{"200K 미만은 제거 + ACW", "local[128k]", "local", "128000"},
		{"200K~1M은 [1m] + ACW", "gpt-x[272k]", "gpt-x[1m]", "272000"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := &config.Profile{Name: "t", Models: &config.Models{Sonnet: c.model}}
			out, res := Apply(p)
			if out.Models.Sonnet != c.wantModel {
				t.Errorf("sonnet = %q, want %q", out.Models.Sonnet, c.wantModel)
			}
			got := out.Env[EnvKey]
			if got != c.wantACW {
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
	if got := out.Env[EnvKey]; got != "272000" {
		t.Errorf("ACW = %q, want 272000 (ChatGPT backend cap)", got)
	}
	if out.Models.Opus != "gpt-5.6-sol[1m]" {
		t.Errorf("opus = %q, want [1m] kept", out.Models.Opus)
	}
	// haiku는 카탈로그(1.05M)→캡(272K)으로 200K<W<1M 구간이 되는데, haiku에는
	// ACW 하향을 짝지어 줄 수 없으므로 [1m]을 붙이지 않는다 (200K 추정이 안전)
	if out.Models.Haiku != "gpt-5.6-luna" {
		t.Errorf("haiku = %q, want gpt-5.6-luna (no [1m])", out.Models.Haiku)
	}
}

// haiku가 200K<W<1M인데 메인 티어가 전부 1M이라 ACW가 주입되지 않는 구성 —
// haiku에 [1m]만 붙이면 262K 모델을 1M으로 과대 선언하게 되므로 붙이면 안 된다.
func TestApplyHaikuMidWindowNotOverstated(t *testing.T) {
	hermetic(t)
	p := &config.Profile{Name: "t", Models: &config.Models{
		Opus:   "kimi-k3",
		Sonnet: "kimi-k3",
		Haiku:  "kimi-k2.5",
	}}
	out, res := Apply(p)
	if out.Models.Haiku != "kimi-k2.5" {
		t.Errorf("haiku = %q, want kimi-k2.5 (no [1m] without paired ACW)", out.Models.Haiku)
	}
	if _, ok := out.Env[EnvKey]; ok {
		t.Errorf("no ACW expected (mains are 1M): %v", out.Env)
	}
	if res.HaikuBelow != 262_144 {
		t.Errorf("HaikuBelow = %d, want 262144", res.HaikuBelow)
	}
}

// 사용자 ACW가 티어의 실제 윈도우보다 크면 [1m] 부착이 오히려 오버플로를
// 무장시킨다(200K 추정이었으면 무해했을 값) — 부착을 생략해야 한다.
func TestApplyUserACWLargerThanWindow(t *testing.T) {
	neutralize(t, AutoEnv)
	t.Setenv(EnvKey, "500000")
	p := &config.Profile{Name: "t", Models: &config.Models{Sonnet: "local[262k]"}}
	out, res := Apply(p)
	if out.Models.Sonnet != "local" {
		t.Errorf("sonnet = %q, want local (no [1m]: user ACW 500000 > window 262000)", out.Models.Sonnet)
	}
	if !res.UserSet {
		t.Errorf("res = %+v, want UserSet", res)
	}

	// 사용자 ACW가 윈도우 이하이면 [1m]+사용자 값 조합이 안전하므로 부착 유지
	t.Setenv(EnvKey, "250000")
	out2, _ := Apply(p)
	if out2.Models.Sonnet != "local[1m]" {
		t.Errorf("sonnet = %q, want local[1m] (user ACW 250000 <= window 262000)", out2.Models.Sonnet)
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
