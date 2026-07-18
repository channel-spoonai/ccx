package codex

import (
	"encoding/json"
	"errors"
	"io"
)

// AnthropicNonStreamResponse는 비스트리밍 /v1/messages 응답.
type AnthropicNonStreamResponse struct {
	ID           string         `json:"id"`
	Type         string         `json:"type"`
	Role         string         `json:"role"`
	Model        string         `json:"model"`
	Content      []ContentOut   `json:"content"`
	StopReason   StopReason     `json:"stop_reason,omitempty"`
	StopSequence any            `json:"stop_sequence"`
	Usage        AnthropicUsage `json:"usage"`
}

// ContentOut은 응답에 들어가는 text 또는 tool_use 블록.
type ContentOut struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

// AccumulateOptions는 비스트리밍 응답을 만들 때 필요한 메타.
type AccumulateOptions struct {
	MessageID string
	Model     string
}

// AccumulateResponse는 Codex SSE를 끝까지 소비해 단일 Anthropic 응답으로 접는다.
// 비스트리밍 클라이언트(Claude Code가 stream:false로 보내는 드문 경우)용.
func AccumulateResponse(upstream io.Reader, opts AccumulateOptions) (*AnthropicNonStreamResponse, error) {
	type block struct {
		isText bool
		text   string
		id     string
		name   string
		args   string
	}
	ordered := []int{}
	blocks := map[int]*block{}
	var stop StopReason
	var usage AnthropicUsage

	err := ReduceUpstream(upstream, func(e ReducerEvent) bool {
		switch e.Kind {
		case EventTextStart:
			blocks[e.Index] = &block{isText: true}
			ordered = append(ordered, e.Index)
		case EventTextDelta:
			if b, ok := blocks[e.Index]; ok && b.isText {
				b.text += e.Text
			}
		case EventToolStart:
			blocks[e.Index] = &block{id: e.ToolID, name: e.ToolName}
			ordered = append(ordered, e.Index)
		case EventToolDelta:
			if b, ok := blocks[e.Index]; ok && !b.isText {
				b.args += e.PartialJSON
			}
		case EventTextStop, EventToolStop:
			// no-op
		case EventFinish:
			stop = e.StopReason
			usage = MapUsage(e.Usage)
		}
		return true
	})
	if err != nil {
		var up *UpstreamError
		if errors.As(err, &up) {
			return nil, up
		}
		return nil, err
	}

	out := &AnthropicNonStreamResponse{
		ID:         opts.MessageID,
		Type:       "message",
		Role:       "assistant",
		Model:      opts.Model,
		StopReason: stop,
		Usage:      usage,
	}
	for _, i := range ordered {
		b, ok := blocks[i]
		if !ok {
			continue
		}
		if b.isText {
			if b.text != "" {
				out.Content = append(out.Content, ContentOut{Type: "text", Text: b.text})
			}
			continue
		}
		input := json.RawMessage("{}")
		if b.args != "" {
			// 검증: 유효한 JSON이면 그대로, 아니면 _raw 래핑.
			if json.Valid([]byte(b.args)) {
				input = json.RawMessage(b.args)
			} else {
				wrapped, _ := json.Marshal(map[string]string{"_raw": b.args})
				input = wrapped
			}
		}
		out.Content = append(out.Content, ContentOut{
			Type:  "tool_use",
			ID:    b.id,
			Name:  b.name,
			Input: input,
		})
	}
	return out, nil
}
