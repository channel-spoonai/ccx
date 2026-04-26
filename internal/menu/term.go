package menu

import (
	"fmt"
	"os"
	"time"

	"golang.org/x/term"
)

// Key represents a single decoded key press. For printable input use Rune.
type Key struct {
	Rune rune
	Name string // "up","down","left","right","enter","esc","backspace","ctrl-c","" (=rune)
}

// byteReader is a background goroutine feeding stdin bytes into a channel so
// ReadKey can distinguish a bare ESC from the start of a CSI sequence by
// peeking with a timeout. Started lazily on first Read.
var (
	byteCh   chan byte
	readOnce bool
)

func startReader() {
	if readOnce {
		return
	}
	readOnce = true
	byteCh = make(chan byte, 64)
	go func() {
		buf := make([]byte, 1)
		for {
			n, err := os.Stdin.Read(buf)
			if err != nil || n == 0 {
				close(byteCh)
				return
			}
			byteCh <- buf[0]
		}
	}()
}

func readByte() (byte, bool) {
	startReader()
	b, ok := <-byteCh
	return b, ok
}

func readWithTimeout(d time.Duration) (byte, bool) {
	startReader()
	select {
	case b, ok := <-byteCh:
		return b, ok
	case <-time.After(d):
		return 0, false
	}
}

func IsTTY() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

type Restorer func()

// MakeRaw puts stdin into raw mode and returns a restore func. On Windows this
// also enables VT processing on stdout so ANSI escapes render correctly.
func MakeRaw() (Restorer, error) {
	fd := int(os.Stdin.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return nil, err
	}
	enableVT()
	return func() { _ = term.Restore(fd, oldState) }, nil
}

// EnterAltScreen / ExitAltScreen toggle the terminal alternate buffer so the
// interactive UI doesn't pollute the user's scrollback. Idempotent.
var altActive bool

func EnterAltScreen() {
	if altActive || !IsTTY() {
		return
	}
	fmt.Print("\x1B[?1049h\x1B[H")
	altActive = true
}

func ExitAltScreen() {
	if !altActive {
		return
	}
	fmt.Print("\x1B[?1049l")
	altActive = false
}

func ClearScreen() {
	fmt.Print("\x1B[2J\x1B[H")
}

// ReadKey reads a single key from stdin. Assumes raw mode is active.
// Handles CSI/SS3 escape sequences (arrow keys) and Esc vs escape-prefixed
// sequences by peeking with a short timeout.
func ReadKey() (Key, error) {
	b, ok := readByte()
	if !ok {
		return Key{}, fmt.Errorf("stdin closed")
	}
	switch b {
	case 0x03:
		return Key{Name: "ctrl-c"}, nil
	case '\r', '\n':
		return Key{Name: "enter"}, nil
	case 0x7f, 0x08:
		return Key{Name: "backspace"}, nil
	case 0x1B:
		// Could be bare ESC or start of a CSI/SS3 sequence. Peek for more.
		next, ok := readWithTimeout(40 * time.Millisecond)
		if !ok {
			return Key{Name: "esc"}, nil
		}
		if next == '[' || next == 'O' {
			third, ok := readWithTimeout(40 * time.Millisecond)
			if !ok {
				return Key{Name: "esc"}, nil
			}
			switch third {
			case 'A':
				return Key{Name: "up"}, nil
			case 'B':
				return Key{Name: "down"}, nil
			case 'C':
				return Key{Name: "right"}, nil
			case 'D':
				return Key{Name: "left"}, nil
			}
			// Discard remainder of unknown CSI (e.g. "[1;5A"): consume until
			// a final byte in 0x40–0x7E is seen.
			if next == '[' && (third >= '0' && third <= '9' || third == ';') {
				for {
					nb, ok := readWithTimeout(10 * time.Millisecond)
					if !ok {
						break
					}
					if nb >= 0x40 && nb <= 0x7E {
						break
					}
				}
			}
			return Key{Name: "unknown"}, nil
		}
		// Alt+<char> — treat as escape for now.
		return Key{Name: "esc"}, nil
	}

	// UTF-8 multi-byte rune
	if b < 0x80 {
		return Key{Rune: rune(b)}, nil
	}
	size := utf8Size(b)
	if size <= 1 {
		return Key{Rune: rune(b)}, nil
	}
	full := make([]byte, size)
	full[0] = b
	for i := 1; i < size; i++ {
		nb, ok := readWithTimeout(50 * time.Millisecond)
		if !ok {
			return Key{Rune: '�'}, nil
		}
		full[i] = nb
	}
	r, _ := decodeRune(full)
	return Key{Rune: r}, nil
}

func utf8Size(b byte) int {
	switch {
	case b&0x80 == 0:
		return 1
	case b&0xE0 == 0xC0:
		return 2
	case b&0xF0 == 0xE0:
		return 3
	case b&0xF8 == 0xF0:
		return 4
	}
	return 1
}

func decodeRune(b []byte) (rune, int) {
	var r rune
	switch len(b) {
	case 1:
		return rune(b[0]), 1
	case 2:
		r = rune(b[0]&0x1F)<<6 | rune(b[1]&0x3F)
	case 3:
		r = rune(b[0]&0x0F)<<12 | rune(b[1]&0x3F)<<6 | rune(b[2]&0x3F)
	case 4:
		r = rune(b[0]&0x07)<<18 | rune(b[1]&0x3F)<<12 | rune(b[2]&0x3F)<<6 | rune(b[3]&0x3F)
	}
	return r, len(b)
}
