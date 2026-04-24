package launcher

import (
	"os"
	"strings"
	"testing"

	"github.com/yobuce/claudex/internal/config"
)

func TestResolveSecret(t *testing.T) {
	os.Setenv("CLAUDEX_TEST_KEY", "secret-value")
	defer os.Unsetenv("CLAUDEX_TEST_KEY")
	os.Unsetenv("CLAUDEX_TEST_MISSING")

	cases := []struct {
		in, want string
	}{
		{"plain-value", "plain-value"},
		{"env:CLAUDEX_TEST_KEY", "secret-value"},
		{"env:CLAUDEX_TEST_MISSING", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := ResolveSecret(c.in); got != c.want {
			t.Errorf("ResolveSecret(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestBuildEnvResolvesReferences(t *testing.T) {
	os.Setenv("CLAUDEX_TEST_KEY", "resolved-key")
	defer os.Unsetenv("CLAUDEX_TEST_KEY")

	p := &config.Profile{
		Name:      "t",
		BaseURL:   "https://example.com",
		APIKey:    "env:CLAUDEX_TEST_KEY",
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
	os.Setenv("CLAUDEX_TEST_PRESENT", "x")
	defer os.Unsetenv("CLAUDEX_TEST_PRESENT")
	os.Unsetenv("CLAUDEX_TEST_ABSENT")

	p := &config.Profile{
		APIKey:    "env:CLAUDEX_TEST_PRESENT",
		AuthToken: "env:CLAUDEX_TEST_ABSENT",
	}
	missing := unresolvedEnvRefs(p)
	if len(missing) != 1 || missing[0] != "CLAUDEX_TEST_ABSENT" {
		t.Errorf("want [CLAUDEX_TEST_ABSENT], got %v", missing)
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
