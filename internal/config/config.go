package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	Filename        = "claudex.config.json"
	ExampleFilename = "claudex.config.example.json"
	EnvOverride     = "CLAUDEX_CONFIG"
)

type Models struct {
	Opus   string `json:"opus,omitempty"`
	Sonnet string `json:"sonnet,omitempty"`
	Haiku  string `json:"haiku,omitempty"`
}

type Profile struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	BaseURL     string            `json:"baseUrl,omitempty"`
	APIKey      string            `json:"apiKey,omitempty"`
	AuthToken   string            `json:"authToken,omitempty"`
	Model       string            `json:"model,omitempty"`
	Models      *Models           `json:"models,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
}

type Config struct {
	Profiles []Profile `json:"profiles"`
}

type Loaded struct {
	Config  Config
	Path    string
	Missing bool
}

func ExecDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		resolved = exe
	}
	return filepath.Dir(resolved)
}

// DefaultPath resolves the config file path by checking, in order:
//   1. $CLAUDEX_CONFIG
//   2. $XDG_CONFIG_HOME/claudex/claudex.config.json (or ~/.config/claudex/...)
//   3. ./claudex.config.json (current working directory)
//   4. <exec dir>/claudex.config.json
// The first path that exists wins. If none exist, returns the XDG path so
// save operations have a canonical target.
func DefaultPath() string {
	if v := os.Getenv(EnvOverride); v != "" {
		return v
	}
	candidates := configCandidates()
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return candidates[0]
}

func configCandidates() []string {
	var out []string
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		out = append(out, filepath.Join(xdg, "claudex", Filename))
	} else if home, err := os.UserHomeDir(); err == nil {
		out = append(out, filepath.Join(home, ".config", "claudex", Filename))
	}
	if cwd, err := os.Getwd(); err == nil {
		out = append(out, filepath.Join(cwd, Filename))
	}
	if dir := ExecDir(); dir != "" {
		out = append(out, filepath.Join(dir, Filename))
	}
	return out
}

// ExamplePath tries to locate the example catalog. Checked locations mirror
// DefaultPath but also include ../share/claudex for system installs.
func ExamplePath() string {
	var candidates []string
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(cwd, ExampleFilename))
	}
	if dir := ExecDir(); dir != "" {
		candidates = append(candidates,
			filepath.Join(dir, ExampleFilename),
			filepath.Join(dir, "..", "share", "claudex", ExampleFilename),
		)
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	if len(candidates) > 0 {
		return candidates[0]
	}
	return ExampleFilename
}

func Load() (*Loaded, error) {
	path := DefaultPath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return &Loaded{Path: path, Missing: true}, nil
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("설정 파일 읽기 오류: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("설정 파일 파싱 오류: %w", err)
	}

	for i, p := range cfg.Profiles {
		if p.Name == "" {
			return nil, fmt.Errorf("프로파일 #%d에 \"name\" 필드가 없습니다", i+1)
		}
	}

	return &Loaded{Config: cfg, Path: path}, nil
}

func Save(path string, cfg Config) error {
	buf, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	buf = append(buf, '\n')
	return os.WriteFile(path, buf, 0o600)
}

func LoadExample() ([]Profile, error) {
	path := ExamplePath()
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	return cfg.Profiles, nil
}

func FindProfile(profiles []Profile, name string) *Profile {
	for i := range profiles {
		if equalFold(profiles[i].Name, name) {
			return &profiles[i]
		}
	}
	return nil
}

func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 32
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 32
		}
		if ca != cb {
			return false
		}
	}
	return true
}
