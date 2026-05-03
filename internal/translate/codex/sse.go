package codex

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// SSEEvent는 파싱된 Server-Sent Event 한 블록.
type SSEEvent struct {
	Event string
	Data  string
}

// EncodeSSE는 단일 Anthropic SSE 이벤트를 와이어 형식으로 직렬화.
func EncodeSSE(event string, data any) ([]byte, error) {
	body, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	out := []byte("event: ")
	out = append(out, event...)
	out = append(out, '\n')
	out = append(out, "data: "...)
	out = append(out, body...)
	out = append(out, '\n', '\n')
	return out, nil
}

// ParseSSE는 io.Reader에서 SSE 이벤트를 순서대로 yield한다.
// raine의 sse.ts와 동일한 동작 — 이벤트는 빈 줄로 구분되며, line은 CR/LF/CRLF 어느 것이든 허용.
// 콜백이 false를 반환하면 즉시 중단.
func ParseSSE(r io.Reader, fn func(SSEEvent) bool) error {
	br := bufio.NewReaderSize(r, 64*1024)
	var buf strings.Builder
	for {
		chunk, err := br.ReadString('\n')
		if len(chunk) > 0 {
			buf.WriteString(chunk)
			// 이벤트 경계 확인.
			for {
				b := findBoundary(buf.String())
				if b.start < 0 {
					break
				}
				raw := buf.String()
				block := raw[:b.start]
				rest := raw[b.end:]
				buf.Reset()
				buf.WriteString(rest)
				if evt, ok := parseEventBlock(block); ok {
					if !fn(evt) {
						return nil
					}
				}
			}
		}
		if err != nil {
			if err == io.EOF {
				// 마지막 trailing 블록.
				rem := strings.TrimSpace(buf.String())
				if rem != "" {
					if evt, ok := parseEventBlock(buf.String()); ok {
						_ = fn(evt)
					}
				}
				return nil
			}
			return fmt.Errorf("SSE 읽기 실패: %w", err)
		}
	}
}

type boundary struct{ start, end int }

// findBoundary는 \r\n\r\n / \n\n / \r\r 중 가장 먼저 나오는 위치를 찾는다.
func findBoundary(s string) boundary {
	candidates := []struct {
		sep string
	}{{"\r\n\r\n"}, {"\n\n"}, {"\r\r"}}
	best := -1
	bestLen := 0
	for _, c := range candidates {
		if i := strings.Index(s, c.sep); i >= 0 && (best < 0 || i < best) {
			best = i
			bestLen = len(c.sep)
		}
	}
	if best < 0 {
		return boundary{-1, -1}
	}
	return boundary{best, best + bestLen}
}

func parseEventBlock(raw string) (SSEEvent, bool) {
	var event string
	var dataLines []string
	// 라인 분리: \r\n / \n / \r 어느 것이든 분리자.
	for _, line := range splitLines(raw) {
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		colon := strings.IndexByte(line, ':')
		field := line
		value := ""
		if colon >= 0 {
			field = line[:colon]
			value = line[colon+1:]
			if strings.HasPrefix(value, " ") {
				value = value[1:]
			}
		}
		switch field {
		case "event":
			event = value
		case "data":
			dataLines = append(dataLines, value)
		}
	}
	if event == "" && len(dataLines) == 0 {
		return SSEEvent{}, false
	}
	return SSEEvent{Event: event, Data: strings.Join(dataLines, "\n")}, true
}

func splitLines(s string) []string {
	// 표준 strings.Split이 단일 분리자만 지원해서 직접 처리.
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		} else if c == '\r' {
			out = append(out, s[start:i])
			if i+1 < len(s) && s[i+1] == '\n' {
				i++
			}
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}
