package codex

import (
	"encoding/json"
	"strings"
	"testing"
)

// mustJSON은 테스트 fixture를 짧게 쓰려는 헬퍼.
func mustJSON(t *testing.T, s string) json.RawMessage {
	t.Helper()
	if !json.Valid([]byte(s)) {
		t.Fatalf("invalid fixture json: %s", s)
	}
	return json.RawMessage(s)
}

func TestBuildInstructions_StringSystem(t *testing.T) {
	got, err := buildInstructions(json.RawMessage(`"You are helpful"`))
	if err != nil {
		t.Fatal(err)
	}
	if got != "You are helpful" {
		t.Errorf("got %q", got)
	}
}

func TestBuildInstructions_BlockArrayJoinedWithDoubleNewline(t *testing.T) {
	got, err := buildInstructions(json.RawMessage(`[{"type":"text","text":"first"},{"type":"text","text":"second"}]`))
	if err != nil {
		t.Fatal(err)
	}
	if got != "first\n\nsecond" {
		t.Errorf("got %q", got)
	}
}

func TestBuildInstructions_StripsBillingHeader(t *testing.T) {
	got, err := buildInstructions(json.RawMessage(`[{"type":"text","text":"x-anthropic-billing-header: foo"},{"type":"text","text":"real"}]`))
	if err != nil {
		t.Fatal(err)
	}
	if got != "real" {
		t.Errorf("got %q (billing header가 strip되지 않음)", got)
	}
}

func TestBuildInstructions_EmptyMissing(t *testing.T) {
	got, _ := buildInstructions(nil)
	if got != "" {
		t.Errorf("nil system: got %q", got)
	}
}

func TestNormalizeContent_StringWrappedAsTextBlock(t *testing.T) {
	blocks, err := normalizeContent(json.RawMessage(`"hi"`))
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 1 || blocks[0].Type != "text" || blocks[0].Text != "hi" {
		t.Errorf("got %+v", blocks)
	}
}

func TestBuildInput_UserTextOnlyEmitsSingleMessage(t *testing.T) {
	in := []AnthropicMessage{
		{Role: "user", Content: json.RawMessage(`"hello"`)},
	}
	got, err := buildInput(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Type != "message" || got[0].Role != "user" {
		t.Fatalf("got %+v", got)
	}
	if len(got[0].Content) != 1 || got[0].Content[0].Type != "input_text" || got[0].Content[0].Text != "hello" {
		t.Errorf("user content 매핑 잘못됨: %+v", got[0].Content)
	}
}

func TestBuildInput_ToolResultSplitsUserMessage(t *testing.T) {
	// user → text + tool_result + text 형태: tool_result 앞뒤로 message가 쪼개져야 함.
	content := mustJSON(t, `[
		{"type":"text","text":"before"},
		{"type":"tool_result","tool_use_id":"call_1","content":"result body"},
		{"type":"text","text":"after"}
	]`)
	got, err := buildInput([]AnthropicMessage{{Role: "user", Content: content}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("split 결과 3개여야 함, got %d (%+v)", len(got), got)
	}
	if got[0].Type != "message" || got[0].Content[0].Text != "before" {
		t.Errorf("첫 message가 잘못됨: %+v", got[0])
	}
	if got[1].Type != "function_call_output" || got[1].CallID != "call_1" || got[1].Output != "result body" {
		t.Errorf("function_call_output 매핑 잘못됨: %+v", got[1])
	}
	if got[2].Type != "message" || got[2].Content[0].Text != "after" {
		t.Errorf("끝 message가 잘못됨: %+v", got[2])
	}
}

func TestBuildInput_ToolResultErrorPrefix(t *testing.T) {
	content := mustJSON(t, `[
		{"type":"tool_result","tool_use_id":"c","content":"oops","is_error":true}
	]`)
	got, _ := buildInput([]AnthropicMessage{{Role: "user", Content: content}})
	if len(got) != 1 || !strings.Contains(got[0].Output, "[tool execution error]") {
		t.Errorf("error prefix 누락: %+v", got)
	}
}

func TestBuildInput_ToolResultImageOmitted(t *testing.T) {
	content := mustJSON(t, `[
		{"type":"tool_result","tool_use_id":"c","content":[
			{"type":"text","text":"see"},
			{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AAAA"}}
		]}
	]`)
	got, _ := buildInput([]AnthropicMessage{{Role: "user", Content: content}})
	if len(got) != 1 {
		t.Fatalf("got %d items", len(got))
	}
	if !strings.Contains(got[0].Output, "[image omitted: image/png]") {
		t.Errorf("이미지 placeholder 누락: %q", got[0].Output)
	}
	if !strings.Contains(got[0].Output, "see") {
		t.Errorf("text 부분 누락: %q", got[0].Output)
	}
}

func TestBuildInput_AssistantTextAndToolUseInterleaved(t *testing.T) {
	content := mustJSON(t, `[
		{"type":"text","text":"thinking"},
		{"type":"tool_use","id":"call_42","name":"Bash","input":{"cmd":"ls"}},
		{"type":"text","text":"after"}
	]`)
	got, _ := buildInput([]AnthropicMessage{{Role: "assistant", Content: content}})
	if len(got) != 3 {
		t.Fatalf("got %d, want 3 (text → function_call → text 순서 유지)", len(got))
	}
	if got[0].Role != "assistant" || got[0].Content[0].Type != "output_text" {
		t.Errorf("assistant text role/type 잘못됨: %+v", got[0])
	}
	if got[1].Type != "function_call" || got[1].CallID != "call_42" || got[1].Name != "Bash" {
		t.Errorf("function_call 매핑 잘못됨: %+v", got[1])
	}
	if got[1].Arguments != `{"cmd":"ls"}` {
		t.Errorf("arguments JSON 그대로 보존되어야 함: got %q", got[1].Arguments)
	}
}

func TestBuildInput_AssistantToolUseEmptyInputBecomesEmptyObject(t *testing.T) {
	content := mustJSON(t, `[{"type":"tool_use","id":"c","name":"Bash"}]`)
	got, _ := buildInput([]AnthropicMessage{{Role: "assistant", Content: content}})
	if len(got) != 1 || got[0].Arguments != "{}" {
		t.Errorf("빈 input은 {} 으로 직렬화되어야 함: %+v", got)
	}
}

func TestMapToolChoice(t *testing.T) {
	cases := []struct {
		name string
		in   *AnthropicToolChoice
		want string
	}{
		{"nil → auto", nil, `"auto"`},
		{"auto", &AnthropicToolChoice{Type: "auto"}, `"auto"`},
		{"none", &AnthropicToolChoice{Type: "none"}, `"none"`},
		{"any → required", &AnthropicToolChoice{Type: "any"}, `"required"`},
		{"tool with name", &AnthropicToolChoice{Type: "tool", Name: "Bash"}, `{"type":"function","name":"Bash"}`},
		{"tool 무명 → required", &AnthropicToolChoice{Type: "tool"}, `"required"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := mapToolChoice(c.in)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != c.want {
				t.Errorf("got %s, want %s", got, c.want)
			}
		})
	}
}

func TestTranslateRequest_FullExample(t *testing.T) {
	req := &AnthropicRequest{
		Model: "gpt-5.4",
		Messages: []AnthropicMessage{
			{Role: "user", Content: json.RawMessage(`"hi"`)},
		},
		System: json.RawMessage(`"be brief"`),
		Tools: []AnthropicTool{
			{Name: "Bash", Description: "run shell", InputSchema: json.RawMessage(`{"type":"object"}`)},
		},
	}
	out, err := TranslateRequest(req, TranslateOptions{SessionID: "sess-1"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Model != "gpt-5.4" {
		t.Errorf("model 누락: %s", out.Model)
	}
	if out.Instructions != "be brief" {
		t.Errorf("instructions: %q", out.Instructions)
	}
	if !out.Stream || out.Store {
		t.Errorf("stream=true, store=false 강제: got stream=%v store=%v", out.Stream, out.Store)
	}
	if !out.ParallelToolCalls {
		t.Errorf("parallel_tool_calls 기본 true 여야 함")
	}
	if out.PromptCacheKey != "sess-1" {
		t.Errorf("prompt_cache_key 누락")
	}
	if len(out.Tools) != 1 || out.Tools[0].Type != "function" || out.Tools[0].Name != "Bash" {
		t.Errorf("tools 매핑 잘못됨: %+v", out.Tools)
	}
	if string(out.ToolChoice) != `"auto"` {
		t.Errorf("tool_choice 기본값 'auto' 여야 함, got %s", out.ToolChoice)
	}
	if out.Text == nil || out.Text.Verbosity != "low" {
		t.Errorf("text.verbosity=low 기본값 누락")
	}
}

func TestTranslateRequest_EffortAndJsonSchema(t *testing.T) {
	req := &AnthropicRequest{
		Model:    "gpt-5.4",
		Messages: []AnthropicMessage{{Role: "user", Content: json.RawMessage(`"x"`)}},
		OutputConfig: &AnthropicOutputConfig{
			Effort: "max",
			Format: &AnthropicOutputFormatSchema{
				Type:   "json_schema",
				Name:   "Out",
				Schema: json.RawMessage(`{"type":"object"}`),
			},
		},
	}
	out, err := TranslateRequest(req, TranslateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Reasoning == nil || out.Reasoning.Effort != "xhigh" {
		t.Errorf("max → xhigh 매핑 실패: %+v", out.Reasoning)
	}
	if len(out.Include) == 0 || out.Include[0] != "reasoning.encrypted_content" {
		t.Errorf("reasoning include 누락")
	}
	if out.Text.Format == nil || out.Text.Format.Type != "json_schema" || !out.Text.Format.Strict {
		t.Errorf("json_schema format 매핑 실패: %+v", out.Text.Format)
	}
}

func TestTranslateRequest_InvalidEffortRejected(t *testing.T) {
	req := &AnthropicRequest{
		Model:        "x",
		Messages:     []AnthropicMessage{},
		OutputConfig: &AnthropicOutputConfig{Effort: "extreme"},
	}
	if _, err := TranslateRequest(req, TranslateOptions{}); err == nil {
		t.Error("invalid effort에 에러 반환 안됨")
	}
}

func TestTranslateRequest_OverrideEffort(t *testing.T) {
	req := &AnthropicRequest{
		Model:    "x",
		Messages: []AnthropicMessage{},
	}
	out, err := TranslateRequest(req, TranslateOptions{EffortOverride: "high"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Reasoning == nil || out.Reasoning.Effort != "high" {
		t.Errorf("override 미적용: %+v", out.Reasoning)
	}
}
