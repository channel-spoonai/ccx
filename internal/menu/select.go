package menu

import (
	"fmt"
	"os"

	"github.com/channel-spoonai/ccx/internal/config"
)

type ActionKind int

const (
	ActionLaunch ActionKind = iota
	ActionAdd
	ActionEdit
	ActionDelete
	ActionCancel
)

type Action struct {
	Kind    ActionKind
	Index   int
	Profile *config.Profile
}

type itemKind int

const (
	itemProfile itemKind = iota
	itemAdd
)

type item struct {
	kind    itemKind
	profile *config.Profile
	index   int
}

// SelectProfile renders the main menu and returns the user's action. Requires
// a TTY; the caller should fall back to -xSet when stdin isn't interactive.
//
// notice는 헤더 아래에 한 줄로 표시할 안내 (예: "v0.4.0"). 빈 문자열이면 생략.
func SelectProfile(profiles []config.Profile, notice string) (Action, error) {
	if !IsTTY() {
		return Action{}, fmt.Errorf("인터랙티브 모드에는 TTY가 필요합니다. -xSet으로 프로파일을 지정하세요")
	}

	restore, err := MakeRaw()
	if err != nil {
		return Action{}, err
	}
	defer restore()

	items := make([]item, 0, len(profiles)+1)
	for i := range profiles {
		items = append(items, item{kind: itemProfile, profile: &profiles[i], index: i})
	}
	items = append(items, item{kind: itemAdd})

	selected := 0
	render := func() {
		ClearScreen()
		fmt.Print("\r\n")
		fmt.Print("  \x1B[1m\x1B[36m ccx \x1B[0m\x1B[90m— 프로파일을 선택하세요\x1B[0m\r\n")
		if notice != "" {
			fmt.Printf("  \x1B[33m⚑ 업데이트 %s 사용 가능 — `ccx update`\x1B[0m\r\n", notice)
		}
		fmt.Print("\r\n")
		if len(profiles) == 0 {
			fmt.Print("   \x1B[90m(등록된 프로파일이 없습니다)\x1B[0m\r\n\r\n")
		}
		for i, it := range items {
			cursor := " "
			if i == selected {
				cursor = "\x1B[33m❯\x1B[0m"
			}
			switch it.kind {
			case itemProfile:
				p := it.profile
				name := p.Name
				if i == selected {
					name = "\x1B[1m\x1B[33m" + name + "\x1B[0m"
				} else {
					name = "\x1B[37m" + name + "\x1B[0m"
				}
				desc := ""
				if p.Description != "" {
					desc = "  \x1B[90m" + p.Description + "\x1B[0m"
				}
				fmt.Printf("   %s %d. %s%s\r\n", cursor, i+1, name, desc)
				if i == selected && p.Models != nil {
					m := p.Models
					if m.Opus != "" {
						fmt.Printf("      \x1B[90m  opus   → %s\x1B[0m\r\n", m.Opus)
					}
					if m.Sonnet != "" {
						fmt.Printf("      \x1B[90m  sonnet → %s\x1B[0m\r\n", m.Sonnet)
					}
					if m.Haiku != "" {
						fmt.Printf("      \x1B[90m  haiku  → %s\x1B[0m\r\n", m.Haiku)
					}
				}
			case itemAdd:
				label := "+ 새 프로바이더 추가..."
				if i == selected {
					label = "\x1B[1m\x1B[32m" + label + "\x1B[0m"
				} else {
					label = "\x1B[32m" + label + "\x1B[0m"
				}
				fmt.Printf("   %s %d. %s\r\n", cursor, i+1, label)
			}
		}
		fmt.Print("\r\n  \x1B[90m ↑↓ 이동  Enter 선택  e 편집  d 삭제  Esc 취소\x1B[0m\r\n\r\n")
	}

	render()

	pick := func(idx int) Action {
		it := items[idx]
		if it.kind == itemProfile {
			return Action{Kind: ActionLaunch, Index: it.index, Profile: it.profile}
		}
		return Action{Kind: ActionAdd}
	}

	for {
		key, err := ReadKey()
		if err != nil {
			return Action{}, err
		}
		switch key.Name {
		case "ctrl-c":
			ExitAltScreen()
			restore()
			fmt.Println("취소되었습니다.")
			os.Exit(0)
		case "esc":
			return Action{Kind: ActionCancel}, nil
		case "enter":
			return pick(selected), nil
		case "up":
			if selected > 0 {
				selected--
				render()
			}
		case "down":
			if selected < len(items)-1 {
				selected++
				render()
			}
		}
		if key.Name != "" {
			continue
		}
		r := key.Rune
		if r >= '1' && r <= '9' {
			idx := int(r - '1')
			if idx < len(items) {
				return pick(idx), nil
			}
			continue
		}
		if (r == 'e' || r == 'E') && items[selected].kind == itemProfile {
			it := items[selected]
			return Action{Kind: ActionEdit, Index: it.index, Profile: it.profile}, nil
		}
		if (r == 'd' || r == 'D') && items[selected].kind == itemProfile {
			it := items[selected]
			return Action{Kind: ActionDelete, Index: it.index, Profile: it.profile}, nil
		}
	}
}
