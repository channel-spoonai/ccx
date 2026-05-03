package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	codexauth "github.com/channel-spoonai/ccx/internal/auth/codex"
	"github.com/channel-spoonai/ccx/internal/config"
	"github.com/channel-spoonai/ccx/internal/flows"
	"github.com/channel-spoonai/ccx/internal/launcher"
	"github.com/channel-spoonai/ccx/internal/menu"
	proxy "github.com/channel-spoonai/ccx/internal/proxy/codex"
	"github.com/channel-spoonai/ccx/internal/update"
)

// 빌드 시점에 ldflags로 주입된다 (.goreleaser.yaml).
// dev 빌드는 build.sh가 "dev"를 주입 — update.IsDevBuild가 식별.
var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

type parsedArgs struct {
	profileName string
	claudeArgs  []string
}

func parseArgs(argv []string) parsedArgs {
	var out parsedArgs
	i := 0
	for i < len(argv) {
		if argv[i] == "-xSet" {
			if i+1 >= len(argv) {
				fmt.Fprintln(os.Stderr, "Error: -xSet 뒤에 프로파일 이름이 필요합니다.")
				fmt.Fprintln(os.Stderr, `사용법: ccx -xSet "프로파일 이름" [claude 옵션...]`)
				os.Exit(1)
			}
			out.profileName = argv[i+1]
			i += 2
			continue
		}
		out.claudeArgs = append(out.claudeArgs, argv[i])
		i++
	}
	return out
}

func main() {
	// 이전 `ccx update` 사이클이 남긴 .old 바이너리 잔재를 청소 (Windows 전용 no-op on Unix).
	update.CleanupStaleBinary()

	// Hidden 서브명령: 자식 데몬 모드. 부모 ccx가 SpawnDaemon으로 자기 자신을 재호출할 때 진입.
	// 사용자에겐 노출하지 않으므로 help/문서에도 포함시키지 않는다.
	if proxy.IsDaemonInvocation(os.Args) {
		runProxyDaemon()
		return
	}

	// 사용자용 codex 서브명령: `ccx codex login [--device]` / `ccx codex logout` / `ccx codex status`
	if len(os.Args) >= 2 && os.Args[1] == "codex" {
		runCodexCommand(os.Args[2:])
		return
	}

	// `ccx update` — 자동 자기 갱신.
	if len(os.Args) >= 2 && os.Args[1] == "update" {
		runUpdateCommand(os.Args[2:])
		return
	}

	args := parseArgs(os.Args[1:])

	// 24h에 한 번만 GitHub API 호출. 캐시 hit이면 즉시 노출, miss면 백그라운드 fetch만.
	updateNotice := update.MaybeNotify(version)

	loaded, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}

	if args.profileName != "" {
		if loaded.Missing {
			fmt.Fprintf(os.Stderr, "Error: 설정 파일을 찾을 수 없습니다: %s\n", loaded.Path)
			os.Exit(1)
		}
		profile := config.FindProfile(loaded.Config.Profiles, args.profileName)
		if profile == nil {
			fmt.Fprintf(os.Stderr, "Error: 프로파일 %q을(를) 찾을 수 없습니다.\n\n", args.profileName)
			fmt.Fprintln(os.Stderr, "사용 가능한 프로파일:")
			for _, p := range loaded.Config.Profiles {
				if p.Description != "" {
					fmt.Fprintf(os.Stderr, "  - %s (%s)\n", p.Name, p.Description)
				} else {
					fmt.Fprintf(os.Stderr, "  - %s\n", p.Name)
				}
			}
			os.Exit(1)
		}
		if updateNotice != "" {
			fmt.Fprintf(os.Stderr, "[ccx] 새 버전 %s 사용 가능 — `ccx update`\n", updateNotice)
		}
		if err := launcher.Launch(profile, args.claudeArgs); err != nil {
			if errors.Is(err, launcher.ErrClaudeNotFound()) {
				fmt.Fprintln(os.Stderr, "Error:", err)
				os.Exit(1)
			}
			fmt.Fprintln(os.Stderr, "claude 실행 오류:", err)
			os.Exit(1)
		}
		return
	}

	runInteractive(loaded, args.claudeArgs, updateNotice)
}

// runCodexCommand는 사용자가 `ccx codex ...` 로 호출했을 때 진입.
// login/logout/status 만 지원.
func runCodexCommand(argv []string) {
	if len(argv) == 0 {
		printCodexUsage()
		os.Exit(1)
	}
	switch argv[0] {
	case "login":
		device := false
		for _, a := range argv[1:] {
			if a == "--device" || a == "-d" {
				device = true
			} else {
				fmt.Fprintf(os.Stderr, "Error: 알 수 없는 인자 %q\n", a)
				os.Exit(1)
			}
		}
		if err := codexLogin(device); err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
	case "logout":
		if err := codexauth.ClearAuth(); err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
		fmt.Println("[ccx] Codex 토큰을 삭제했습니다.")
	case "status":
		printCodexStatus()
	case "-h", "--help", "help":
		printCodexUsage()
	default:
		fmt.Fprintf(os.Stderr, "Error: 알 수 없는 codex 명령 %q\n\n", argv[0])
		printCodexUsage()
		os.Exit(1)
	}
}

func printCodexUsage() {
	fmt.Println(`사용법:
  ccx codex login            브라우저 PKCE 플로우로 ChatGPT 계정에 로그인
  ccx codex login --device   디바이스 코드 플로우 (헤드리스/SSH 환경)
  ccx codex logout           저장된 토큰 삭제
  ccx codex status           현재 인증 상태 출력`)
}

func codexLogin(device bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	var (
		tok codexauth.TokenResponse
		err error
	)
	if device {
		tok, err = codexauth.RunDeviceLogin(ctx, func(url, code string) {
			fmt.Printf("\n[ccx] 다른 기기 브라우저에서 다음 URL을 열고 코드를 입력하세요:\n  URL : %s\n  코드: %s\n\n", url, code)
		})
	} else {
		tok, err = codexauth.RunBrowserLogin(ctx, func(url string) {
			fmt.Printf("\n[ccx] 다음 URL을 브라우저에서 열어 인증하세요:\n\n  %s\n\n", url)
		})
	}
	if err != nil {
		return err
	}

	mgr := codexauth.NewManager()
	if err := mgr.PersistInitial(tok); err != nil {
		return fmt.Errorf("토큰 저장 실패: %w", err)
	}
	stored, _ := codexauth.LoadAuth()
	fmt.Printf("[ccx] 인증 성공. 토큰 위치: %s\n", codexauth.AuthPath())
	if stored != nil && stored.AccountID != "" {
		fmt.Printf("[ccx] ChatGPT 계정 ID: %s\n", stored.AccountID)
	}
	return nil
}

func printCodexStatus() {
	stored, err := codexauth.LoadAuth()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
	if stored == nil {
		fmt.Println("[ccx] Codex 인증 안됨 — `ccx codex login` 실행 필요.")
		fmt.Printf("       토큰 저장 경로(login 후): %s\n", codexauth.AuthPath())
		return
	}
	fmt.Printf("[ccx] Codex 인증됨\n")
	fmt.Printf("       토큰 파일 : %s\n", codexauth.AuthPath())
	if stored.AccountID != "" {
		fmt.Printf("       계정 ID   : %s\n", stored.AccountID)
	}
	fmt.Printf("       만료 시각 : %s\n", stored.ExpiresAt.Format(time.RFC3339))
	if stored.IsExpired(time.Now()) {
		fmt.Println("       (만료 임박/만료 — 다음 요청에서 자동 refresh)")
	}
}

// runProxyDaemon은 hidden __codex-proxy 서브명령으로 진입했을 때 실행된다.
// 부모로부터 환경변수로 shared secret과 ppid를 받아 데몬을 띄운다.
func runProxyDaemon() {
	secret := os.Getenv(proxy.CCXProxySecretEnv)
	ppid, _ := strconv.Atoi(os.Getenv(proxy.CCXProxyParentPIDEnv))

	err := proxy.RunDaemon(proxy.DaemonOptions{
		ParentPID:    ppid,
		SharedSecret: secret,
		// IdleTimeout 비활성 — 부모 PID polling(1초 간격, ESRCH 감지)이 lifetime을 정확히 관리.
		// 10분 idle로 자체 종료하면 사용자가 작업 재개 시 ConnectionRefused 발생.
		IdleTimeout: 0,
		ReadyWriter: os.Stdout,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "[ccx codex-proxy]", err)
		os.Exit(1)
	}
}

func runInteractive(loaded *config.Loaded, claudeArgs []string, updateNotice string) {
	menu.EnterAltScreen()
	defer menu.ExitAltScreen()

	for {
		action, err := menu.SelectProfile(loaded.Config.Profiles, updateNotice)
		if err != nil {
			menu.ExitAltScreen()
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
		switch action.Kind {
		case menu.ActionLaunch:
			menu.ExitAltScreen()
			if err := launcher.Launch(action.Profile, claudeArgs); err != nil {
				fmt.Fprintln(os.Stderr, "Error:", err)
				os.Exit(1)
			}
			return
		case menu.ActionCancel:
			menu.ExitAltScreen()
			fmt.Println("취소되었습니다.")
			return
		case menu.ActionAdd:
			if err := flows.Add(loaded); err != nil {
				fmt.Fprintln(os.Stderr, "Error:", err)
			}
		case menu.ActionEdit:
			if err := flows.Edit(loaded, action.Index); err != nil {
				fmt.Fprintln(os.Stderr, "Error:", err)
			}
		case menu.ActionDelete:
			if err := flows.Delete(loaded, action.Index); err != nil {
				fmt.Fprintln(os.Stderr, "Error:", err)
			}
		}

		// 저장 반영을 위해 재로드
		reloaded, err := config.Load()
		if err != nil {
			menu.ExitAltScreen()
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
		loaded = reloaded
	}
}
