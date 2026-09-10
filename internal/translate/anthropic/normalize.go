// Package anthropic은 Anthropic Messages 페이로드를 업스트림에 그대로 흘려보내되,
// 프리픽스 캐시를 깨는 형태만 최소 침습으로 손보는 정규화를 담당한다.
package anthropic

import "encoding/json"

// NormalizeSystemMessages는 messages 배열 안의 role:"system" 메시지를 role:"user"로 바꾼다.
// 바뀐 개수를 함께 돌려주며, 하나도 없으면 원본 바이트를 그대로 반환한다.
//
// 왜 필요한가 — Claude Code는 anthropic-beta `mid-conversation-system-2026-04-07`로
// 대화 **중간에** role:"system" 메시지를 턴마다 하나씩 추가한다. Qwen 계열 chat template은
// `System message must be at the beginning.`으로 이를 거부하므로 서버가 재배치·병합하는데,
// 새 system 메시지가 붙을 때마다 그 결과가 달라져 프롬프트 앞부분이 흔들린다. 그러면 직전 턴의
// KV 스냅샷이 더 이상 프롬프트의 접두가 아니게 되어 재프리필이 발생한다.
// (MTPLX 실측: 정규화 전 턴 재사용률 85.1% → 정규화 후 100.0%.)
//
// 최상위 키는 messages만 교체하고 나머지는 원본 RawMessage를 그대로 보존한다 — 프록시는
// 번역기가 아니라 패스스루이므로 손대지 않은 필드가 업스트림에 변형 없이 도달해야 한다.
func NormalizeSystemMessages(body []byte) ([]byte, int, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, 0, err
	}
	raw, ok := envelope["messages"]
	if !ok {
		return body, 0, nil
	}
	var messages []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &messages); err != nil {
		return nil, 0, err
	}

	converted := 0
	for _, m := range messages {
		var role string
		if err := json.Unmarshal(m["role"], &role); err != nil {
			continue
		}
		if role != "system" {
			continue
		}
		m["role"] = json.RawMessage(`"user"`)
		converted++
	}
	if converted == 0 {
		return body, 0, nil
	}

	patched, err := json.Marshal(messages)
	if err != nil {
		return nil, 0, err
	}
	envelope["messages"] = patched
	out, err := json.Marshal(envelope)
	if err != nil {
		return nil, 0, err
	}
	return out, converted, nil
}
