package codex

import (
	"strings"
	"testing"
)

func TestParseSSE_TwoEventsLF(t *testing.T) {
	in := "event: a\ndata: 1\n\nevent: b\ndata: 2\n\n"
	var got []SSEEvent
	if err := ParseSSE(strings.NewReader(in), func(e SSEEvent) bool {
		got = append(got, e)
		return true
	}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Event != "a" || got[0].Data != "1" || got[1].Event != "b" || got[1].Data != "2" {
		t.Errorf("got %+v", got)
	}
}

func TestParseSSE_CRLFAndMixedSeparators(t *testing.T) {
	in := "event: a\r\ndata: 1\r\n\r\nevent: b\rdata: 2\r\r"
	var got []SSEEvent
	_ = ParseSSE(strings.NewReader(in), func(e SSEEvent) bool {
		got = append(got, e)
		return true
	})
	if len(got) != 2 {
		t.Fatalf("got %d events: %+v", len(got), got)
	}
	if got[0].Event != "a" || got[1].Event != "b" {
		t.Errorf("event names: %+v", got)
	}
}

func TestParseSSE_MultilineData(t *testing.T) {
	in := "event: m\ndata: line1\ndata: line2\n\n"
	var got []SSEEvent
	_ = ParseSSE(strings.NewReader(in), func(e SSEEvent) bool {
		got = append(got, e)
		return true
	})
	if len(got) != 1 || got[0].Data != "line1\nline2" {
		t.Errorf("multi-line data가 \\n으로 join되어야 함: %+v", got)
	}
}

func TestParseSSE_CommentLineSkipped(t *testing.T) {
	in := ": this is a comment\nevent: keep\ndata: ok\n\n"
	var got []SSEEvent
	_ = ParseSSE(strings.NewReader(in), func(e SSEEvent) bool {
		got = append(got, e)
		return true
	})
	if len(got) != 1 || got[0].Event != "keep" {
		t.Errorf("comment 처리 문제: %+v", got)
	}
}

func TestParseSSE_TrailingPartialEventEmitted(t *testing.T) {
	// boundary 없이 끝나는 trailing 블록도 emit 되어야 함.
	in := "event: tail\ndata: x"
	var got []SSEEvent
	_ = ParseSSE(strings.NewReader(in), func(e SSEEvent) bool {
		got = append(got, e)
		return true
	})
	if len(got) != 1 || got[0].Event != "tail" {
		t.Errorf("trailing 이벤트 누락: %+v", got)
	}
}

func TestEncodeSSE_Format(t *testing.T) {
	out, err := EncodeSSE("ping", map[string]string{"hello": "world"})
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	if !strings.HasPrefix(got, "event: ping\n") {
		t.Errorf("prefix 잘못됨: %q", got)
	}
	if !strings.Contains(got, `data: {"hello":"world"}`) {
		t.Errorf("data 라인 잘못됨: %q", got)
	}
	if !strings.HasSuffix(got, "\n\n") {
		t.Errorf("\\n\\n 종결 누락: %q", got)
	}
}
