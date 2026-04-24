package launcher

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/yobuce/claudex/internal/config"
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

// ResolveSecret expands an "env:VAR_NAME" reference into the actual env value
// so users can keep real keys out of claudex.config.json. Plain values pass
// through unchanged. Missing variables resolve to "" (same as empty key) —
// the caller logs a warning via WarnUnresolvedRefs.
func ResolveSecret(v string) string {
	const prefix = "env:"
	if strings.HasPrefix(v, prefix) {
		return os.Getenv(strings.TrimPrefix(v, prefix))
	}
	return v
}

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

// Launch replaces the current process with claude on Unix, or runs it as a
// child and propagates the exit code on Windows (exec is not available).
func Launch(p *config.Profile, args []string) error {
	env := BuildEnv(p)
	printBanner(p)

	binary, err := exec.LookPath(ClaudeCmd)
	if err != nil {
		return errClaudeNotFound
	}

	cmd := exec.Command(binary, args...)
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.ExitCode())
		}
		return err
	}
	return nil
}

var errClaudeNotFound = errors.New(`"claude"를 찾을 수 없습니다. Claude Code가 설치되어 있나요? https://docs.anthropic.com/en/docs/claude-code`)

func ErrClaudeNotFound() error { return errClaudeNotFound }

func printBanner(p *config.Profile) {
	fmt.Printf("\x1B[36m[claudex]\x1B[0m 프로파일: \x1B[1m%s\x1B[0m\n", p.Name)
	if missing := unresolvedEnvRefs(p); len(missing) > 0 {
		fmt.Printf("\x1B[33m[claudex]\x1B[0m ⚠ 환경변수 미설정: %s\n", strings.Join(missing, ", "))
	}
	if p.BaseURL != "" {
		fmt.Printf("\x1B[36m[claudex]\x1B[0m API: %s\n", p.BaseURL)
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
			fmt.Printf("\x1B[36m[claudex]\x1B[0m 모델: %s\n", strings.Join(parts, ", "))
		}
	}
	fmt.Println()
}
