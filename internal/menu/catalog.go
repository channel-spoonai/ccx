package menu

import (
	"fmt"
	"strings"
)

// CatalogItem is a selectable row in a searchable list. Pinned items are always
// shown regardless of the filter query (used for "기타 (직접 입력)" / "(이 티어는 설정하지 않음)").
type CatalogItem struct {
	Label       string
	Description string
	Payload     any
	Pinned      bool
}

// SelectFromCatalog renders a scrollable, searchable list. Returns the picked
// payload or nil if the user pressed Esc. Requires raw mode capable TTY.
func SelectFromCatalog(items []CatalogItem, title string, pageSize int) (any, error) {
	if !IsTTY() {
		return nil, fmt.Errorf("인터랙티브 모드에는 TTY가 필요합니다")
	}
	if pageSize <= 0 {
		pageSize = 10
	}

	restore, err := MakeRaw()
	if err != nil {
		return nil, err
	}
	defer restore()

	var query []rune
	selected := 0
	scrollOffset := 0

	normalize := func(s string) string {
		return strings.ReplaceAll(strings.ToLower(s), " ", "")
	}

	getVisible := func() []CatalogItem {
		if len(query) == 0 {
			out := make([]CatalogItem, len(items))
			copy(out, items)
			return out
		}
		q := normalize(string(query))
		var out []CatalogItem
		for _, it := range items {
			if it.Pinned ||
				strings.Contains(normalize(it.Label), q) ||
				strings.Contains(normalize(it.Description), q) {
				out = append(out, it)
			}
		}
		return out
	}

	clampScroll := func(visibleLen int) {
		if visibleLen == 0 {
			scrollOffset = 0
			return
		}
		if selected < scrollOffset {
			scrollOffset = selected
		}
		if selected >= scrollOffset+pageSize {
			scrollOffset = selected - pageSize + 1
		}
		maxOffset := visibleLen - pageSize
		if maxOffset < 0 {
			maxOffset = 0
		}
		if scrollOffset > maxOffset {
			scrollOffset = maxOffset
		}
		if scrollOffset < 0 {
			scrollOffset = 0
		}
	}

	countUnpinned := func(list []CatalogItem) int {
		n := 0
		for _, it := range list {
			if !it.Pinned {
				n++
			}
		}
		return n
	}

	render := func() {
		visible := getVisible()
		matched := countUnpinned(visible)
		totalBase := countUnpinned(items)

		ClearScreen()
		fmt.Print("\r\n")
		fmt.Printf("  \x1B[1m\x1B[36m claudex \x1B[0m\x1B[90m— %s\x1B[0m\r\n\r\n", title)

		counter := ""
		if len(query) > 0 {
			counter = fmt.Sprintf("\x1B[90m  (%d/%d)\x1B[0m", matched, totalBase)
		}
		placeholder := ""
		if len(query) == 0 {
			placeholder = "\x1B[90m타이핑하여 검색\x1B[0m"
		}
		fmt.Printf("  \x1B[36m검색:\x1B[0m \x1B[1m%s\x1B[0m\x1B[7m \x1B[0m%s%s\r\n", string(query), placeholder, counter)
		fmt.Print("  \x1B[90m" + strings.Repeat("─", 56) + "\x1B[0m\r\n\r\n")

		if len(visible) == 0 {
			fmt.Print("   \x1B[90m(일치하는 항목 없음)\x1B[0m\r\n")
		} else {
			clampScroll(len(visible))
			windowEnd := scrollOffset + pageSize
			if windowEnd > len(visible) {
				windowEnd = len(visible)
			}
			hasAbove := scrollOffset > 0
			hasBelow := windowEnd < len(visible)

			if hasAbove {
				fmt.Printf("   \x1B[90m▲ %d개 위\x1B[0m\r\n", scrollOffset)
			} else {
				fmt.Print("\r\n")
			}
			for i := scrollOffset; i < windowEnd; i++ {
				it := visible[i]
				cursor := " "
				if i == selected {
					cursor = "\x1B[33m❯\x1B[0m"
				}
				color := "37"
				if it.Pinned {
					color = "32"
				}
				label := it.Label
				if i == selected {
					label = "\x1B[1m\x1B[33m" + label + "\x1B[0m"
				} else {
					label = "\x1B[" + color + "m" + label + "\x1B[0m"
				}
				desc := ""
				if it.Description != "" {
					desc = "  \x1B[90m" + it.Description + "\x1B[0m"
				}
				fmt.Printf("   %s %s%s\r\n", cursor, label, desc)
			}
			if hasBelow {
				fmt.Printf("   \x1B[90m▼ %d개 아래\x1B[0m\r\n", len(visible)-windowEnd)
			} else {
				fmt.Print("\r\n")
			}
		}

		fmt.Print("\r\n  \x1B[90m 문자 입력: 검색  ↑↓: 이동  Enter: 선택  Backspace: 지우기  Esc: 취소\x1B[0m\r\n\r\n")
	}

	render()

	for {
		key, err := ReadKey()
		if err != nil {
			return nil, err
		}
		visible := getVisible()
		switch key.Name {
		case "ctrl-c":
			ExitAltScreen()
			restore()
			fmt.Println("취소되었습니다.")
			return nil, fmt.Errorf("cancelled")
		case "esc":
			return nil, nil
		case "enter":
			if selected < len(visible) {
				return visible[selected].Payload, nil
			}
		case "up":
			if selected > 0 {
				selected--
				render()
			}
		case "down":
			if selected < len(visible)-1 {
				selected++
				render()
			}
		case "left", "right":
			// 무시
		case "backspace":
			if len(query) > 0 {
				query = query[:len(query)-1]
				selected = 0
				scrollOffset = 0
				render()
			}
		}
		if key.Name != "" {
			continue
		}
		r := key.Rune
		if r >= 0x20 {
			query = append(query, r)
			selected = 0
			scrollOffset = 0
			render()
		}
	}
}
