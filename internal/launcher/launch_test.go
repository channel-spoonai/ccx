package launcher

import (
	"os"
	"strings"
	"testing"

	"github.com/channel-spoonai/ccx/internal/config"
	"github.com/channel-spoonai/ccx/internal/ctxwin"
)

func TestResolveSecret(t *testing.T) {
	os.Setenv("CCX_TEST_KEY", "secret-value")
	defer os.Unsetenv("CCX_TEST_KEY")
	os.Unsetenv("CCX_TEST_MISSING")

	cases := []struct {
		in, want string
	}{
		{"plain-value", "plain-value"},
		{"env:CCX_TEST_KEY", "secret-value"},
		{"env:CCX_TEST_MISSING", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := ResolveSecret(c.in); got != c.want {
			t.Errorf("ResolveSecret(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestBuildEnvResolvesReferences(t *testing.T) {
	os.Setenv("CCX_TEST_KEY", "resolved-key")
	defer os.Unsetenv("CCX_TEST_KEY")

	p := &config.Profile{
		Name:      "t",
		BaseURL:   "https://example.com",
		APIKey:    "env:CCX_TEST_KEY",
		AuthToken: "plain-token",
	}
	env := BuildEnv(p)

	has := func(needle string) bool {
		for _, e := range env {
			if strings.HasPrefix(e, needle) {
				return true
			}
		}
		return false
	}
	if !has("ANTHROPIC_API_KEY=resolved-key") {
		t.Errorf("expected ANTHROPIC_API_KEY=resolved-key, got env %v", filterAnthropic(env))
	}
	if !has("ANTHROPIC_AUTH_TOKEN=plain-token") {
		t.Errorf("plain token should pass through")
	}
}

func TestUnresolvedEnvRefs(t *testing.T) {
	os.Setenv("CCX_TEST_PRESENT", "x")
	defer os.Unsetenv("CCX_TEST_PRESENT")
	os.Unsetenv("CCX_TEST_ABSENT")

	p := &config.Profile{
		APIKey:    "env:CCX_TEST_PRESENT",
		AuthToken: "env:CCX_TEST_ABSENT",
	}
	missing := unresolvedEnvRefs(p)
	if len(missing) != 1 || missing[0] != "CCX_TEST_ABSENT" {
		t.Errorf("want [CCX_TEST_ABSENT], got %v", missing)
	}
}

// Apply → BuildEnv 체인이 suffix 변환 결과를 실제 env로 반영하는지 통합 검증.
func TestApplyThenBuildEnv(t *testing.T) {
	for _, key := range []string{ctxwin.EnvKey, ctxwin.AutoEnv} {
		if old, ok := os.LookupEnv(key); ok {
			os.Unsetenv(key)
			defer os.Setenv(key, old)
		}
	}

	p := &config.Profile{
		Name: "t",
		Models: &config.Models{
			Opus:   "deepseek-v4-pro", // 카탈로그 1M → [1m] 부착
			Sonnet: "kimi-k2.5[262k]", // suffix → [1m] + ACW=262000
			Haiku:  "GLM-4.5-Air",     // 카탈로그 131072, ACW 후보에서 제외
		},
	}
	applied, res := ctxwin.Apply(p)
	env := BuildEnv(applied)

	has := func(needle string) bool {
		for _, e := range env {
			if strings.HasPrefix(e, needle) {
				return true
			}
		}
		return false
	}
	if !has("ANTHROPIC_DEFAULT_OPUS_MODEL=deepseek-v4-pro[1m]") {
		t.Errorf("opus not rewritten: %v", filterAnthropic(env))
	}
	if !has("ANTHROPIC_DEFAULT_SONNET_MODEL=kimi-k2.5[1m]") {
		t.Errorf("sonnet not rewritten: %v", filterAnthropic(env))
	}
	if !has("ANTHROPIC_DEFAULT_HAIKU_MODEL=GLM-4.5-Air") {
		t.Errorf("haiku changed unexpectedly: %v", filterAnthropic(env))
	}
	if !has(ctxwin.EnvKey + "=262000") {
		t.Errorf("auto-compact window not injected: res=%+v", res)
	}
}

func filterAnthropic(env []string) []string {
	var out []string
	for _, e := range env {
		if strings.HasPrefix(e, "ANTHROPIC_") {
			out = append(out, e)
		}
	}
	return out
}

func envValue(env []string, key string) string { return lookupEnv(env, key) }

func TestSessionHeaderInjected(t *testing.T) {
	os.Unsetenv(customHeadersEnv)
	p := &config.Profile{Name: "t", BaseURL: "http://localhost:8000", SessionHeader: "x-session-id"}

	got := envValue(BuildEnv(p), customHeadersEnv)
	if !strings.HasPrefix(got, "x-session-id: ccx-") {
		t.Fatalf("헤더가 주입되지 않았다: %q", got)
	}
	// 런치마다 값이 달라야 동시 세션이 서로의 프리픽스를 덮지 않는다.
	if again := envValue(BuildEnv(p), customHeadersEnv); again == got {
		t.Errorf("두 번의 BuildEnv가 같은 세션 id를 냈다: %q", got)
	}
}

func TestSessionHeaderDisabledByDefault(t *testing.T) {
	os.Unsetenv(customHeadersEnv)
	p := &config.Profile{Name: "t", BaseURL: "http://localhost:8000"}
	if got := envValue(BuildEnv(p), customHeadersEnv); got != "" {
		t.Errorf("SessionHeader가 비었는데 헤더가 생겼다: %q", got)
	}
}

func TestSessionHeaderMergesWithProfileEnv(t *testing.T) {
	os.Unsetenv(customHeadersEnv)
	p := &config.Profile{
		Name:          "t",
		SessionHeader: "x-session-id",
		Env:           map[string]string{customHeadersEnv: "x-tenant: acme"},
	}
	got := envValue(BuildEnv(p), customHeadersEnv)
	lines := strings.Split(got, "\n")
	if len(lines) != 2 || lines[0] != "x-tenant: acme" || !strings.HasPrefix(lines[1], "x-session-id: ccx-") {
		t.Errorf("기존 헤더를 보존하며 덧붙이지 않았다: %q", got)
	}
}

func TestSessionHeaderYieldsToUserValue(t *testing.T) {
	os.Unsetenv(customHeadersEnv)
	p := &config.Profile{
		Name:          "t",
		SessionHeader: "x-session-id",
		Env:           map[string]string{customHeadersEnv: "X-Session-Id: mine"},
	}
	if got := envValue(BuildEnv(p), customHeadersEnv); got != "X-Session-Id: mine" {
		t.Errorf("사용자 지정 헤더를 덮어썼다: %q", got)
	}
}
