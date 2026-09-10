package launcher

import (
	"crypto/rand"
	"encoding/hex"
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
	// p.Env 루프 뒤에 병합한다 — 사용자가 profile.env로 직접 지정한
	// ANTHROPIC_CUSTOM_HEADERS를 덮지 않고 한 줄 덧붙이기 위해서다.
	if h := strings.TrimSpace(p.SessionHeader); h != "" {
		if merged, ok := mergeSessionHeader(lookupEnv(env, customHeadersEnv), h, NewSessionID()); ok {
			env = replaceOrAppend(env, customHeadersEnv, merged)
		}
	}
	return env
}

// customHeadersEnv는 Claude Code가 모든 요청에 실어 보내는 추가 헤더 목록.
// 개행으로 구분된 "Name: Value" 형식으로 파싱된다.
const customHeadersEnv = "ANTHROPIC_CUSTOM_HEADERS"

// NewSessionID는 런치마다 유니크한 세션 어피니티 값을 만든다.
// 값 자체에 의미는 없고 "같은 claude 프로세스의 요청끼리만 같으면" 된다 —
// 그래서 동시에 두 세션을 띄워도 서로의 프리픽스를 덮어쓰지 않는다.
func NewSessionID() string {
	var buf [6]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("ccx-%d", os.Getpid())
	}
	return "ccx-" + hex.EncodeToString(buf[:])
}

// mergeSessionHeader는 기존 ANTHROPIC_CUSTOM_HEADERS에 세션 헤더 한 줄을 덧붙인다.
// 같은 이름의 헤더가 이미 있으면 사용자 지정이 우선이므로 그대로 두고 ok=false.
func mergeSessionHeader(existing, name, value string) (string, bool) {
	for _, line := range strings.Split(existing, "\n") {
		key, _, found := strings.Cut(line, ":")
		if found && strings.EqualFold(strings.TrimSpace(key), name) {
			return "", false
		}
	}
	line := name + ": " + value
	if strings.TrimSpace(existing) == "" {
		return line, true
	}
	return strings.TrimRight(existing, "\n") + "\n" + line, true
}

// lookupEnv는 조립 중인 env 슬라이스에서 값을 읽는다 (os.Getenv는 p.Env 반영 전 값이라 못 쓴다).
func lookupEnv(env []string, key string) string {
	prefix := key + "="
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			return strings.TrimPrefix(e, prefix)
		}
	}
	return ""
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
		// ctxwin이 재작성한 "[1m]"은 Claude Code가 인식하는 유일한 표기라서 붙는 것이지
		// 그 모델이 1M을 처리한다는 뜻이 아니다. 그대로 보여주면 262K 모델이 1M으로 읽혀
		// 오해를 부르므로 여기서는 떼고, 실제 윈도우는 바로 아래 Context 줄이 말한다.
		if s := stripCtxSuffix(p.Models.Opus); s != "" {
			parts = append(parts, "opus→"+s)
		}
		if s := stripCtxSuffix(p.Models.Sonnet); s != "" {
			parts = append(parts, "sonnet→"+s)
		}
		if s := stripCtxSuffix(p.Models.Haiku); s != "" {
			parts = append(parts, "haiku→"+s)
		}
		if len(parts) > 0 {
			fmt.Printf("\x1B[36m[ccx]\x1B[0m Models: %s\n", strings.Join(parts, ", "))
		}
	}
	if h := strings.TrimSpace(p.SessionHeader); h != "" {
		fmt.Printf("\x1B[36m[ccx]\x1B[0m Session affinity: %s (per-launch id)\n", h)
	}
	printContextLine(res)
	fmt.Println()
}

// stripCtxSuffix는 배너 표시용으로 컨텍스트 표기를 떼어낸다.
func stripCtxSuffix(model string) string {
	if base, _, ok := ctxwin.ParseSuffix(model); ok {
		return base
	}
	return model
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
