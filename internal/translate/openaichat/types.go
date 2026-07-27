// Package openaichat는 Anthropic Messages API와 OpenAI Chat Completions API 사이의 변환을 담당한다.
//
// 입력 측(Anthropic /v1/messages)은 Claude Code가 보내는 요청을 그대로 받고,
// 출력 측(OpenAI /v1/chat/completions)은 표준 OpenAI Chat Completions 페이로드를 보낸다.
// 응답은 SSE 또는 단일 JSON으로 받아 다시 Anthropic 포맷으로 풀어준다.
//
// 변환 규칙은 OpenAI 공식 스펙을 따른다 — lightning-mlx, vLLM, LocalAI 등 표준 OpenAI 호환 서버는
// 동일한 페이로드 모양을 기대한다.
package openaichat

import "encoding/json"

// === Anthropic 입력 측 타입 (codex/types.go 와 1:1) ===
//
// codex 패키지의 타입을 그대로 재사용하지 않고 자체적으로 둔 이유:
// 두 어댑터가 독립적으로 진화할 수 있어야 하고 (예: Anthropic이 새 블록 타입을 추가했을 때
// 한 쪽만 먼저 지원할 수 있음), import 사이클 위험도 피한다.

// ImageSource는 Anthropic image 블록의 source.
type ImageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
	URL       string `json:"url,omitempty"`
}

// AnthropicBlock은 Anthropic content block의 fat-union.
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

	// tool_result
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`

	// thinking — strip
	Thinking  string `json:"thinking,omitempty"`
	Signature string `json:"signature,omitempty"`

	CacheControl json.RawMessage `json:"cache_control,omitempty"`
}

type AnthropicMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type AnthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type AnthropicToolChoice struct {
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
}

type AnthropicRequest struct {
	Model         string               `json:"model"`
	Messages      []AnthropicMessage   `json:"messages"`
	System        json.RawMessage      `json:"system,omitempty"`
	Tools         []AnthropicTool      `json:"tools,omitempty"`
	ToolChoice    *AnthropicToolChoice `json:"tool_choice,omitempty"`
	MaxTokens     int                  `json:"max_tokens,omitempty"`
	Temperature   *float64             `json:"temperature,omitempty"`
	TopP          *float64             `json:"top_p,omitempty"`
	StopSequences []string             `json:"stop_sequences,omitempty"`
	Stream        bool                 `json:"stream,omitempty"`
}

// === OpenAI Chat Completions 출력 측 타입 ===

// ChatContentPart는 multimodal content block.
//   - {"type":"text","text":"..."}
//   - {"type":"image_url","image_url":{"url":"data:..."}}
type ChatContentPart struct {
	Type     string        `json:"type"`
	Text     string        `json:"text,omitempty"`
	ImageURL *ChatImageURL `json:"image_url,omitempty"`
}

type ChatImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

// ChatToolCall은 assistant 메시지가 발행하는 함수 호출.
type ChatToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"` // 항상 "function"
	Function ChatFunctionCall `json:"function"`
}

type ChatFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON 문자열 (object가 아님)
}

// ChatMessage는 chat completions 의 message 항목.
// content는 string 또는 []ChatContentPart 둘 다 가능하므로 json.RawMessage로 둔다.
type ChatMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content,omitempty"`
	Name       string          `json:"name,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
	ToolCalls  []ChatToolCall  `json:"tool_calls,omitempty"`
}

// ChatToolDef는 tools 필드의 각 항목.
type ChatToolDef struct {
	Type     string             `json:"type"` // "function"
	Function ChatToolDefDetails `json:"function"`
}

type ChatToolDefDetails struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// ChatRequest는 /v1/chat/completions 요청 페이로드.
//
// 표준 OpenAI 필드만 포함. provider-specific 확장(예: enable_thinking, video_fps)은
// 의도적으로 생략 — 변환기는 일반 OpenAI 스펙만 따르고 lightning-mlx의 비표준 옵션은
// profile env로 직접 주입할 수 있게 한다.
type ChatRequest struct {
	Model         string           `json:"model"`
	Messages      []ChatMessage    `json:"messages"`
	Tools         []ChatToolDef    `json:"tools,omitempty"`
	ToolChoice    json.RawMessage  `json:"tool_choice,omitempty"` // string 또는 {type,function:{name}}
	MaxTokens     int              `json:"max_tokens,omitempty"`
	Temperature   *float64         `json:"temperature,omitempty"`
	TopP          *float64         `json:"top_p,omitempty"`
	Stop          []string         `json:"stop,omitempty"`
	Stream        bool             `json:"stream,omitempty"`
	StreamOptions *ChatStreamOpts  `json:"stream_options,omitempty"`

	// EnableThinking은 OpenAI 표준 외 확장. lightning-mlx(rapid-mlx) / vLLM의 일부 reasoning 모델은
	// 이 플래그로 chain-of-thought("reasoning_content")를 활성화·비활성화한다.
	// false로 보내면 reasoning을 SKIP하고 content에 토큰 예산을 모두 쓴다 — Claude Code는
	// Anthropic 자체 thinking 메커니즘을 쓰므로 OpenAI 측 reasoning_content를 알아듣지 못한다.
	// 이 플래그를 모르는 서버는 무시하므로 안전하게 항상 false로 보낸다.
	EnableThinking *bool `json:"enable_thinking,omitempty"`
}

type ChatStreamOpts struct {
	IncludeUsage bool `json:"include_usage,omitempty"`
}

// === OpenAI Chat Completions 응답 측 타입 (non-streaming) ===

type ChatResponse struct {
	ID      string             `json:"id"`
	Object  string             `json:"object"`
	Created int64              `json:"created"`
	Model   string             `json:"model"`
	Choices []ChatChoice       `json:"choices"`
	Usage   *ChatUsage         `json:"usage,omitempty"`
}

type ChatChoice struct {
	Index        int             `json:"index"`
	Message      ChatRespMessage `json:"message"`
	FinishReason string          `json:"finish_reason"`
}

type ChatRespMessage struct {
	Role      string         `json:"role"`
	Content   string         `json:"content,omitempty"`
	ToolCalls []ChatToolCall `json:"tool_calls,omitempty"`
}

type ChatUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// === SSE chunk 형식 ===

type ChatStreamChunk struct {
	ID      string             `json:"id"`
	Object  string             `json:"object"`
	Created int64              `json:"created"`
	Model   string             `json:"model"`
	Choices []ChatStreamChoice `json:"choices"`
	Usage   *ChatUsage         `json:"usage,omitempty"`
}

type ChatStreamChoice struct {
	Index        int                `json:"index"`
	Delta        ChatStreamDelta    `json:"delta"`
	FinishReason string             `json:"finish_reason,omitempty"`
}

type ChatStreamDelta struct {
	Role      string                  `json:"role,omitempty"`
	Content   string                  `json:"content,omitempty"`
	ToolCalls []ChatStreamToolCallDelta `json:"tool_calls,omitempty"`
}

// ChatStreamToolCallDelta는 streaming 중 tool_call 조각.
// 단일 청크에 일부 필드만 들어올 수 있어 모두 옵셔널.
type ChatStreamToolCallDelta struct {
	Index    int                       `json:"index"`
	ID       string                    `json:"id,omitempty"`
	Type     string                    `json:"type,omitempty"`
	Function *ChatStreamFunctionDelta  `json:"function,omitempty"`
}

type ChatStreamFunctionDelta struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// === Anthropic 출력 측 타입 (응답 반환용) ===

// AnthropicResponseBlock은 변환 결과 응답의 content 블록.
type AnthropicResponseBlock struct {
	Type string `json:"type"`

	// text
	Text string `json:"text,omitempty"`

	// tool_use
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

// AnthropicResponseUsage 는 사용량 (Anthropic 스타일).
type AnthropicResponseUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// AnthropicResponse는 비스트리밍 변환 결과.
type AnthropicResponse struct {
	ID         string                   `json:"id"`
	Type       string                   `json:"type"`  // "message"
	Role       string                   `json:"role"`  // "assistant"
	Model      string                   `json:"model"`
	Content    []AnthropicResponseBlock `json:"content"`
	StopReason string                   `json:"stop_reason"` // end_turn / max_tokens / tool_use / stop_sequence
	Usage      AnthropicResponseUsage   `json:"usage"`
}
