// Package codex는 Anthropic Messages API와 OpenAI Codex Responses API 간 변환을 한다.
//
// 입력 측(Anthropic /v1/messages)은 Claude Code가 보내는 요청을 그대로 받고,
// 출력 측(Codex Responses)은 chatgpt.com/backend-api/codex/responses 가 기대하는
// 페이로드로 만든다. 응답은 SSE로 받아 Anthropic SSE 이벤트로 다시 풀어준다.
//
// 변환 규칙은 raine/claude-code-proxy의 src/providers/codex/translate/* 와
// src/anthropic/schema.ts 의 검증된 매핑을 그대로 따른다 — OpenAI가 비공식
// 클라이언트를 차단하기 시작하면 일치된 페이로드 모양만이 살아남으므로
// 자체 변형을 가하지 않는다.
package codex

import "encoding/json"

// ImageSource는 Anthropic image 블록의 source.
type ImageSource struct {
	Type      string `json:"type"`                 // "base64" 또는 "url"
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
	URL       string `json:"url,omitempty"`
}

// AnthropicBlock은 Anthropic content block의 fat-union 표현.
// Go의 json 디코더가 tagged union을 직접 지원하지 않으므로 모든 필드를 옵셔널로 두고
// Type 필드로 디스패치한다.
type AnthropicBlock struct {
	Type string `json:"type"`

	// text
	Text string `json:"text,omitempty"`

	// image
	Source *ImageSource `json:"source,omitempty"`

	// tool_use
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`

	// tool_result — content는 string 또는 []block
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`

	// thinking
	Thinking  string `json:"thinking,omitempty"`
	Signature string `json:"signature,omitempty"`

	// 통과 필드 — 직접 사용하지 않고 strip 대상.
	CacheControl json.RawMessage `json:"cache_control,omitempty"`
}

// AnthropicMessage 의 content는 string 또는 []AnthropicBlock.
type AnthropicMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// AnthropicTool은 도구 정의.
type AnthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// AnthropicToolChoice는 tool_choice 옵션.
type AnthropicToolChoice struct {
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
}

// AnthropicRequest는 /v1/messages 요청 페이로드.
type AnthropicRequest struct {
	Model       string               `json:"model"`
	Messages    []AnthropicMessage   `json:"messages"`
	System      json.RawMessage      `json:"system,omitempty"` // string 또는 []AnthropicBlock
	Tools       []AnthropicTool      `json:"tools,omitempty"`
	ToolChoice  *AnthropicToolChoice `json:"tool_choice,omitempty"`
	MaxTokens   int                  `json:"max_tokens,omitempty"`
	Temperature *float64             `json:"temperature,omitempty"`
	TopP        *float64             `json:"top_p,omitempty"`
	Stream      bool                 `json:"stream,omitempty"`

	// thinking, output_config 같은 Anthropic 전용 필드는 strip — 우리가 매핑 불가.
	// metadata도 codex 측에 직접 매핑 안 됨.
	OutputConfig *AnthropicOutputConfig `json:"output_config,omitempty"`
}

// AnthropicOutputConfig 는 effort, json_schema 같은 출력 형식 힌트.
type AnthropicOutputConfig struct {
	Effort string                       `json:"effort,omitempty"`
	Format *AnthropicOutputFormatSchema `json:"format,omitempty"`
}

type AnthropicOutputFormatSchema struct {
	Type   string          `json:"type"`
	Schema json.RawMessage `json:"schema,omitempty"`
	Name   string          `json:"name,omitempty"`
	Strict bool            `json:"strict,omitempty"`
}

// === Codex Responses API 페이로드 ===

// ResponsesContentPart 는 input_text/output_text/input_image 의 fat-union.
type ResponsesContentPart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
}

// ResponsesInputItem 도 fat-union: message / function_call / function_call_output.
type ResponsesInputItem struct {
	Type    string                 `json:"type"`
	Role    string                 `json:"role,omitempty"`
	Content []ResponsesContentPart `json:"content,omitempty"`

	// function_call
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`

	// function_call_output
	Output string `json:"output,omitempty"`
}

// ResponsesTool 은 함수 도구.
type ResponsesTool struct {
	Type        string          `json:"type"` // 항상 "function"
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
	Strict      bool            `json:"strict,omitempty"`
}

// ResponsesText 는 verbosity / format 힌트.
type ResponsesText struct {
	Verbosity string                 `json:"verbosity,omitempty"`
	Format    *ResponsesTextFormat   `json:"format,omitempty"`
}

type ResponsesTextFormat struct {
	Type   string          `json:"type"` // "text" / "json_object" / "json_schema"
	Name   string          `json:"name,omitempty"`
	Schema json.RawMessage `json:"schema,omitempty"`
	Strict bool            `json:"strict,omitempty"`
}

// ResponsesReasoning — effort 등.
type ResponsesReasoning struct {
	Effort string `json:"effort,omitempty"`
}

// ResponsesToolChoiceFunc 는 function 강제 지정.
type ResponsesToolChoiceFunc struct {
	Type string `json:"type"` // "function"
	Name string `json:"name"`
}

// ResponsesRequest는 chatgpt.com/backend-api/codex/responses 요청 본체.
// 필드 추가는 raine/upstream Codex 코드에 정확한 근거가 있을 때만 — 추측으로
// 필드를 더하면 OpenAI가 즉시 거부할 수 있다.
type ResponsesRequest struct {
	Model              string                  `json:"model"`
	Instructions       string                  `json:"instructions,omitempty"`
	Input              []ResponsesInputItem    `json:"input"`
	Tools              []ResponsesTool         `json:"tools,omitempty"`
	ToolChoice         json.RawMessage         `json:"tool_choice,omitempty"` // string 또는 {type,name}
	ParallelToolCalls  bool                    `json:"parallel_tool_calls"`
	Reasoning          *ResponsesReasoning     `json:"reasoning,omitempty"`
	Store              bool                    `json:"store"`
	Stream             bool                    `json:"stream"`
	Include            []string                `json:"include,omitempty"`
	PromptCacheKey     string                  `json:"prompt_cache_key,omitempty"`
	Text               *ResponsesText          `json:"text,omitempty"`
}
