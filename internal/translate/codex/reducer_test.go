package codex

import (
	"errors"
	"strings"
	"testing"
)

// codexStream은 테스트용으로 raine 형식 그대로 SSE 스트림을 만든다.
func codexStream(events ...string) string {
	var b strings.Builder
	for _, e := range events {
		b.WriteString(e)
		b.WriteString("\n\n")
	}
	return b.String()
}

func evt(typ, dataJSON string) string {
	return "event: " + typ + "\ndata: " + dataJSON
}

func TestReduceUpstream_TextOnly(t *testing.T) {
	stream := codexStream(
		evt("response.output_item.added",
			`{"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"i_1"}}`),
		evt("response.output_text.delta",
			`{"type":"response.output_text.delta","output_index":0,"delta":"Hel"}`),
		evt("response.output_text.delta",
			`{"type":"response.output_text.delta","output_index":0,"delta":"lo"}`),
		evt("response.output_item.done",
			`{"type":"response.output_item.done","output_index":0,"item":{"type":"message"}}`),
		evt("response.completed",
			`{"type":"response.completed","response":{"usage":{"input_tokens":10,"output_tokens":2}}}`),
	)
	var events []ReducerEvent
	if err := ReduceUpstream(strings.NewReader(stream), func(e ReducerEvent) bool {
		events = append(events, e)
		return true
	}); err != nil {
		t.Fatal(err)
	}
	want := []EventKind{EventTextStart, EventTextDelta, EventTextDelta, EventTextStop, EventFinish}
	if len(events) != len(want) {
		t.Fatalf("event count: got %d, want %d (%+v)", len(events), len(want), events)
	}
	for i, k := range want {
		if events[i].Kind != k {
			t.Errorf("event[%d]: got %d, want %d", i, events[i].Kind, k)
		}
	}
	if events[len(events)-1].StopReason != StopEndTurn {
		t.Errorf("stop reason: got %s", events[len(events)-1].StopReason)
	}
}

func TestReduceUpstream_ToolUseFlowAndStopReason(t *testing.T) {
	stream := codexStream(
		evt("response.output_item.added",
			`{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","call_id":"c1","name":"Bash"}}`),
		evt("response.function_call_arguments.delta",
			`{"type":"response.function_call_arguments.delta","output_index":0,"delta":"{\"cmd\":"}`),
		evt("response.function_call_arguments.delta",
			`{"type":"response.function_call_arguments.delta","output_index":0,"delta":"\"ls\"}"}`),
		evt("response.function_call_arguments.done",
			`{"type":"response.function_call_arguments.done","output_index":0}`),
		evt("response.output_item.done",
			`{"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","arguments":"{\"cmd\":\"ls\"}"}}`),
		evt("response.completed",
			`{"type":"response.completed","response":{}}`),
	)
	var events []ReducerEvent
	_ = ReduceUpstream(strings.NewReader(stream), func(e ReducerEvent) bool {
		events = append(events, e)
		return true
	})
	if len(events) < 4 {
		t.Fatalf("events: %+v", events)
	}
	if events[0].Kind != EventToolStart || events[0].ToolID != "c1" || events[0].ToolName != "Bash" {
		t.Errorf("tool-start 잘못됨: %+v", events[0])
	}
	// 마지막 이벤트는 finish이고 stop reason은 tool_use.
	last := events[len(events)-1]
	if last.Kind != EventFinish || last.StopReason != StopToolUse {
		t.Errorf("stop reason: got %s, want tool_use (%+v)", last.StopReason, last)
	}
}

func TestReduceUpstream_ReadToolBuffersUntilDone(t *testing.T) {
	stream := codexStream(
		evt("response.output_item.added",
			`{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","call_id":"c","name":"Read"}}`),
		evt("response.function_call_arguments.delta",
			`{"type":"response.function_call_arguments.delta","output_index":0,"delta":"{\"file\":"}`),
		evt("response.function_call_arguments.delta",
			`{"type":"response.function_call_arguments.delta","output_index":0,"delta":"\"a.go\",\"pages\":\"\"}"}`),
		evt("response.output_item.done",
			`{"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","arguments":"{\"file\":\"a.go\",\"pages\":\"\"}"}}`),
		evt("response.completed",
			`{"type":"response.completed","response":{}}`),
	)
	var deltas []string
	_ = ReduceUpstream(strings.NewReader(stream), func(e ReducerEvent) bool {
		if e.Kind == EventToolDelta {
			deltas = append(deltas, e.PartialJSON)
		}
		return true
	})
	if len(deltas) != 1 {
		t.Fatalf("Read 도구는 done에서 한 번에 emit되어야 함, got %d (%+v)", len(deltas), deltas)
	}
	// pages="" 가 sanitize되어야 함.
	if strings.Contains(deltas[0], "pages") {
		t.Errorf("pages 필드가 sanitize되지 않음: %s", deltas[0])
	}
	if !strings.Contains(deltas[0], `"file":"a.go"`) {
		t.Errorf("file 필드 누락: %s", deltas[0])
	}
}

func TestReduceUpstream_ReasoningItemsIgnored(t *testing.T) {
	stream := codexStream(
		evt("response.output_item.added",
			`{"type":"response.output_item.added","output_index":0,"item":{"type":"reasoning"}}`),
		evt("response.output_item.done",
			`{"type":"response.output_item.done","output_index":0,"item":{"type":"reasoning"}}`),
		evt("response.completed", `{"type":"response.completed","response":{}}`),
	)
	var startCount int
	_ = ReduceUpstream(strings.NewReader(stream), func(e ReducerEvent) bool {
		if e.Kind == EventTextStart || e.Kind == EventToolStart {
			startCount++
		}
		return true
	})
	if startCount != 0 {
		t.Errorf("reasoning이 새 블록을 만들었음: %d", startCount)
	}
}

func TestReduceUpstream_RateLimitSurfaced(t *testing.T) {
	stream := codexStream(
		evt("codex.rate_limits",
			`{"type":"codex.rate_limits","rate_limits":{"limit_reached":true,"primary":{"reset_after_seconds":42}}}`),
	)
	err := ReduceUpstream(strings.NewReader(stream), func(e ReducerEvent) bool { return true })
	var up *UpstreamError
	if !errors.As(err, &up) || up.Kind != ErrorRateLimit {
		t.Fatalf("rate_limit 에러가 surface 안됨: %v", err)
	}
	if up.RetryAfterSeconds != 42 {
		t.Errorf("retry_after 누락: %d", up.RetryAfterSeconds)
	}
}

func TestReduceUpstream_ResponseFailed(t *testing.T) {
	stream := codexStream(
		evt("response.failed",
			`{"type":"response.failed","response":{"error":{"message":"boom"}}}`),
	)
	err := ReduceUpstream(strings.NewReader(stream), func(e ReducerEvent) bool { return true })
	var up *UpstreamError
	if !errors.As(err, &up) || up.Kind != ErrorFailed {
		t.Fatalf("failed 에러 surface 안됨: %v", err)
	}
	if up.Message != "boom" {
		t.Errorf("message: %q", up.Message)
	}
}

func TestReduceUpstream_IncompleteStopReason(t *testing.T) {
	stream := codexStream(
		evt("response.output_item.added",
			`{"type":"response.output_item.added","output_index":0,"item":{"type":"message"}}`),
		evt("response.output_text.delta",
			`{"type":"response.output_text.delta","output_index":0,"delta":"x"}`),
		evt("response.output_item.done",
			`{"type":"response.output_item.done","output_index":0,"item":{"type":"message"}}`),
		evt("response.incomplete",
			`{"type":"response.incomplete","response":{"incomplete_details":{"reason":"max_output_tokens"}}}`),
	)
	var last ReducerEvent
	_ = ReduceUpstream(strings.NewReader(stream), func(e ReducerEvent) bool {
		last = e
		return true
	})
	if last.Kind != EventFinish || last.StopReason != StopMaxTokens {
		t.Errorf("incomplete → max_tokens 매핑 실패: %+v", last)
	}
}

func TestMapUsage_SubtractsCachedTokens(t *testing.T) {
	u := &CodexUsage{
		InputTokens:  100,
		OutputTokens: 5,
	}
	u.InputTokensDetails = &struct {
		CachedTokens int `json:"cached_tokens,omitempty"`
	}{CachedTokens: 30}
	got := MapUsage(u)
	if got.InputTokens != 70 {
		t.Errorf("input_tokens: got %d, want 70", got.InputTokens)
	}
	if got.CacheReadInputTokens != 30 {
		t.Errorf("cache_read_input_tokens: got %d, want 30", got.CacheReadInputTokens)
	}
	if got.OutputTokens != 5 {
		t.Errorf("output_tokens: got %d", got.OutputTokens)
	}
}

func TestMapUsage_NilSafe(t *testing.T) {
	got := MapUsage(nil)
	if got.InputTokens != 0 || got.OutputTokens != 0 {
		t.Errorf("nil usage 처리 실패: %+v", got)
	}
}
