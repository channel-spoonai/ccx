package launcher

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/channel-spoonai/ccx/internal/config"
)

// withClaudeSettings는 CLAUDE_CONFIG_DIR와 cwd를 임시 디렉터리로 돌려 설정 파일을 격리한다.
func withClaudeSettings(t *testing.T, userJSON string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	if userJSON != "" {
		if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(userJSON), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(t.TempDir())
}

const autoSettings = `{"permissions":{"defaultMode":"auto"}}`

func TestIsLocalProfile(t *testing.T) {
	cases := map[string]bool{
		"http://localhost:8000":          true,
		"http://127.0.0.1:1234":          true,
		"http://[::1]:8000":              true,
		"http://192.168.0.176:8000":      true,
		"http://10.0.0.5:8000":           true,
		"http://mac-mini.local:8000":     true,
		"https://api.z.ai/api/anthropic": false,
		"https://openrouter.ai/api":      false,
		"":                               false,
	}
	for u, want := range cases {
		if got := IsLocalProfile(&config.Profile{BaseURL: u}); got != want {
			t.Errorf("IsLocalProfile(%q) = %v, want %v", u, got, want)
		}
	}
}

func TestLocalDowngradesAutoMode(t *testing.T) {
	withClaudeSettings(t, autoSettings)
	t.Setenv(awaySummaryEnv, "")
	os.Unsetenv(awaySummaryEnv)

	p := &config.Profile{BaseURL: "http://localhost:8000"}
	got, args, adj := applyLocalDefaults(p, []string{"-p", "hi"})

	if want := []string{"--permission-mode", "acceptEdits", "-p", "hi"}; !reflect.DeepEqual(args, want) {
		t.Errorf("args = %v, want %v", args, want)
	}
	if adj.PermissionTo != "acceptEdits" || !adj.AwaySummaryOff {
		t.Errorf("adj = %+v", adj)
	}
	if got.Env[awaySummaryEnv] != "0" {
		t.Errorf("away summary env = %q, want 0", got.Env[awaySummaryEnv])
	}
	if p.Env != nil {
		t.Error("원본 프로파일의 Env가 변경됨")
	}
}

func TestLocalLeavesNonAutoModeAlone(t *testing.T) {
	withClaudeSettings(t, `{"permissions":{"defaultMode":"default"}}`)
	_, args, adj := applyLocalDefaults(&config.Profile{BaseURL: "http://localhost:8000"}, nil)
	if len(args) != 0 || adj.PermissionTo != "" {
		t.Errorf("auto가 아닌데 권한 모드를 바꿈: args=%v adj=%+v", args, adj)
	}
}

func TestProjectLocalSettingsOverrideUser(t *testing.T) {
	withClaudeSettings(t, `{"permissions":{"defaultMode":"default"}}`)
	if err := os.MkdirAll(".claude", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(".claude", "settings.local.json"), []byte(autoSettings), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := effectiveDefaultMode(); got != "auto" {
		t.Errorf("effectiveDefaultMode = %q, want auto", got)
	}
}

func TestExplicitPermissionArgWins(t *testing.T) {
	withClaudeSettings(t, autoSettings)
	for _, in := range [][]string{
		{"--permission-mode", "auto"},
		{"--permission-mode=plan"},
		{"--dangerously-skip-permissions"},
	} {
		_, args, adj := applyLocalDefaults(&config.Profile{BaseURL: "http://localhost:8000"}, in)
		if !reflect.DeepEqual(args, in) || adj.PermissionTo != "" {
			t.Errorf("CLI 지정 %v가 덮어써짐: %v", in, args)
		}
	}
}

func TestProfilePermissionModeWins(t *testing.T) {
	withClaudeSettings(t, autoSettings)
	p := &config.Profile{BaseURL: "http://localhost:8000", PermissionMode: "auto"}
	_, args, adj := applyLocalDefaults(p, nil)
	if want := []string{"--permission-mode", "auto"}; !reflect.DeepEqual(args, want) || adj.PermissionTo != "" {
		t.Errorf("args=%v adj=%+v", args, adj)
	}
}

func TestRemoteProfileUntouched(t *testing.T) {
	withClaudeSettings(t, autoSettings)
	p := &config.Profile{BaseURL: "https://api.z.ai/api/anthropic"}
	got, args, adj := applyLocalDefaults(p, []string{"-c"})
	if got != p || !reflect.DeepEqual(args, []string{"-c"}) || adj != (localAdjustments{}) {
		t.Errorf("원격 프로파일이 변경됨: args=%v adj=%+v", args, adj)
	}
}

func TestAwaySummaryUserValueWins(t *testing.T) {
	withClaudeSettings(t, "")
	p := &config.Profile{BaseURL: "http://localhost:8000", Env: map[string]string{awaySummaryEnv: "1"}}
	got, _, adj := applyLocalDefaults(p, nil)
	if got.Env[awaySummaryEnv] != "1" || adj.AwaySummaryOff {
		t.Errorf("profile.env 값이 덮어써짐: %q", got.Env[awaySummaryEnv])
	}

	t.Setenv(awaySummaryEnv, "1")
	_, _, adj = applyLocalDefaults(&config.Profile{BaseURL: "http://localhost:8000"}, nil)
	if adj.AwaySummaryOff {
		t.Error("셸 환경변수 값이 덮어써짐")
	}
}
