//go:build windows

package menu

import (
	"os"

	"golang.org/x/sys/windows"
)

// enableVT turns on ENABLE_VIRTUAL_TERMINAL_PROCESSING on stdout so that ANSI
// escape sequences render on legacy Windows consoles (cmd.exe). Modern
// Terminal/PowerShell 7 already have it enabled but this is a no-op there.
func enableVT() {
	h := windows.Handle(os.Stdout.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(h, &mode); err != nil {
		return
	}
	_ = windows.SetConsoleMode(h, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING)
}
