package launcher

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/channel-spoonai/ccx/internal/config"
	"github.com/channel-spoonai/ccx/internal/ctxwin"
)

const ClaudeCmd = "claude"

func BuildEnv(p *config.Profile) []string {
	env := os.Environ()
	set := func(k, raw string) {
		v := ResolveSecret(raw)
		if v == "" {
			return
		}
		env = replaceOrAppend(env, k, v)
	}

	set("ANTHROPIC_BASE_URL", p.BaseURL)
	set("ANTHROPIC_API_KEY", p.APIKey)
	set("ANTHROPIC_AUTH_TOKEN", p.AuthToken)
	if p.Models != nil {
		set("ANTHROPIC_DEFAULT_OPUS_MODEL", p.Models.Opus)
		set("ANTHROPIC_DEFAULT_SONNET_MODEL", p.Models.Sonnet)
		set("ANTHROPIC_DEFAULT_HAIKU_MODEL", p.Models.Haiku)
	}
	set("ANTHROPIC_MODEL", p.Model)
	for k, v := range p.Env {
		set(k, v)
	}
	return env
}

// ResolveSecret은 config.ResolveSecret의 얇은 별칭 — 구현이 config 패키지로
// 이동했다 (ctxwin도 써야 하는데 launcher를 import하면 순환이라서).
func ResolveSecret(v string) string { return config.ResolveSecret(v) }

// unresolvedEnvRefs returns env-var names referenced by the profile but not
// set in the current process environment. Used to warn the user before claude
// exits with 401.
func unresolvedEnvRefs(p *config.Profile) []string {
	var missing []string
	check := func(v string) {
		const prefix = "env:"
		if !strings.HasPrefix(v, prefix) {
			return
		}
		name := strings.TrimPrefix(v, prefix)
		if _, ok := os.LookupEnv(name); !ok {
			missing = append(missing, name)
		}
	}
	check(p.APIKey)
	check(p.AuthToken)
	check(p.BaseURL)
	check(p.Model)
	if p.Models != nil {
		check(p.Models.Opus)
		check(p.Models.Sonnet)
		check(p.Models.Haiku)
	}
	for _, v := range p.Env {
		check(v)
	}
	return missing
}

func replaceOrAppend(env []string, key, value string) []string {
	prefix := key + "="
	for i, e := range env {
		if strings.HasPrefix(e, prefix) {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}

var errClaudeNotFound = errors.New(`"claude" not found. Is Claude Code installed? https://docs.anthropic.com/en/docs/claude-code`)

func ErrClaudeNotFound() error { return errClaudeNotFound }

func printBanner(p *config.Profile, res *ctxwin.Resolution) {
	fmt.Printf("\x1B[36m[ccx]\x1B[0m Profile: \x1B[1m%s\x1B[0m\n", p.Name)
	if missing := unresolvedEnvRefs(p); len(missing) > 0 {
		fmt.Printf("\x1B[33m[ccx]\x1B[0m ⚠ unset environment variables: %s\n", strings.Join(missing, ", "))
	}
	if p.BaseURL != "" {
		fmt.Printf("\x1B[36m[ccx]\x1B[0m API: %s\n", p.BaseURL)
	}
	if p.Models != nil {
		var parts []string
		if p.Models.Opus != "" {
			parts = append(parts, "opus→"+p.Models.Opus)
		}
		if p.Models.Sonnet != "" {
			parts = append(parts, "sonnet→"+p.Models.Sonnet)
		}
		if p.Models.Haiku != "" {
			parts = append(parts, "haiku→"+p.Models.Haiku)
		}
		if len(parts) > 0 {
			fmt.Printf("\x1B[36m[ccx]\x1B[0m Models: %s\n", strings.Join(parts, ", "))
		}
	}
	printContextLine(res)
	fmt.Println()
}

// printContextLine은 ctxwin이 해석한 컨텍스트 윈도우와 주입 결과를 한 줄로 보여준다.
func printContextLine(res *ctxwin.Resolution) {
	if res == nil || len(res.Tiers) == 0 {
		return
	}
	var parts []string
	for _, t := range res.Tiers {
		parts = append(parts, fmt.Sprintf("%s %s (%s)", t.Label, formatWindow(t.Window), t.Source))
	}
	line := strings.Join(parts, ", ")
	if res.AutoCompact > 0 {
		line += fmt.Sprintf(" → auto-compact %d", res.AutoCompact)
	} else if res.UserSet {
		line += " → auto-compact set by user"
	}
	fmt.Printf("\x1B[36m[ccx]\x1B[0m Context: %s\n", line)
	if res.HaikuBelow > 0 {
		fmt.Printf("\x1B[33m[ccx]\x1B[0m ⚠ haiku window %s is below the session window — background calls may truncate\n", formatWindow(res.HaikuBelow))
	}
}

// formatWindow는 토큰 수를 배너용 짧은 표기로 바꾼다 (262144 → "262k", 1050000 → "1.05m").
func formatWindow(w int) string {
	switch {
	case w >= 1_000_000:
		s := strings.TrimRight(fmt.Sprintf("%.2f", float64(w)/1_000_000), "0")
		return strings.TrimSuffix(s, ".") + "m"
	case w >= 1_000:
		return fmt.Sprintf("%dk", w/1_000)
	default:
		return fmt.Sprintf("%d", w)
	}
}
