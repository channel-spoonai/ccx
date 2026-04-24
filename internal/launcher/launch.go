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
	set := func(k, v string) {
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
