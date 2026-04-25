package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/yobuce/claudex/internal/config"
	"github.com/yobuce/claudex/internal/flows"
	"github.com/yobuce/claudex/internal/launcher"
	"github.com/yobuce/claudex/internal/menu"
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
				fmt.Fprintln(os.Stderr, `사용법: claudex -xSet "프로파일 이름" [claude 옵션...]`)
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
	args := parseArgs(os.Args[1:])

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

	runInteractive(loaded, args.claudeArgs)
}

func runInteractive(loaded *config.Loaded, claudeArgs []string) {
	menu.EnterAltScreen()
	defer menu.ExitAltScreen()

	for {
		action, err := menu.SelectProfile(loaded.Config.Profiles)
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
