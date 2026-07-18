package codex

import (
	"encoding/json"
	"io"
)

// CodexUsage는 Codex가 응답 끝에 포함하는 사용량 정보.
type CodexUsage struct {
	InputTokens         int `json:"input_tokens,omitempty"`
	OutputTokens        int `json:"output_tokens,omitempty"`
	InputTokensDetails  *struct {
		CachedTokens int `json:"cached_tokens,omitempty"`
	} `json:"input_tokens_details,omitempty"`
	OutputTokensDetails *struct {
		ReasoningTokens int `json:"reasoning_tokens,omitempty"`
	} `json:"output_tokens_details,omitempty"`
}

// AnthropicUsage는 Anthropic 클라이언트가 기대하는 usage 모양.
type AnthropicUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

// MapUsage는 Codex 사용량을 Anthropic 형식으로 변환한다.
// OpenAI는 캐시된 토큰을 input_tokens 안에 포함해 보고하지만 Anthropic 측에서는 분리해
// 보고한다. Claude Code가 input + cache_read 를 합산해 컨텍스트 크기를 추정하므로
// 여기서 캐시 부분을 빼지 않으면 두 번 더해진다 — raine과 동일한 보정.
func MapUsage(u *CodexUsage) AnthropicUsage {
	if u == nil {
		return AnthropicUsage{}
	}
	cached := 0
	if u.InputTokensDetails != nil {
		cached = u.InputTokensDetails.CachedTokens
	}
	in := u.InputTokens - cached
	if in < 0 {
		in = 0
	}
	return AnthropicUsage{
		InputTokens:              in,
		OutputTokens:             u.OutputTokens,
		CacheCreationInputTokens: 0,
		CacheReadInputTokens:     cached,
	}
}

// === Reducer (state machine) ===

// EventKind 구분.
type EventKind int

const (
	EventTextStart EventKind = iota
	EventTextDelta
	EventTextStop
	EventToolStart
	EventToolDelta
	EventToolStop
	EventFinish
)

// StopReason은 Anthropic의 stop_reason.
type StopReason string

const (
	StopEndTurn   StopReason = "end_turn"
	StopToolUse   StopReason = "tool_use"
	StopMaxTokens StopReason = "max_tokens"
)

// ReducerEvent는 SSE를 추상화한 다운스트림용 이벤트.
type ReducerEvent struct {
	Kind        EventKind
	Index       int
	Text        string
	ToolID      string
	ToolName    string
	PartialJSON string
	StopReason  StopReason
	Usage       *CodexUsage
}

// UpstreamErrorKind는 Codex 측에서 받은 오류 분류.
type UpstreamErrorKind string

const (
	ErrorRateLimit UpstreamErrorKind = "rate_limit"
	ErrorFailed    UpstreamErrorKind = "failed"
)

// UpstreamError는 Codex SSE에서 surface된 명시적 에러.
type UpstreamError struct {
	Kind              UpstreamErrorKind
	Message           string
	RetryAfterSeconds int
}

func (e *UpstreamError) Error() string { return string(e.Kind) + ": " + e.Message }

// blockState는 reducer 내부 상태.
type blockState struct {
	isText      bool
	index       int
	textAccum   string // text 전용
	callID      string // tool 전용
	name        string
	argsAccum   string
	hadDelta    bool
	bufferUntil bool
	emittedArgs bool
}

// shouldBufferToolArgs: Read 도구는 arguments JSON에 끝까지 들어와야 정확히 처리되므로
// done 시점까지 버퍼링한다 (raine과 동일).
func shouldBufferToolArgs(name string) bool { return name == "Read" }

// sanitizeToolArgs: Read 도구의 비표준 pages="" 필드를 제거한다 (raine과 동일).
func sanitizeToolArgs(name, args string) string {
	if name != "Read" || args == "" {
		return args
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(args), &m); err != nil {
		return args
	}
	v, ok := m["pages"]
	if !ok {
		return args
	}
	if s, isStr := v.(string); !isStr || s != "" {
		return args
	}
	delete(m, "pages")
	out, err := json.Marshal(m)
	if err != nil {
		return args
	}
	return string(out)
}

// ReduceUpstream는 Codex SSE 스트림을 읽어 ReducerEvent 시퀀스를 emit 콜백으로 흘려보낸다.
// 콜백이 false를 반환하면 중단. 업스트림이 명시적 에러 이벤트를 보낸 경우 *UpstreamError 반환.
func ReduceUpstream(r io.Reader, emit func(ReducerEvent) bool) error {
	blocks := map[int]*blockState{}
	itemIDToOutputIdx := map[string]int{}
	anthropicIdx := 0
	sawToolUse := false
	var finalUsage *CodexUsage
	incomplete := false
	var streamErr *UpstreamError

	parseErr := ParseSSE(r, func(evt SSEEvent) bool {
		if evt.Data == "" {
			return true
		}
		var p map[string]json.RawMessage
		if err := json.Unmarshal([]byte(evt.Data), &p); err != nil {
			return true // 잘못된 JSON은 조용히 skip — raine 동작
		}
		t := strJSON(p["type"])
		if t == "" {
			t = evt.Event
		}

		switch t {
		case "codex.rate_limits":
			var rl struct {
				RateLimits struct {
					LimitReached bool `json:"limit_reached"`
					Primary      struct {
						ResetAfterSeconds int `json:"reset_after_seconds"`
					} `json:"primary"`
				} `json:"rate_limits"`
			}
			_ = json.Unmarshal([]byte(evt.Data), &rl)
			if rl.RateLimits.LimitReached {
				streamErr = &UpstreamError{
					Kind:              ErrorRateLimit,
					Message:           "rate limit reached",
					RetryAfterSeconds: rl.RateLimits.Primary.ResetAfterSeconds,
				}
				return false
			}
			return true

		case "response.failed", "response.error", "error":
			streamErr = &UpstreamError{Kind: ErrorFailed, Message: extractErrorMessage(evt.Data)}
			return false

		case "response.output_item.added":
			var pl struct {
				OutputIndex int             `json:"output_index"`
				Item        json.RawMessage `json:"item"`
			}
			if err := json.Unmarshal([]byte(evt.Data), &pl); err != nil || len(pl.Item) == 0 {
				return true
			}
			var item struct {
				Type   string `json:"type"`
				ID     string `json:"id"`
				CallID string `json:"call_id"`
				Name   string `json:"name"`
			}
			_ = json.Unmarshal(pl.Item, &item)
			switch item.Type {
			case "reasoning":
				return true
			case "message":
				idx := anthropicIdx
				anthropicIdx++
				blocks[pl.OutputIndex] = &blockState{isText: true, index: idx}
				if item.ID != "" {
					itemIDToOutputIdx[item.ID] = pl.OutputIndex
				}
				return emit(ReducerEvent{Kind: EventTextStart, Index: idx})
			case "function_call":
				sawToolUse = true
				idx := anthropicIdx
				anthropicIdx++
				blocks[pl.OutputIndex] = &blockState{
					index:       idx,
					callID:      item.CallID,
					name:        item.Name,
					bufferUntil: shouldBufferToolArgs(item.Name),
				}
				return emit(ReducerEvent{
					Kind:     EventToolStart,
					Index:    idx,
					ToolID:   item.CallID,
					ToolName: item.Name,
				})
			}
			return true

		case "response.output_text.delta":
			var pl struct {
				OutputIndex *int   `json:"output_index"`
				ItemID      string `json:"item_id"`
				Delta       string `json:"delta"`
			}
			if err := json.Unmarshal([]byte(evt.Data), &pl); err != nil {
				return true
			}
			var st *blockState
			if pl.OutputIndex != nil {
				st = blocks[*pl.OutputIndex]
			}
			if st == nil && pl.ItemID != "" {
				if mapped, ok := itemIDToOutputIdx[pl.ItemID]; ok {
					st = blocks[mapped]
				}
			}
			if st == nil || !st.isText || pl.Delta == "" {
				return true
			}
			st.textAccum += pl.Delta
			return emit(ReducerEvent{Kind: EventTextDelta, Index: st.index, Text: pl.Delta})

		case "response.function_call_arguments.delta":
			var pl struct {
				OutputIndex int    `json:"output_index"`
				Delta       string `json:"delta"`
			}
			if err := json.Unmarshal([]byte(evt.Data), &pl); err != nil {
				return true
			}
			st := blocks[pl.OutputIndex]
			if st == nil || st.isText || pl.Delta == "" {
				return true
			}
			st.argsAccum += pl.Delta
			st.hadDelta = true
			if st.bufferUntil {
				return true // done에서 한 번에 emit
			}
			st.emittedArgs = true
			return emit(ReducerEvent{Kind: EventToolDelta, Index: st.index, PartialJSON: pl.Delta})

		case "response.function_call_arguments.done":
			var pl struct {
				OutputIndex int    `json:"output_index"`
				Arguments   string `json:"arguments"`
			}
			_ = json.Unmarshal([]byte(evt.Data), &pl)
			st := blocks[pl.OutputIndex]
			if st == nil || st.isText {
				return true
			}
			if pl.Arguments != "" && st.argsAccum == "" {
				st.argsAccum = pl.Arguments
			}
			return true

		case "response.output_item.done":
			var pl struct {
				OutputIndex int             `json:"output_index"`
				Item        json.RawMessage `json:"item"`
			}
			_ = json.Unmarshal([]byte(evt.Data), &pl)
			st, ok := blocks[pl.OutputIndex]
			if !ok {
				return true
			}
			var item struct {
				Type      string `json:"type"`
				Arguments string `json:"arguments"`
			}
			_ = json.Unmarshal(pl.Item, &item)
			if item.Type == "reasoning" {
				delete(blocks, pl.OutputIndex)
				return true
			}
			if !st.isText {
				finalArgs := item.Arguments
				if finalArgs == "" {
					finalArgs = st.argsAccum
				}
				if finalArgs != "" {
					st.argsAccum = sanitizeToolArgs(st.name, finalArgs)
					if st.bufferUntil || !st.emittedArgs {
						st.emittedArgs = true
						if !emit(ReducerEvent{Kind: EventToolDelta, Index: st.index, PartialJSON: st.argsAccum}) {
							return false
						}
					}
				}
			}
			delete(blocks, pl.OutputIndex)
			if st.isText {
				return emit(ReducerEvent{Kind: EventTextStop, Index: st.index})
			}
			return emit(ReducerEvent{Kind: EventToolStop, Index: st.index})

		case "response.completed", "response.incomplete":
			var pl struct {
				Response struct {
					Usage             *CodexUsage `json:"usage"`
					Status            string      `json:"status"`
					IncompleteDetails *struct {
						Reason string `json:"reason"`
					} `json:"incomplete_details"`
				} `json:"response"`
			}
			_ = json.Unmarshal([]byte(evt.Data), &pl)
			finalUsage = pl.Response.Usage
			if t == "response.incomplete" || pl.Response.Status == "incomplete" ||
				(pl.Response.IncompleteDetails != nil && pl.Response.IncompleteDetails.Reason == "max_output_tokens") {
				incomplete = true
			}
			return true
		}
		return true
	})

	if streamErr != nil {
		return streamErr
	}
	if parseErr != nil {
		return parseErr
	}

	reason := StopEndTurn
	if incomplete {
		reason = StopMaxTokens
	} else if sawToolUse {
		reason = StopToolUse
	}
	emit(ReducerEvent{Kind: EventFinish, StopReason: reason, Usage: finalUsage})
	return nil
}

func strJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

func extractErrorMessage(data string) string {
	var p struct {
		Response struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		} `json:"response"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal([]byte(data), &p)
	if p.Response.Error.Message != "" {
		return p.Response.Error.Message
	}
	if p.Error.Message != "" {
		return p.Error.Message
	}
	return "Upstream error"
}
