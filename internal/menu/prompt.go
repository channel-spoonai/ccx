package menu

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type PromptOptions struct {
	Default  string
	Required bool
	Prefill  bool // 기본값을 편집 가능한 상태로 버퍼에 미리 채움
}

// PromptLine asks a question and returns the user's answer. If Prefill is
// true, Default is pre-populated in the line buffer (raw mode line editor).
// Otherwise the standard cooked input is used and empty input returns Default.
func PromptLine(question string, opts PromptOptions) (string, error) {
	for {
		var value string
		var err error
		if opts.Prefill && IsTTY() {
			value, err = promptPrefill(question, opts.Default)
		} else {
			value, err = promptCooked(question, opts.Default)
		}
		if err != nil {
			return "", err
		}
		if opts.Required && value == "" {
			fmt.Println("  \x1B[31m값이 필요합니다.\x1B[0m")
			continue
		}
		return value, nil
	}
}

func promptCooked(question, def string) (string, error) {
	hint := ""
	if def != "" {
		hint = " \x1B[90m[" + def + "]\x1B[0m"
	}
	fmt.Printf("  %s%s: ", question, hint)
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	line = strings.TrimRight(line, "\r\n")
	line = strings.TrimSpace(line)
	if line == "" {
		return def, nil
	}
	return line, nil
}

// promptPrefill reads a single line with raw-mode editing and a prefilled
// default. Supports printable runes, Backspace, Enter, Ctrl+U (erase all),
// Ctrl+C (abort). No cursor movement — matches the Node readline.write(def) UX.
func promptPrefill(question, def string) (string, error) {
	restore, err := MakeRaw()
	if err != nil {
		// Fall back to cooked mode.
		return promptCooked(question, def)
	}
	defer restore()

	buf := []rune(def)
	prompt := fmt.Sprintf("  %s: ", question)

	redraw := func() {
		// \r = 줄 시작, \x1B[2K = 줄 삭제
		fmt.Printf("\r\x1B[2K%s%s", prompt, string(buf))
	}
	redraw()

	for {
		key, err := ReadKey()
		if err != nil {
			fmt.Print("\r\n")
			return "", err
		}
		switch key.Name {
		case "ctrl-c":
			restore()
			ExitAltScreen()
			fmt.Print("\r\n")
			os.Exit(130)
		case "enter":
			fmt.Print("\r\n")
			return strings.TrimSpace(string(buf)), nil
		case "backspace":
			if len(buf) > 0 {
				buf = buf[:len(buf)-1]
				redraw()
			}
		case "up", "down", "left", "right", "esc", "unknown":
			// 무시 — 커서 이동 없음
		}
		if key.Name != "" {
			continue
		}
		r := key.Rune
		if r == 0x15 { // Ctrl+U
			buf = buf[:0]
			redraw()
			continue
		}
		if r >= 0x20 {
			buf = append(buf, r)
			redraw()
		}
	}
}

// PromptChoice prints a numbered list of choices and returns the 0-based index.
func PromptChoice(question string, choices []string) (int, error) {
	fmt.Println()
	fmt.Printf("  \x1B[1m%s\x1B[0m\n", question)
	for i, c := range choices {
		fmt.Printf("    %d. %s\n", i+1, c)
	}
	for {
		ans, err := PromptLine("번호", PromptOptions{Required: true})
		if err != nil {
			return 0, err
		}
		idx, err := strconv.Atoi(ans)
		if err == nil && idx >= 1 && idx <= len(choices) {
			return idx - 1, nil
		}
		fmt.Println("  \x1B[31m유효하지 않은 번호입니다.\x1B[0m")
	}
}
