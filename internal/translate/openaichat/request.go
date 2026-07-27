package openaichat

import (
	"encoding/json"
	"fmt"
	"strings"
)

// TranslateOptions는 변환에 영향을 주는 외부 컨텍스트.
type TranslateOptions struct {
	// IncludeUsage이 true면 stream_options.include_usage=true를 보낸다 —
	// 일부 OpenAI 호환 서버는 streaming 종료 직전에 usage 청크를 추가로 흘려보낸다.
	IncludeUsage bool

	// EnableThinking은 모델의 chain-of-thought("reasoning_content") 활성화 여부.
	// nil이면 필드를 보내지 않고, *bool 값이 있으면 ChatRequest.EnableThinking으로 그대로 전달.
	// Claude Code는 OpenAI 측 reasoning_content를 활용할 수 없으므로 false 권장.
	EnableThinking *bool
}

// TranslateRequest는 Anthropic 요청을 OpenAI Chat Completions 요청으로 변환한다.
//
// 매핑 요약:
//   - system (string|[]block) → 첫 메시지 {role:"system", content:...}
//   - user 메시지: text/image 블록을 multimodal content 배열로,
//     tool_result는 별도 {role:"tool", tool_call_id, content} 메시지로 split
//   - assistant 메시지: text와 tool_use를 한 메시지에 합쳐 {content, tool_calls}
//   - thinking 블록은 strip (OpenAI Chat Completions는 모름)
func TranslateRequest(req *AnthropicRequest, opts TranslateOptions) (*ChatRequest, error) {
	messages, err := buildMessages(req.System, req.Messages)
	if err != nil {
		return nil, fmt.Errorf("messages translation failed: %w", err)
	}

	var tools []ChatToolDef
	for _, t := range req.Tools {
		tools = append(tools, ChatToolDef{
			Type: "function",
			Function: ChatToolDefDetails{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			},
		})
	}

	tc, err := mapToolChoice(req.ToolChoice)
	if err != nil {
		return nil, err
	}

	out := &ChatRequest{
		Model:       req.Model,
		Messages:    messages,
		Tools:       tools,
		ToolChoice:  tc,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		Stop:        req.StopSequences,
		Stream:      req.Stream,
	}
	if req.Stream && opts.IncludeUsage {
		out.StreamOptions = &ChatStreamOpts{IncludeUsage: true}
	}
	if opts.EnableThinking != nil {
		out.EnableThinking = opts.EnableThinking
	}
	return out, nil
}

func mapToolChoice(c *AnthropicToolChoice) (json.RawMessage, error) {
	if c == nil {
		return nil, nil
	}
	switch c.Type {
	case "auto":
		return json.RawMessage(`"auto"`), nil
	case "none":
		return json.RawMessage(`"none"`), nil
	case "any":
		return json.RawMessage(`"required"`), nil
	case "tool":
		if c.Name == "" {
			return json.RawMessage(`"required"`), nil
		}
		return json.Marshal(map[string]any{
			"type":     "function",
			"function": map[string]string{"name": c.Name},
		})
	default:
		return nil, fmt.Errorf("unknown tool_choice.type: %q", c.Type)
	}
}

// systemToString은 system 필드(string 또는 []block)를 단일 문자열로 합친다.
// "x-anthropic-billing-header:" 로 시작하는 텍스트는 strip — 빌링 메타이지 모델이 봐선 안 됨.
func systemToString(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if strings.HasPrefix(s, "x-anthropic-billing-header:") {
			return "", nil
		}
		return s, nil
	}
	var blocks []AnthropicBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return "", fmt.Errorf("failed to recognize system format: %w", err)
	}
	var parts []string
	for _, b := range blocks {
		if b.Type != "text" || b.Text == "" {
			continue
		}
		if strings.HasPrefix(b.Text, "x-anthropic-billing-header:") {
			continue
		}
		parts = append(parts, b.Text)
	}
	return strings.Join(parts, "\n\n"), nil
}

func normalizeContent(raw json.RawMessage) ([]AnthropicBlock, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return []AnthropicBlock{{Type: "text", Text: s}}, nil
	}
	var blocks []AnthropicBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil, fmt.Errorf("failed to recognize content format: %w", err)
	}
	return blocks, nil
}

func imageToURL(src *ImageSource) string {
	if src == nil {
		return ""
	}
	if src.Type == "url" {
		return src.URL
	}
	return "data:" + src.MediaType + ";base64," + src.Data
}

// toolResultToString은 tool_result.content를 평문 문자열로 합친다.
// 일부 OpenAI 호환 서버는 tool 메시지의 content를 string 만 받아들이므로 평문화한다.
// 이미지가 들어있으면 placeholder로 치환.
func toolResultToString(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, nil
	}
	var blocks []AnthropicBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return "", fmt.Errorf("failed to recognize tool_result.content: %w", err)
	}
	var parts []string
	for _, b := range blocks {
		switch b.Type {
		case "text":
			parts = append(parts, b.Text)
		case "image":
			mt := "url"
			if b.Source != nil && b.Source.Type == "base64" {
				mt = b.Source.MediaType
			}
			parts = append(parts, "[image omitted: "+mt+"]")
		}
	}
	return strings.Join(parts, "\n"), nil
}

// buildMessages는 system + 메시지 배열을 OpenAI Chat Completions messages 배열로 풀어쓴다.
func buildMessages(system json.RawMessage, msgs []AnthropicMessage) ([]ChatMessage, error) {
	var out []ChatMessage

	if sys, err := systemToString(system); err != nil {
		return nil, err
	} else if sys != "" {
		out = append(out, ChatMessage{Role: "system", Content: jsonString(sys)})
	}

	for _, msg := range msgs {
		blocks, err := normalizeContent(msg.Content)
		if err != nil {
			return nil, err
		}
		switch msg.Role {
		case "user":
			// tool_result 블록은 별도 {role:"tool"} 메시지로 분리해야 한다.
			// text/image 블록은 하나의 user 메시지 안에 multimodal content로 모은다.
			var contentParts []ChatContentPart
			flushUser := func() {
				if len(contentParts) == 0 {
					return
				}
				out = append(out, ChatMessage{Role: "user", Content: mustMarshal(contentParts)})
				contentParts = nil
			}
			for _, b := range blocks {
				switch b.Type {
				case "text":
					contentParts = append(contentParts, ChatContentPart{Type: "text", Text: b.Text})
				case "image":
					contentParts = append(contentParts, ChatContentPart{
						Type:     "image_url",
						ImageURL: &ChatImageURL{URL: imageToURL(b.Source)},
					})
				case "tool_result":
					flushUser()
					body, err := toolResultToString(b.Content)
					if err != nil {
						return nil, err
					}
					if b.IsError {
						body = "[tool execution error]\n" + body
					}
					out = append(out, ChatMessage{
						Role:       "tool",
						ToolCallID: b.ToolUseID,
						Content:    jsonString(body),
					})
				}
				// thinking/기타 strip
			}
			// text-only user 메시지가 하나뿐이면 multimodal 배열 대신 단순 string으로 보낸다 —
			// 호환성이 가장 높은 형태. 이미지가 있을 때만 array 형태 유지.
			if len(contentParts) == 1 && contentParts[0].Type == "text" {
				out = append(out, ChatMessage{Role: "user", Content: jsonString(contentParts[0].Text)})
				contentParts = nil
			}
			flushUser()

		case "assistant":
			var textBuf strings.Builder
			var toolCalls []ChatToolCall
			for _, b := range blocks {
				switch b.Type {
				case "text":
					if textBuf.Len() > 0 {
						textBuf.WriteString("\n")
					}
					textBuf.WriteString(b.Text)
				case "tool_use":
					args := string(b.Input)
					if args == "" || args == "null" {
						args = "{}"
					}
					toolCalls = append(toolCalls, ChatToolCall{
						ID:   b.ID,
						Type: "function",
						Function: ChatFunctionCall{
							Name:      b.Name,
							Arguments: args,
						},
					})
				}
			}
			am := ChatMessage{Role: "assistant"}
			if textBuf.Len() > 0 {
				am.Content = jsonString(textBuf.String())
			}
			if len(toolCalls) > 0 {
				am.ToolCalls = toolCalls
			}
			// 둘 다 비어있으면 빈 string content를 둔다 — OpenAI 서버는 content 필수.
			if am.Content == nil && len(am.ToolCalls) == 0 {
				am.Content = jsonString("")
			}
			out = append(out, am)
		}
	}
	return out, nil
}

func jsonString(s string) json.RawMessage {
	b, _ := json.Marshal(s)
	return b
}

func mustMarshal(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		// 직렬화 실패는 사실상 발생하지 않는 케이스 — 발생하면 빈 string으로 fallback.
		return jsonString("")
	}
	return b
}
