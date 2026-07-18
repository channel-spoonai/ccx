package codex

import (
	"errors"
	"io"
)

// StreamOptions는 Anthropic SSE 변환에 필요한 메타.
type StreamOptions struct {
	MessageID string
	Model     string
}

// FinishCallback은 스트림이 정상 종료될 때 호출된다.
type FinishCallback func(stop StopReason, usage *CodexUsage)

// TranslateStream는 Codex SSE를 읽어 Anthropic SSE 이벤트를 w로 흘려쓴다.
//
// HTTP 200 응답을 이미 보낸 뒤 호출하므로, rate-limit/실패는 응답 SSE 안의
// `error` 이벤트로 surface된다 (라인 형식은 raine과 동일하게 Anthropic api_error/rate_limit_error).
//
// onFinish는 finish 이벤트 직전에 호출 — 호출자가 usage 같은 통계를 외부 로그에 남기기 위해.
func TranslateStream(upstream io.Reader, w io.Writer, opts StreamOptions, onFinish FinishCallback) error {
	flusher := wrapFlush(w)

	emit := func(event string, data any) bool {
		buf, err := EncodeSSE(event, data)
		if err != nil {
			return false
		}
		if _, err := w.Write(buf); err != nil {
			return false
		}
		flusher()
		return true
	}

	messageStarted := false
	ensureMessageStart := func() bool {
		if messageStarted {
			return true
		}
		messageStarted = true
		ok := emit("message_start", map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id":            opts.MessageID,
				"type":          "message",
				"role":          "assistant",
				"model":         opts.Model,
				"content":       []any{},
				"stop_reason":   nil,
				"stop_sequence": nil,
				"usage": AnthropicUsage{},
			},
		})
		if !ok {
			return false
		}
		return emit("ping", map[string]any{"type": "ping"})
	}

	activeTools := map[int]struct{ id, name string }{}

	err := ReduceUpstream(upstream, func(e ReducerEvent) bool {
		switch e.Kind {
		case EventTextStart:
			if !ensureMessageStart() {
				return false
			}
			return emit("content_block_start", map[string]any{
				"type":          "content_block_start",
				"index":         e.Index,
				"content_block": map[string]any{"type": "text", "text": ""},
			})
		case EventTextDelta:
			return emit("content_block_delta", map[string]any{
				"type":  "content_block_delta",
				"index": e.Index,
				"delta": map[string]any{"type": "text_delta", "text": e.Text},
			})
		case EventTextStop:
			return emit("content_block_stop", map[string]any{
				"type":  "content_block_stop",
				"index": e.Index,
			})
		case EventToolStart:
			activeTools[e.Index] = struct{ id, name string }{e.ToolID, e.ToolName}
			if !ensureMessageStart() {
				return false
			}
			return emit("content_block_start", map[string]any{
				"type":  "content_block_start",
				"index": e.Index,
				"content_block": map[string]any{
					"type":  "tool_use",
					"id":    e.ToolID,
					"name":  e.ToolName,
					"input": map[string]any{},
				},
			})
		case EventToolDelta:
			return emit("content_block_delta", map[string]any{
				"type":  "content_block_delta",
				"index": e.Index,
				"delta": map[string]any{
					"type":         "input_json_delta",
					"partial_json": e.PartialJSON,
				},
			})
		case EventToolStop:
			delete(activeTools, e.Index)
			return emit("content_block_stop", map[string]any{
				"type":  "content_block_stop",
				"index": e.Index,
			})
		case EventFinish:
			if !ensureMessageStart() {
				return false
			}
			if onFinish != nil {
				onFinish(e.StopReason, e.Usage)
			}
			if !emit("message_delta", map[string]any{
				"type":  "message_delta",
				"delta": map[string]any{"stop_reason": e.StopReason, "stop_sequence": nil},
				"usage": MapUsage(e.Usage),
			}) {
				return false
			}
			return emit("message_stop", map[string]any{"type": "message_stop"})
		}
		return true
	})

	if err != nil {
		var upErr *UpstreamError
		if errors.As(err, &upErr) {
			ensureMessageStart()
			errType := "api_error"
			if upErr.Kind == ErrorRateLimit {
				errType = "rate_limit_error"
			}
			emit("error", map[string]any{
				"type": "error",
				"error": map[string]any{
					"type":    errType,
					"message": upErr.Message,
				},
			})
			return nil
		}
		ensureMessageStart()
		emit("error", map[string]any{
			"type": "error",
			"error": map[string]any{"type": "api_error", "message": err.Error()},
		})
		return nil
	}
	return nil
}

// wrapFlush는 w가 http.Flusher라면 매번 flush하도록 한다.
type flusher interface{ Flush() }

func wrapFlush(w io.Writer) func() {
	if f, ok := w.(flusher); ok {
		return f.Flush
	}
	return func() {}
}
