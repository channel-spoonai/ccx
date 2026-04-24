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

// DefaultPath returns the canonical config location. Fallback to CWD/exec dir
// was intentionally removed so secrets don't land in a project repo by accident.
// $CLAUDEX_CONFIG still overrides for power users / tests.
func DefaultPath() string {
	if v := os.Getenv(EnvOverride); v != "" {
		return v
	}
	return canonicalPath()
}

func canonicalPath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "claudex", Filename)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		// 최후 수단 — 사실상 도달하지 않음
		return filepath.Join(".", Filename)
	}
	return filepath.Join(home, ".config", "claudex", Filename)
}

// legacyPaths lists the pre-1단계 fallback locations. Checked once on Load to
// migrate any existing file into the canonical path.
func legacyPaths() []string {
	var out []string
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
		if from, ok := tryMigrateLegacy(path); ok {
			fmt.Fprintf(os.Stderr, "[claudex] 설정 파일을 %s → %s 로 이동했습니다.\n", from, path)
		} else {
			return &Loaded{Path: path, Missing: true}, nil
		}
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
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	// 기존 디렉토리가 더 느슨한 퍼미션으로 존재할 수 있어 타이트닝 시도.
	// Windows는 Chmod mode를 대부분 무시하지만 에러는 내지 않음.
	_ = os.Chmod(dir, 0o700)

	buf, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	buf = append(buf, '\n')
	return os.WriteFile(path, buf, 0o600)
}

// tryMigrateLegacy moves a config file from the old CWD/exec-dir locations
// into the canonical path. Returns (source, true) on success so the caller
// can inform the user, or (_, false) if nothing was migrated.
func tryMigrateLegacy(dst string) (string, bool) {
	for _, src := range legacyPaths() {
		if src == dst {
			continue
		}
		if _, err := os.Stat(src); err != nil {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			continue
		}
		if err := os.Rename(src, dst); err == nil {
			_ = os.Chmod(dst, 0o600)
			return src, true
		}
		// 크로스 디바이스 등 rename 실패 — 복사 + 삭제로 폴백
		if err := copyFile(src, dst); err == nil {
			_ = os.Chmod(dst, 0o600)
			_ = os.Remove(src)
			return src, true
		}
	}
	return "", false
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o600)
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
