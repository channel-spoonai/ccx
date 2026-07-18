package openaichat

import (
	"encoding/json"
	"fmt"
	"io"
)

// AccumulateOptions는 비스트리밍 응답 변환의 외부 입력.
type AccumulateOptions struct {
	// MessageID가 비어있으면 OpenAI 응답의 id를 그대로 사용.
	// 비어있지 않으면 그 값으로 override.
	MessageID string
	// Model이 비어있으면 OpenAI 응답의 model을 그대로 사용.
	Model string
}

// AccumulateResponse는 OpenAI Chat Completions의 단일 JSON 응답 본문을 읽어
// Anthropic Messages 응답으로 변환한다.
func AccumulateResponse(body io.Reader, opts AccumulateOptions) (*AnthropicResponse, error) {
	var raw ChatResponse
	if err := json.NewDecoder(body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("failed to decode upstream JSON: %w", err)
	}
	if len(raw.Choices) == 0 {
		return nil, fmt.Errorf("upstream returned no choices")
	}
	choice := raw.Choices[0]

	resp := &AnthropicResponse{
		Type:       "message",
		Role:       "assistant",
		ID:         opts.MessageID,
		Model:      opts.Model,
		StopReason: mapFinishReason(choice.FinishReason, len(choice.Message.ToolCalls) > 0),
	}
	if resp.ID == "" {
		resp.ID = raw.ID
	}
	if resp.Model == "" {
		resp.Model = raw.Model
	}

	if choice.Message.Content != "" {
		resp.Content = append(resp.Content, AnthropicResponseBlock{
			Type: "text",
			Text: choice.Message.Content,
		})
	}
	for _, tc := range choice.Message.ToolCalls {
		input := json.RawMessage(tc.Function.Arguments)
		if len(input) == 0 || string(input) == "" {
			input = json.RawMessage("{}")
		}
		// arguments가 유효한 JSON이 아니면 string으로 감싸 보존한다.
		// (정상적인 OpenAI 응답에서는 발생하지 않지만 일부 호환 서버가 raw string을 보낼 수 있음)
		var probe any
		if err := json.Unmarshal(input, &probe); err != nil {
			input, _ = json.Marshal(string(tc.Function.Arguments))
		}
		resp.Content = append(resp.Content, AnthropicResponseBlock{
			Type:  "tool_use",
			ID:    tc.ID,
			Name:  tc.Function.Name,
			Input: input,
		})
	}

	// Anthropic 응답은 content가 비어있지 않아야 한다 — 모델이 정말 빈 응답을 반환하면
	// 빈 text 블록을 채운다.
	if len(resp.Content) == 0 {
		resp.Content = []AnthropicResponseBlock{{Type: "text", Text: ""}}
	}

	if raw.Usage != nil {
		resp.Usage = AnthropicResponseUsage{
			InputTokens:  raw.Usage.PromptTokens,
			OutputTokens: raw.Usage.CompletionTokens,
		}
	}
	return resp, nil
}

// mapFinishReason은 OpenAI finish_reason을 Anthropic stop_reason으로 매핑한다.
//
//	stop / null     → end_turn
//	length          → max_tokens
//	tool_calls      → tool_use
//	function_call   → tool_use (legacy)
//	content_filter  → end_turn (보존할 더 좋은 라벨이 없음)
//
// hasToolCalls는 finish_reason이 비어있고 메시지에 tool_calls가 있을 때 tool_use로 보정하기 위함.
func mapFinishReason(reason string, hasToolCalls bool) string {
	switch reason {
	case "length":
		return "max_tokens"
	case "tool_calls", "function_call":
		return "tool_use"
	case "content_filter":
		return "end_turn"
	case "stop":
		return "end_turn"
	case "":
		if hasToolCalls {
			return "tool_use"
		}
		return "end_turn"
	default:
		return "end_turn"
	}
}
