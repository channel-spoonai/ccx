package anthropic

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

// convHeadLimit은 해시 대상 바이트 상한. 시스템 프롬프트 + 첫 메시지는 실측 7KB 수준이라
// 넉넉하고, 비정상적으로 큰 첫 메시지에서 비용이 튀는 것만 막는다.
const convHeadLimit = 128 * 1024

// ConversationKey는 대화를 식별하는 짧은 해시를 만든다. 없거나 만들 수 없으면 "".
//
// 왜 대화 단위인가 — 세션 어피니티 id를 런치 단위로 하나만 쓰면 메인 대화와 서브에이전트가
// 같은 id를 공유한다. 둘은 시스템 프롬프트도 히스토리도 다른 별개 대화라, 먼저 도착한 쪽이
// id를 차지하고 자기 내용을 그 세션의 committed 스트림에 커밋해 버린다. 실측에서 서브에이전트가
// 메인 id를 차지한 턴의 재사용률이 38.2%까지 떨어졌다. 대화마다 다른 id를 주면 서로 오염되지
// 않고, 동시 요청이 같은 id로 겹칠 일도 없어진다.
//
// 재료는 시스템 블록과 첫 메시지의 **텍스트만** 쓴다. Claude Code는 턴마다 cache_control
// 마커를 옮기고 content를 문자열↔블록배열로 바꾸므로, 그런 표현 차이까지 해시에 넣으면
// 같은 대화가 턴마다 다른 id를 받는다.
func ConversationKey(body []byte) string {
	var envelope struct {
		System   json.RawMessage   `json:"system"`
		Messages []json.RawMessage `json:"messages"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return ""
	}

	var head strings.Builder
	head.WriteString(contentText(envelope.System))
	if len(envelope.Messages) > 0 {
		var first struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		}
		if json.Unmarshal(envelope.Messages[0], &first) == nil {
			head.WriteString("\x00")
			head.WriteString(first.Role)
			head.WriteString("\x00")
			head.WriteString(contentText(first.Content))
		}
	}
	if head.Len() == 0 {
		return ""
	}

	s := head.String()
	if len(s) > convHeadLimit {
		s = s[:convHeadLimit]
	}
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:5])
}

// contentText는 Anthropic content 필드(문자열 또는 블록 배열)에서 텍스트만 뽑아 잇는다.
// 텍스트가 아닌 블록은 타입 이름만 남겨 구조 변화는 반영하되 가변 메타(cache_control,
// tool_use_id 등)는 빼둔다.
func contentText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	var b strings.Builder
	for _, blk := range blocks {
		if blk.Type == "text" {
			b.WriteString(blk.Text)
			continue
		}
		b.WriteString("\x00")
		b.WriteString(blk.Type)
	}
	return b.String()
}
