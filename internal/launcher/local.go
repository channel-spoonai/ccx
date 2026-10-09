package launcher

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/channel-spoonai/ccx/internal/config"
	"github.com/channel-spoonai/ccx/internal/ctxwin"
)

// 로컬 모델 서버용 기본값.
//
// auto 모드는 툴 호출마다 대화 전체를 <transcript>로 묶은 분류기 요청을 따로 보낸다.
// 로컬 서버에서는 이 요청이 매번 캐시 없는 ~30K 프리필이 되고(MTPLX 실측 460 tok/s),
// Claude Code의 분류기 타임아웃(약 60초)을 넘겨 끊긴다 — 끝까지 못 가니 KV가 커밋되지
// 않아 다음 분류도 처음부터 다시 프리필한다. 2026-09 실측: 분류 요청 31건 중 완료 0건,
// 툴 호출마다 약 65초 지연, 동시 프리필 경합으로 메인 턴 TTFT 0.5초 → 7~13초.
//
// away summary(자리를 비웠다 돌아오면 보이는 recap)는 메인 대화를 포크한 부가 요청이라
// 대화 해시가 같아 같은 세션 id로 나간다. 세션을 고정하는 로컬 서버에서는 그 내용이 메인
// 세션의 committed 스트림에 커밋돼 다음 메인 턴의 프리픽스가 어긋난다.
const (
	// localPermissionFallback은 auto 대신 쓰는 모드 — 편집은 자동 승인, 셸은 확인.
	localPermissionFallback = "acceptEdits"
	awaySummaryEnv          = "CLAUDE_CODE_ENABLE_AWAY_SUMMARY"
)

// localAdjustments는 배너에 보여줄 변경 내역.
type localAdjustments struct {
	PermissionFrom string // 비어 있으면 권한 모드를 건드리지 않음
	PermissionTo   string
	AwaySummaryOff bool
	Guard1M        ctxwin.Guard       // 기본 opus[1m] 차단 내역 (로컬 여부 무관)
	SettingsPin    ctxwin.SettingsPin // settings.json의 Claude 모델 ID → 별칭 고정 내역
}

// applyLocalDefaults는 로컬 프로파일이면 auto 모드와 away summary를 끈다.
// p는 복사본을 돌려주며(Env 맵도 복제) 호출자의 프로파일은 바꾸지 않는다.
// 프록시 경로는 prepare 단계에서 baseUrl을 127.0.0.1 프록시로 바꾸므로 그 전에 호출해야 한다.
func applyLocalDefaults(p *config.Profile, args []string) (*config.Profile, []string, localAdjustments) {
	var adj localAdjustments

	// 프로파일에 명시한 permissionMode는 로컬 여부와 무관하게 적용한다 (CLI 인자가 우선).
	if mode := strings.TrimSpace(p.PermissionMode); mode != "" {
		if !hasPermissionArg(args) {
			args = withPermissionMode(args, mode)
		}
		// 명시값은 사용자의 선택이라 배너 경고 대상이 아니다.
	}

	if !IsLocalProfile(p) {
		return p, args, adj
	}

	if strings.TrimSpace(p.PermissionMode) == "" && !hasPermissionArg(args) {
		if effectiveSettings().Permissions.DefaultMode == "auto" {
			args = withPermissionMode(args, localPermissionFallback)
			adj.PermissionFrom, adj.PermissionTo = "auto", localPermissionFallback
		}
	}

	if _, userSet := p.Env[awaySummaryEnv]; !userSet {
		if _, ambient := os.LookupEnv(awaySummaryEnv); !ambient {
			cp := *p
			cp.Env = make(map[string]string, len(p.Env)+1)
			for k, v := range p.Env {
				cp.Env[k] = v
			}
			cp.Env[awaySummaryEnv] = "0"
			p = &cp
			adj.AwaySummaryOff = true
		}
	}
	return p, args, adj
}

// IsLocalProfile은 baseUrl 호스트가 loopback이거나 사설망 주소면 true.
// 같은 LAN의 다른 머신(예: 192.168.x의 Mac mini)도 로컬 모델 서버라 같은 문제를 겪는다.
func IsLocalProfile(p *config.Profile) bool {
	return config.IsLocalBaseURL(p.BaseURL)
}

// hasPermissionArg는 사용자가 CLI로 권한 모드를 직접 골랐는지 본다.
func hasPermissionArg(args []string) bool {
	for _, a := range args {
		if a == "--" {
			return false
		}
		if a == "--permission-mode" || strings.HasPrefix(a, "--permission-mode=") ||
			a == "--dangerously-skip-permissions" {
			return true
		}
	}
	return false
}

// withPermissionMode는 플래그를 맨 앞에 넣는다 — 루트 옵션이라 서브커맨드(mcp 등) 앞에 와도 된다.
func withPermissionMode(args []string, mode string) []string {
	return append([]string{"--permission-mode", mode}, args...)
}

// claudeSettings는 ccx가 참고하는 Claude Code 설정 필드.
type claudeSettings struct {
	Model       string `json:"model"`
	Permissions struct {
		DefaultMode string `json:"defaultMode"`
	} `json:"permissions"`
}

// effectiveSettings는 Claude Code 설정 파일을 우선순위대로 겹쳐 읽는다
// (user < project < local, 필드별로 비어 있지 않은 값이 이긴다). managed 정책은
// CLI 인자로도 못 바꾸므로 보지 않는다.
func effectiveSettings() claudeSettings {
	var files []string
	if dir := claudeConfigDir(); dir != "" {
		files = append(files, filepath.Join(dir, "settings.json"))
	}
	if cwd, err := os.Getwd(); err == nil {
		files = append(files,
			filepath.Join(cwd, ".claude", "settings.json"),
			filepath.Join(cwd, ".claude", "settings.local.json"))
	}
	var eff claudeSettings
	for _, f := range files {
		s := readSettings(f)
		if s.Model != "" {
			eff.Model = s.Model
		}
		if s.Permissions.DefaultMode != "" {
			eff.Permissions.DefaultMode = s.Permissions.DefaultMode
		}
	}
	return eff
}

// modelChoice는 Claude Code가 시작 모델을 정할 때 보는, 프로파일 밖의 사용자 지정을 모은다.
func modelChoice(args []string) ctxwin.ModelChoice {
	return ctxwin.ModelChoice{CLI: modelArg(args), Settings: effectiveSettings().Model}
}

// modelArg는 CLI의 --model 값을 찾는다 ("--" 뒤는 claude 인자가 아니다).
func modelArg(args []string) string {
	for i, a := range args {
		if a == "--" {
			break
		}
		if a == "--model" && i+1 < len(args) {
			return args[i+1]
		}
		if v, ok := strings.CutPrefix(a, "--model="); ok {
			return v
		}
	}
	return ""
}

func claudeConfigDir() string {
	if d := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR")); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude")
}

func readSettings(path string) claudeSettings {
	var s claudeSettings
	data, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(data, &s) != nil {
		return claudeSettings{}
	}
	s.Model = strings.TrimSpace(s.Model)
	s.Permissions.DefaultMode = strings.TrimSpace(s.Permissions.DefaultMode)
	return s
}

func printLocalAdjustments(adj localAdjustments) {
	if adj.PermissionTo != "" {
		fmt.Printf("\x1B[36m[ccx]\x1B[0m Permission mode: %s → %s (local model — auto-mode classifier disabled)\n", adj.PermissionFrom, adj.PermissionTo)
	}
	if adj.AwaySummaryOff {
		fmt.Printf("\x1B[36m[ccx]\x1B[0m Away summary (recap): off (local model — keeps the session cache aligned)\n")
	}
	if pin := adj.SettingsPin; pin.To != "" {
		fmt.Printf("\x1B[36m[ccx]\x1B[0m Startup model: %s → %s (settings.json model is not served by this profile)\n", pin.From, pin.To)
	}
	if adj.Guard1M.Disable1M {
		fmt.Printf("\x1B[36m[ccx]\x1B[0m 1M context: off (no model declares ≥1M — overrides Claude Code's default opus[1m])\n")
	}
	if m := adj.Guard1M.PinModel; m != "" {
		fmt.Printf("\x1B[36m[ccx]\x1B[0m Startup model: %s (no model declares ≥1M — avoids Claude Code's default opus[1m])\n", m)
	}
}
