package anthropic

import (
	"bytes"
	"encoding/json"
)

// Anthropic 규격에서 usage.input_tokens는 캐시에 적중하지 않은 신규 입력만 센다 —
// 전체 프롬프트 = input_tokens + cache_read_input_tokens + cache_creation_input_tokens.
// mlx-serve는 input_tokens에 전체 프롬프트를 싣고 cache_read를 따로 또 실어, 소비자가 합산하면
// 캐시분이 이중으로 잡힌다(실측: 6315토큰 프롬프트에 input 6315 + cache_read 6284).
// Claude Code의 컨텍스트·비용 미터가 거의 2배로 표시되는 원인이다.
//
// 이 파일의 보정은 그런 업스트림에서만 켜야 한다 — 규격대로 보내는 서버에 적용하면
// 신규 입력이 0 근처로 과소 집계된다.

// FixInclusiveUsage는 비스트리밍 응답 본문의 최상위 usage를 보정한다.
// 캐시 필드가 없거나 0이면 원본을 그대로 돌려준다.
func FixInclusiveUsage(body []byte) ([]byte, bool, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, false, err
	}
	raw, ok := envelope["usage"]
	if !ok {
		return body, false, nil
	}
	usage, err := decodeUsage(raw)
	if err != nil {
		return nil, false, err
	}
	input, ok := intField(usage, "input_tokens")
	cache := cacheTokens(usage)
	if !ok || cache == 0 {
		return body, false, nil
	}
	setInt(usage, "input_tokens", max(0, input-cache))
	return reencode(envelope, "usage", usage)
}

// UsageStreamFixer는 SSE 스트림 하나의 usage를 보정한다. 스트림마다 새로 만든다.
//
// mlx-serve는 input_tokens를 message_start에, cache_read를 마지막 message_delta에 나눠 보낸다.
// message_start 시점에는 캐시분을 모르므로 원본 input을 기억해 뒀다가, 캐시 필드가 실린
// message_delta에 보정된 input_tokens를 넣는다 — Claude Code는 delta의 usage 필드로
// start 값을 덮어쓴다.
type UsageStreamFixer struct {
	rawInput  int64
	haveInput bool
}

// Event는 SSE data 페이로드 하나를 받아 필요하면 고친 바이트를 돌려준다.
// message_start/message_delta가 아니면 파싱하지 않고 통과시킨다.
func (f *UsageStreamFixer) Event(data []byte) ([]byte, bool, error) {
	switch {
	case bytes.Contains(data, []byte(`"message_start"`)):
		return f.messageStart(data)
	case bytes.Contains(data, []byte(`"message_delta"`)):
		return f.messageDelta(data)
	}
	return data, false, nil
}

func (f *UsageStreamFixer) messageStart(data []byte) ([]byte, bool, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, false, err
	}
	if typeOf(envelope) != "message_start" {
		return data, false, nil
	}
	var message map[string]json.RawMessage
	if err := json.Unmarshal(envelope["message"], &message); err != nil {
		return data, false, nil
	}
	raw, ok := message["usage"]
	if !ok {
		return data, false, nil
	}
	usage, err := decodeUsage(raw)
	if err != nil {
		return nil, false, err
	}
	input, ok := intField(usage, "input_tokens")
	if !ok {
		return data, false, nil
	}
	f.rawInput, f.haveInput = input, true

	// 캐시분이 message_start에 함께 오는 서버라면 여기서 바로 고친다.
	cache := cacheTokens(usage)
	if cache == 0 {
		return data, false, nil
	}
	setInt(usage, "input_tokens", max(0, input-cache))
	patched, _, err := reencode(message, "usage", usage)
	if err != nil {
		return nil, false, err
	}
	return reencode(envelope, "message", json.RawMessage(patched))
}

func (f *UsageStreamFixer) messageDelta(data []byte) ([]byte, bool, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, false, err
	}
	if typeOf(envelope) != "message_delta" {
		return data, false, nil
	}
	raw, ok := envelope["usage"]
	if !ok {
		return data, false, nil
	}
	usage, err := decodeUsage(raw)
	if err != nil {
		return nil, false, err
	}
	cache := cacheTokens(usage)
	if cache == 0 {
		return data, false, nil
	}
	input, ok := intField(usage, "input_tokens")
	if !ok {
		if !f.haveInput {
			return data, false, nil
		}
		input = f.rawInput
	}
	// Claude Code는 delta의 input_tokens가 0이면 "값 없음"으로 보고 message_start 값(보정 전
	// 전체 프롬프트)을 유지한다. 프롬프트 전체가 캐시에 적중한 턴이 그렇게 되돌아가지 않도록 1로 막는다.
	setInt(usage, "input_tokens", max(1, input-cache))
	return reencode(envelope, "usage", usage)
}

func decodeUsage(raw json.RawMessage) (map[string]json.RawMessage, error) {
	var usage map[string]json.RawMessage
	if err := json.Unmarshal(raw, &usage); err != nil {
		return nil, err
	}
	return usage, nil
}

func typeOf(envelope map[string]json.RawMessage) string {
	var t string
	_ = json.Unmarshal(envelope["type"], &t)
	return t
}

func intField(m map[string]json.RawMessage, key string) (int64, bool) {
	raw, ok := m[key]
	if !ok {
		return 0, false
	}
	var v int64
	if err := json.Unmarshal(raw, &v); err != nil {
		return 0, false
	}
	return v, true
}

func cacheTokens(usage map[string]json.RawMessage) int64 {
	read, _ := intField(usage, "cache_read_input_tokens")
	creation, _ := intField(usage, "cache_creation_input_tokens")
	return read + creation
}

func setInt(m map[string]json.RawMessage, key string, v int64) {
	b, _ := json.Marshal(v)
	m[key] = b
}

// reencode는 수정한 하위 객체를 envelope[key]에 다시 넣고 envelope 전체를 직렬화한다.
func reencode[T any](envelope map[string]json.RawMessage, key string, value T) ([]byte, bool, error) {
	b, err := json.Marshal(value)
	if err != nil {
		return nil, false, err
	}
	envelope[key] = b
	out, err := json.Marshal(envelope)
	if err != nil {
		return nil, false, err
	}
	return out, true, nil
}
