package codex

import (
	"errors"
	"strings"
	"testing"
)

func TestAccumulateResponse_TextOnly(t *testing.T) {
	stream := codexStream(
		evt("response.output_item.added",
			`{"type":"response.output_item.added","output_index":0,"item":{"type":"message"}}`),
		evt("response.output_text.delta",
			`{"type":"response.output_text.delta","output_index":0,"delta":"Hello "}`),
		evt("response.output_text.delta",
			`{"type":"response.output_text.delta","output_index":0,"delta":"world"}`),
		evt("response.output_item.done",
			`{"type":"response.output_item.done","output_index":0,"item":{"type":"message"}}`),
		evt("response.completed",
			`{"type":"response.completed","response":{"usage":{"input_tokens":5,"output_tokens":2}}}`),
	)
	got, err := AccumulateResponse(strings.NewReader(stream), AccumulateOptions{
		MessageID: "msg_x",
		Model:     "gpt-5.4",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "msg_x" || got.Model != "gpt-5.4" || got.Type != "message" || got.Role != "assistant" {
		t.Errorf("메타 필드 잘못됨: %+v", got)
	}
	if len(got.Content) != 1 || got.Content[0].Type != "text" || got.Content[0].Text != "Hello world" {
		t.Errorf("text 누적 잘못됨: %+v", got.Content)
	}
	if got.StopReason != StopEndTurn {
		t.Errorf("stop reason: %s", got.StopReason)
	}
	if got.Usage.InputTokens != 5 || got.Usage.OutputTokens != 2 {
		t.Errorf("usage: %+v", got.Usage)
	}
}

func TestAccumulateResponse_ToolUseInputParsed(t *testing.T) {
	stream := codexStream(
		evt("response.output_item.added",
			`{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","call_id":"c1","name":"Bash"}}`),
		evt("response.function_call_arguments.delta",
			`{"type":"response.function_call_arguments.delta","output_index":0,"delta":"{\"cmd\":\"ls\"}"}`),
		evt("response.output_item.done",
			`{"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","arguments":"{\"cmd\":\"ls\"}"}}`),
		evt("response.completed", `{"type":"response.completed","response":{}}`),
	)
	got, err := AccumulateResponse(strings.NewReader(stream), AccumulateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Content) != 1 || got.Content[0].Type != "tool_use" || got.Content[0].ID != "c1" || got.Content[0].Name != "Bash" {
		t.Errorf("tool_use 매핑 잘못됨: %+v", got.Content)
	}
	if string(got.Content[0].Input) != `{"cmd":"ls"}` {
		t.Errorf("input JSON 보존 실패: %s", got.Content[0].Input)
	}
	if got.StopReason != StopToolUse {
		t.Errorf("stop_reason: %s, want tool_use", got.StopReason)
	}
}

func TestAccumulateResponse_RateLimitErrorPropagated(t *testing.T) {
	stream := codexStream(
		evt("codex.rate_limits",
			`{"type":"codex.rate_limits","rate_limits":{"limit_reached":true}}`),
	)
	_, err := AccumulateResponse(strings.NewReader(stream), AccumulateOptions{})
	var up *UpstreamError
	if !errors.As(err, &up) || up.Kind != ErrorRateLimit {
		t.Errorf("rate_limit이 그대로 전파되어야 함: %v", err)
	}
}

func TestAccumulateResponse_DiscardsEmptyText(t *testing.T) {
	stream := codexStream(
		evt("response.output_item.added",
			`{"type":"response.output_item.added","output_index":0,"item":{"type":"message"}}`),
		evt("response.output_item.done",
			`{"type":"response.output_item.done","output_index":0,"item":{"type":"message"}}`),
		evt("response.completed", `{"type":"response.completed","response":{}}`),
	)
	got, _ := AccumulateResponse(strings.NewReader(stream), AccumulateOptions{})
	if len(got.Content) != 0 {
		t.Errorf("빈 text 블록은 응답에서 제외되어야 함: %+v", got.Content)
	}
}
