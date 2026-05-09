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
		t.Errorf("got %q (billing header was not stripped)", got)
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
		t.Errorf("user content mapping wrong: %+v", got[0].Content)
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
		t.Fatalf("expected 3 split results, got %d (%+v)", len(got), got)
	}
	if got[0].Type != "message" || got[0].Content[0].Text != "before" {
		t.Errorf("first message wrong: %+v", got[0])
	}
	if got[1].Type != "function_call_output" || got[1].CallID != "call_1" || got[1].Output != "result body" {
		t.Errorf("function_call_output mapping wrong: %+v", got[1])
	}
	if got[2].Type != "message" || got[2].Content[0].Text != "after" {
		t.Errorf("trailing message wrong: %+v", got[2])
	}
}

func TestBuildInput_ToolResultErrorPrefix(t *testing.T) {
	content := mustJSON(t, `[
		{"type":"tool_result","tool_use_id":"c","content":"oops","is_error":true}
	]`)
	got, _ := buildInput([]AnthropicMessage{{Role: "user", Content: content}})
	if len(got) != 1 || !strings.Contains(got[0].Output, "[tool execution error]") {
		t.Errorf("error prefix missing: %+v", got)
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
		t.Errorf("image placeholder missing: %q", got[0].Output)
	}
	if !strings.Contains(got[0].Output, "see") {
		t.Errorf("text portion missing: %q", got[0].Output)
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
		t.Fatalf("got %d, want 3 (text → function_call → text order preserved)", len(got))
	}
	if got[0].Role != "assistant" || got[0].Content[0].Type != "output_text" {
		t.Errorf("assistant text role/type wrong: %+v", got[0])
	}
	if got[1].Type != "function_call" || got[1].CallID != "call_42" || got[1].Name != "Bash" {
		t.Errorf("function_call mapping wrong: %+v", got[1])
	}
	if got[1].Arguments != `{"cmd":"ls"}` {
		t.Errorf("arguments JSON should be preserved verbatim: got %q", got[1].Arguments)
	}
}

func TestBuildInput_AssistantToolUseEmptyInputBecomesEmptyObject(t *testing.T) {
	content := mustJSON(t, `[{"type":"tool_use","id":"c","name":"Bash"}]`)
	got, _ := buildInput([]AnthropicMessage{{Role: "assistant", Content: content}})
	if len(got) != 1 || got[0].Arguments != "{}" {
		t.Errorf("empty input should serialize to {}: %+v", got)
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
		{"tool without name → required", &AnthropicToolChoice{Type: "tool"}, `"required"`},
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
		t.Errorf("model missing: %s", out.Model)
	}
	if out.Instructions != "be brief" {
		t.Errorf("instructions: %q", out.Instructions)
	}
	if !out.Stream || out.Store {
		t.Errorf("stream=true, store=false should be forced: got stream=%v store=%v", out.Stream, out.Store)
	}
	if !out.ParallelToolCalls {
		t.Errorf("parallel_tool_calls should default to true")
	}
	if out.PromptCacheKey != "sess-1" {
		t.Errorf("prompt_cache_key missing")
	}
	if len(out.Tools) != 1 || out.Tools[0].Type != "function" || out.Tools[0].Name != "Bash" {
		t.Errorf("tools mapping wrong: %+v", out.Tools)
	}
	if string(out.ToolChoice) != `"auto"` {
		t.Errorf("tool_choice should default to 'auto', got %s", out.ToolChoice)
	}
	if out.Text == nil || out.Text.Verbosity != "low" {
		t.Errorf("text.verbosity=low default missing")
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
		t.Errorf("max → xhigh mapping failed: %+v", out.Reasoning)
	}
	if len(out.Include) == 0 || out.Include[0] != "reasoning.encrypted_content" {
		t.Errorf("reasoning include missing")
	}
	if out.Text.Format == nil || out.Text.Format.Type != "json_schema" || !out.Text.Format.Strict {
		t.Errorf("json_schema format mapping failed: %+v", out.Text.Format)
	}
}

func TestTranslateRequest_XhighOneToOne(t *testing.T) {
	// Claude Code 5단계 중 xhigh는 Codex의 xhigh와 1:1 매핑이어야 한다.
	req := &AnthropicRequest{
		Model:        "gpt-5.5",
		Messages:     []AnthropicMessage{{Role: "user", Content: json.RawMessage(`"x"`)}},
		OutputConfig: &AnthropicOutputConfig{Effort: "xhigh"},
	}
	out, err := TranslateRequest(req, TranslateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Reasoning == nil || out.Reasoning.Effort != "xhigh" {
		t.Errorf("xhigh → xhigh mapping failed: %+v", out.Reasoning)
	}
}

func TestTranslateRequest_InvalidEffortRejected(t *testing.T) {
	req := &AnthropicRequest{
		Model:        "x",
		Messages:     []AnthropicMessage{},
		OutputConfig: &AnthropicOutputConfig{Effort: "extreme"},
	}
	if _, err := TranslateRequest(req, TranslateOptions{}); err == nil {
		t.Error("expected error for invalid effort")
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
		t.Errorf("override not applied: %+v", out.Reasoning)
	}
}
