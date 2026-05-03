package codex

import (
	"encoding/json"
	"fmt"
	"strings"
)

// TranslateOptions는 변환에 영향을 주는 외부 컨텍스트.
type TranslateOptions struct {
	// SessionID는 Codex의 prompt_cache_key로 전달된다.
	// 같은 세션 내 반복 요청에서 캐시 히트를 노릴 때 사용.
	SessionID string

	// EffortOverride가 비어있지 않으면 요청의 effort를 무시하고 이 값을 사용.
	// 환경변수 CCX_CODEX_EFFORT 같은 외부 입력을 받아 넣는다.
	EffortOverride string
}

var validAnthropicEfforts = map[string]struct{}{"low": {}, "medium": {}, "high": {}, "max": {}}
var validCodexEfforts = map[string]struct{}{"none": {}, "low": {}, "medium": {}, "high": {}, "xhigh": {}}

// TranslateRequest는 Anthropic 요청을 Codex Responses 요청으로 변환한다.
func TranslateRequest(req *AnthropicRequest, opts TranslateOptions) (*ResponsesRequest, error) {
	instructions, err := buildInstructions(req.System)
	if err != nil {
		return nil, fmt.Errorf("system 변환 실패: %w", err)
	}

	input, err := buildInput(req.Messages)
	if err != nil {
		return nil, fmt.Errorf("messages 변환 실패: %w", err)
	}

	var tools []ResponsesTool
	for _, t := range req.Tools {
		tools = append(tools, ResponsesTool{
			Type:        "function",
			Name:        t.Name,
			Description: t.Description,
			Parameters:  t.InputSchema,
		})
	}

	tc, err := mapToolChoice(req.ToolChoice)
	if err != nil {
		return nil, err
	}

	out := &ResponsesRequest{
		Model:             req.Model,
		Input:             input,
		Tools:             tools,
		ToolChoice:        tc,
		ParallelToolCalls: true,
		Store:             false,
		Stream:            true, // 우리는 항상 SSE로 받아 변환한다 — Claude Code 측 stream 여부와 무관.
		Text:              &ResponsesText{Verbosity: "low"},
	}
	if instructions != "" {
		out.Instructions = instructions
	}
	if opts.SessionID != "" {
		out.PromptCacheKey = opts.SessionID
	}

	if req.OutputConfig != nil {
		if f := req.OutputConfig.Format; f != nil && f.Type == "json_schema" {
			name := f.Name
			if name == "" {
				name = "response"
			}
			out.Text.Format = &ResponsesTextFormat{
				Type:   "json_schema",
				Name:   name,
				Schema: f.Schema,
				Strict: true,
			}
		}
		effort, err := resolveEffort(req.OutputConfig.Effort, opts.EffortOverride)
		if err != nil {
			return nil, err
		}
		if effort != "" {
			out.Reasoning = &ResponsesReasoning{Effort: effort}
			out.Include = []string{"reasoning.encrypted_content"}
		}
	} else if opts.EffortOverride != "" {
		effort, err := resolveEffort("", opts.EffortOverride)
		if err != nil {
			return nil, err
		}
		if effort != "" {
			out.Reasoning = &ResponsesReasoning{Effort: effort}
			out.Include = []string{"reasoning.encrypted_content"}
		}
	}

	return out, nil
}

func resolveEffort(anthropicEffort, override string) (string, error) {
	if anthropicEffort != "" {
		if _, ok := validAnthropicEfforts[anthropicEffort]; !ok {
			return "", fmt.Errorf(`output_config.effort 값이 잘못됨: %q (허용: low/medium/high/max)`, anthropicEffort)
		}
	}
	codexEffort := anthropicEffort
	if codexEffort == "max" {
		codexEffort = "xhigh"
	}
	if override != "" {
		if _, ok := validCodexEfforts[override]; !ok {
			return "", fmt.Errorf(`effort override 값이 잘못됨: %q (허용: none/low/medium/high/xhigh)`, override)
		}
		codexEffort = override
	}
	return codexEffort, nil
}

func mapToolChoice(c *AnthropicToolChoice) (json.RawMessage, error) {
	if c == nil {
		return json.RawMessage(`"auto"`), nil
	}
	switch c.Type {
	case "auto":
		return json.RawMessage(`"auto"`), nil
	case "none":
		return json.RawMessage(`"none"`), nil
	case "any":
		return json.RawMessage(`"required"`), nil
	case "tool":
		if c.Name != "" {
			return json.Marshal(ResponsesToolChoiceFunc{Type: "function", Name: c.Name})
		}
		return json.RawMessage(`"required"`), nil
	default:
		return nil, fmt.Errorf("알 수 없는 tool_choice.type: %q", c.Type)
	}
}

// buildInstructions은 system 필드를 합쳐 instructions 문자열로 만든다.
// "x-anthropic-billing-header:" 로 시작하는 텍스트는 strip — Anthropic 빌링 메타이지
// 모델이 봐선 안 됨 (raine과 동일).
func buildInstructions(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}
	// string 형태?
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if strings.HasPrefix(s, "x-anthropic-billing-header:") {
			return "", nil
		}
		return s, nil
	}
	// 또는 []AnthropicBlock 형태.
	var blocks []AnthropicBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return "", fmt.Errorf("system 형식 인식 실패: %w", err)
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

// normalizeContent는 message.content가 문자열이면 단일 text 블록으로 감싼다.
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
		return nil, fmt.Errorf("content 형식 인식 실패: %w", err)
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

// toolResultToString은 tool_result.content를 평문 문자열로 만든다.
// 이미지가 들어있으면 placeholder로 치환 — Codex 백엔드가 tool_result 내 이미지를
// 거부하므로 raine과 동일한 회피책.
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
		return "", fmt.Errorf("tool_result.content 인식 실패: %w", err)
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

// buildInput은 Anthropic messages 배열을 Codex input 배열로 풀어쓴다.
//
// user 메시지: text/image 블록은 한 message 항목으로 모이고, tool_result 가 끼어들면
// 그 시점까지의 텍스트를 flush한 뒤 별도의 function_call_output 항목을 끼워넣는다.
//
// assistant 메시지: text 와 tool_use가 섞여 올 수 있으므로 순서를 보존해
// tool_use를 만나면 그 전까지의 텍스트를 flush하고 function_call 항목을 추가한다.
func buildInput(messages []AnthropicMessage) ([]ResponsesInputItem, error) {
	var out []ResponsesInputItem
	for _, msg := range messages {
		blocks, err := normalizeContent(msg.Content)
		if err != nil {
			return nil, err
		}
		switch msg.Role {
		case "user":
			var parts []ResponsesContentPart
			flushUser := func() {
				if len(parts) == 0 {
					return
				}
				out = append(out, ResponsesInputItem{Type: "message", Role: "user", Content: parts})
				parts = nil
			}
			for _, b := range blocks {
				switch b.Type {
				case "text":
					parts = append(parts, ResponsesContentPart{Type: "input_text", Text: b.Text})
				case "image":
					parts = append(parts, ResponsesContentPart{Type: "input_image", ImageURL: imageToURL(b.Source)})
				case "tool_result":
					flushUser()
					body, err := toolResultToString(b.Content)
					if err != nil {
						return nil, err
					}
					if b.IsError {
						body = "[tool execution error]\n" + body
					}
					out = append(out, ResponsesInputItem{
						Type:   "function_call_output",
						CallID: b.ToolUseID,
						Output: body,
					})
				}
				// thinking, 기타는 무시.
			}
			flushUser()

		case "assistant":
			var textParts []ResponsesContentPart
			flushAssistant := func() {
				if len(textParts) == 0 {
					return
				}
				out = append(out, ResponsesInputItem{Type: "message", Role: "assistant", Content: textParts})
				textParts = nil
			}
			for _, b := range blocks {
				switch b.Type {
				case "text":
					textParts = append(textParts, ResponsesContentPart{Type: "output_text", Text: b.Text})
				case "tool_use":
					flushAssistant()
					args := string(b.Input)
					if args == "" || args == "null" {
						args = "{}"
					}
					out = append(out, ResponsesInputItem{
						Type:      "function_call",
						CallID:    b.ID,
						Name:      b.Name,
						Arguments: args,
					})
				}
			}
			flushAssistant()
		}
	}
	return out, nil
}
