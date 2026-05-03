package codex

import (
	"bytes"
	"strings"
	"testing"
)

func TestTranslateStream_TextProducesAnthropicSSE(t *testing.T) {
	upstream := codexStream(
		evt("response.output_item.added",
			`{"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"i_1"}}`),
		evt("response.output_text.delta",
			`{"type":"response.output_text.delta","output_index":0,"delta":"hi"}`),
		evt("response.output_item.done",
			`{"type":"response.output_item.done","output_index":0,"item":{"type":"message"}}`),
		evt("response.completed",
			`{"type":"response.completed","response":{"usage":{"input_tokens":3,"output_tokens":1}}}`),
	)
	var out bytes.Buffer
	if err := TranslateStream(strings.NewReader(upstream), &out, StreamOptions{
		MessageID: "msg_1",
		Model:     "gpt-5.4",
	}, nil); err != nil {
		t.Fatal(err)
	}
	got := out.String()

	wantContains := []string{
		`event: message_start`,
		`"id":"msg_1"`,
		`"model":"gpt-5.4"`,
		`event: ping`,
		`event: content_block_start`,
		`"type":"text"`,
		`event: content_block_delta`,
		`"text":"hi","type":"text_delta"`,
		`event: content_block_stop`,
		`event: message_delta`,
		`"stop_reason":"end_turn"`,
		`event: message_stop`,
	}
	for _, w := range wantContains {
		if !strings.Contains(got, w) {
			t.Errorf("출력에 누락: %q\n전체:\n%s", w, got)
		}
	}
}

func TestTranslateStream_ToolUseProducesInputJsonDelta(t *testing.T) {
	upstream := codexStream(
		evt("response.output_item.added",
			`{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","call_id":"c1","name":"Bash"}}`),
		evt("response.function_call_arguments.delta",
			`{"type":"response.function_call_arguments.delta","output_index":0,"delta":"{\"x\":1}"}`),
		evt("response.output_item.done",
			`{"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","arguments":"{\"x\":1}"}}`),
		evt("response.completed", `{"type":"response.completed","response":{}}`),
	)
	var out bytes.Buffer
	_ = TranslateStream(strings.NewReader(upstream), &out, StreamOptions{MessageID: "m", Model: "x"}, nil)
	got := out.String()
	if !strings.Contains(got, `"type":"tool_use"`) || !strings.Contains(got, `"id":"c1"`) {
		t.Errorf("tool_use content_block_start 누락:\n%s", got)
	}
	if !strings.Contains(got, `"type":"input_json_delta"`) {
		t.Errorf("input_json_delta 누락:\n%s", got)
	}
	if !strings.Contains(got, `"stop_reason":"tool_use"`) {
		t.Errorf("stop_reason=tool_use 누락:\n%s", got)
	}
}

func TestTranslateStream_RateLimitEmitsErrorEvent(t *testing.T) {
	upstream := codexStream(
		evt("codex.rate_limits",
			`{"type":"codex.rate_limits","rate_limits":{"limit_reached":true}}`),
	)
	var out bytes.Buffer
	if err := TranslateStream(strings.NewReader(upstream), &out, StreamOptions{MessageID: "m", Model: "x"}, nil); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, `event: error`) {
		t.Errorf("error 이벤트 누락:\n%s", got)
	}
	if !strings.Contains(got, `"rate_limit_error"`) {
		t.Errorf("rate_limit_error type 누락:\n%s", got)
	}
}

func TestTranslateStream_OnFinishCalled(t *testing.T) {
	upstream := codexStream(
		evt("response.output_item.added",
			`{"type":"response.output_item.added","output_index":0,"item":{"type":"message"}}`),
		evt("response.output_item.done",
			`{"type":"response.output_item.done","output_index":0,"item":{"type":"message"}}`),
		evt("response.completed",
			`{"type":"response.completed","response":{"usage":{"input_tokens":7,"output_tokens":2}}}`),
	)
	var (
		called      bool
		gotStop     StopReason
		gotInTokens int
	)
	_ = TranslateStream(strings.NewReader(upstream), new(bytes.Buffer), StreamOptions{}, func(stop StopReason, u *CodexUsage) {
		called = true
		gotStop = stop
		if u != nil {
			gotInTokens = u.InputTokens
		}
	})
	if !called {
		t.Fatal("onFinish 호출 안됨")
	}
	if gotStop != StopEndTurn || gotInTokens != 7 {
		t.Errorf("onFinish 인자: stop=%s in=%d", gotStop, gotInTokens)
	}
}
