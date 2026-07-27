package openaichat

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// StreamOptions는 SSE 변환의 외부 입력.
type StreamOptions struct {
	MessageID string // message_start 의 id
	Model     string // message_start 의 model
}

// TranslateStream은 OpenAI Chat Completions SSE 스트림을 읽어 Anthropic SSE 이벤트로 변환·write 한다.
//
// flusher가 nil이 아니면 각 이벤트 직후 flush 시도 — http.ResponseWriter가 http.Flusher 인터페이스를
// 구현하지 않으면 flusher는 nil로 전달.
//
// 변환 사이클:
//
//	OpenAI:                Anthropic:
//	-----                  ---------
//	(시작)                 message_start
//	delta.content          content_block_start(text, 0) (최초) → content_block_delta(text_delta)
//	delta.tool_calls[i]    text 열려있으면 content_block_stop → content_block_start(tool_use, n) →
//	                       content_block_delta(input_json_delta) 반복
//	finish_reason          열린 블록 모두 close → message_delta(stop_reason) → message_stop
func TranslateStream(upstream io.Reader, w io.Writer, opts StreamOptions, flusher interface{ Flush() }) error {
	st := &streamState{
		opts:            opts,
		w:               w,
		flusher:         flusher,
		toolBlockByOAI:  make(map[int]int),
		toolNameByOAI:   make(map[int]string),
		toolIDByOAI:     make(map[int]string),
	}
	if err := st.writeMessageStart(); err != nil {
		return err
	}

	scanner := bufio.NewScanner(upstream)
	// 기본 64KiB 버퍼는 큰 청크에서 모자랄 수 있어 1MiB로 키운다.
	buf := make([]byte, 0, 1024*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" {
			continue
		}
		if payload == "[DONE]" {
			break
		}
		var chunk ChatStreamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			// 알 수 없는 청크는 스킵 — 일부 서버가 비표준 keep-alive 라인을 보낼 수 있음.
			continue
		}
		if err := st.handleChunk(&chunk); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil && err != io.EOF {
		// 스캐너 에러는 부분 변환 후 발생 — 호출자가 인지할 수 있게 surface.
		_ = st.finalize("end_turn")
		return err
	}
	// finalize: 명시적 finish_reason이 안 왔으면 end_turn으로 마감.
	return st.finalize("")
}

type streamState struct {
	opts    StreamOptions
	w       io.Writer
	flusher interface{ Flush() }

	textOpen  bool
	textIdx   int
	nextIdx   int // 다음 발행할 content block index

	// OpenAI tool_calls[].index → Anthropic content block index 매핑
	toolBlockByOAI map[int]int
	toolNameByOAI  map[int]string
	toolIDByOAI    map[int]string
	toolOrder      []int // close 시 결정적 순서 유지

	stopReason   string
	outputTokens int
	inputTokens  int
}

func (s *streamState) writeMessageStart() error {
	body := map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id":            s.opts.MessageID,
			"type":          "message",
			"role":          "assistant",
			"model":         s.opts.Model,
			"content":       []any{},
			"stop_reason":   nil,
			"stop_sequence": nil,
			"usage":         map[string]int{"input_tokens": 0, "output_tokens": 0},
		},
	}
	return s.writeEvent("message_start", body)
}

func (s *streamState) handleChunk(c *ChatStreamChunk) error {
	if c.Usage != nil {
		if c.Usage.PromptTokens > 0 {
			s.inputTokens = c.Usage.PromptTokens
		}
		if c.Usage.CompletionTokens > 0 {
			s.outputTokens = c.Usage.CompletionTokens
		}
	}
	if len(c.Choices) == 0 {
		return nil
	}
	choice := c.Choices[0]
	delta := choice.Delta

	// 1) text delta
	if delta.Content != "" {
		if !s.textOpen {
			s.textIdx = s.nextIdx
			s.nextIdx++
			s.textOpen = true
			if err := s.writeEvent("content_block_start", map[string]any{
				"type":          "content_block_start",
				"index":         s.textIdx,
				"content_block": map[string]any{"type": "text", "text": ""},
			}); err != nil {
				return err
			}
		}
		if err := s.writeEvent("content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": s.textIdx,
			"delta": map[string]any{"type": "text_delta", "text": delta.Content},
		}); err != nil {
			return err
		}
	}

	// 2) tool_call delta
	for _, tc := range delta.ToolCalls {
		blockIdx, opened := s.toolBlockByOAI[tc.Index]
		// id/name은 보통 첫 청크에 들어오지만 일부 서버는 나누어 보낼 수 있어 누적한다.
		if tc.ID != "" {
			s.toolIDByOAI[tc.Index] = tc.ID
		}
		if tc.Function != nil && tc.Function.Name != "" {
			s.toolNameByOAI[tc.Index] = tc.Function.Name
		}
		if !opened {
			// 새 tool_use 블록 — 이전 text 블록이 열려있으면 먼저 닫는다.
			if s.textOpen {
				if err := s.writeEvent("content_block_stop", map[string]any{
					"type":  "content_block_stop",
					"index": s.textIdx,
				}); err != nil {
					return err
				}
				s.textOpen = false
			}
			// id/name이 아직 안 왔을 수 있어 비어있으면 일단 빈 값으로 열고 추후 채워넣지는 않는다 —
			// 사양상 OpenAI는 첫 tool_call delta에 id와 name을 함께 보낸다. 그래도 안전하게 처리.
			blockIdx = s.nextIdx
			s.nextIdx++
			s.toolBlockByOAI[tc.Index] = blockIdx
			s.toolOrder = append(s.toolOrder, tc.Index)
			if err := s.writeEvent("content_block_start", map[string]any{
				"type":  "content_block_start",
				"index": blockIdx,
				"content_block": map[string]any{
					"type":  "tool_use",
					"id":    s.toolIDByOAI[tc.Index],
					"name":  s.toolNameByOAI[tc.Index],
					"input": map[string]any{},
				},
			}); err != nil {
				return err
			}
		}
		if tc.Function != nil && tc.Function.Arguments != "" {
			if err := s.writeEvent("content_block_delta", map[string]any{
				"type":  "content_block_delta",
				"index": blockIdx,
				"delta": map[string]any{
					"type":         "input_json_delta",
					"partial_json": tc.Function.Arguments,
				},
			}); err != nil {
				return err
			}
		}
	}

	// 3) finish_reason
	if choice.FinishReason != "" {
		s.stopReason = mapFinishReason(choice.FinishReason, len(s.toolBlockByOAI) > 0)
	}
	return nil
}

func (s *streamState) finalize(fallbackReason string) error {
	// 열린 블록 닫기 — text 먼저, 다음 tool들을 OpenAI index 순서로.
	if s.textOpen {
		if err := s.writeEvent("content_block_stop", map[string]any{
			"type":  "content_block_stop",
			"index": s.textIdx,
		}); err != nil {
			return err
		}
		s.textOpen = false
	}
	for _, oaiIdx := range s.toolOrder {
		blockIdx := s.toolBlockByOAI[oaiIdx]
		if err := s.writeEvent("content_block_stop", map[string]any{
			"type":  "content_block_stop",
			"index": blockIdx,
		}); err != nil {
			return err
		}
	}

	reason := s.stopReason
	if reason == "" {
		reason = fallbackReason
	}
	if reason == "" {
		reason = "end_turn"
	}

	delta := map[string]any{
		"type":  "message_delta",
		"delta": map[string]any{"stop_reason": reason, "stop_sequence": nil},
		"usage": map[string]int{"output_tokens": s.outputTokens},
	}
	if err := s.writeEvent("message_delta", delta); err != nil {
		return err
	}
	return s.writeEvent("message_stop", map[string]any{"type": "message_stop"})
}

func (s *streamState) writeEvent(name string, body any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal SSE event %q: %w", name, err)
	}
	if _, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", name, data); err != nil {
		return err
	}
	if s.flusher != nil {
		s.flusher.Flush()
	}
	return nil
}
