package anthropic

import "testing"

const turn1 = `{"system":[{"type":"text","text":"You are Claude Code."},
	{"type":"text","cache_control":{"type":"ephemeral"},"text":"Project rules."}],
	"messages":[
		{"role":"user","content":[{"type":"text","text":"<system-reminder>ctx</system-reminder>"},{"type":"text","text":"작업해줘"}]}]}`

// 같은 대화의 다음 턴: 뒤에 메시지가 붙고, cache_control 마커가 옮겨가고,
// content가 문자열 형태로 바뀌기도 한다 — 전부 같은 id를 받아야 한다.
const turn2 = `{"system":[{"type":"text","cache_control":{"type":"ephemeral"},"text":"You are Claude Code."},
	{"type":"text","text":"Project rules."}],
	"messages":[
		{"role":"user","content":[{"type":"text","text":"<system-reminder>ctx</system-reminder>"},{"type":"text","text":"작업해줘"}]},
		{"role":"assistant","content":[{"type":"text","text":"ok"}]},
		{"role":"user","content":"다음"}]}`

func TestConversationKeyStableAcrossTurns(t *testing.T) {
	a, b := ConversationKey([]byte(turn1)), ConversationKey([]byte(turn2))
	if a == "" {
		t.Fatal("키가 비었다")
	}
	if a != b {
		t.Errorf("같은 대화인데 키가 달라졌다: %q vs %q", a, b)
	}
}

// 서브에이전트는 시스템 프롬프트가 다르다 — 메인과 같은 세션을 쓰면 안 된다.
func TestConversationKeyDiffersBySystemPrompt(t *testing.T) {
	sub := `{"system":[{"type":"text","text":"You are a subagent."}],
		"messages":[{"role":"user","content":[{"type":"text","text":"<system-reminder>ctx</system-reminder>"},{"type":"text","text":"작업해줘"}]}]}`
	if ConversationKey([]byte(turn1)) == ConversationKey([]byte(sub)) {
		t.Error("시스템 프롬프트가 다른데 키가 같다")
	}
}

// 같은 종류의 서브에이전트라도 지시가 다르면 별개 대화다.
func TestConversationKeyDiffersByFirstMessage(t *testing.T) {
	other := `{"system":[{"type":"text","text":"You are Claude Code."},{"type":"text","text":"Project rules."}],
		"messages":[{"role":"user","content":[{"type":"text","text":"<system-reminder>ctx</system-reminder>"},{"type":"text","text":"다른 작업"}]}]}`
	if ConversationKey([]byte(turn1)) == ConversationKey([]byte(other)) {
		t.Error("첫 메시지가 다른데 키가 같다")
	}
}

// content가 문자열이든 텍스트 블록 하나든 같은 대화다 — Claude Code가 형태를 바꿔 보낸다.
func TestConversationKeyIgnoresContentShape(t *testing.T) {
	asString := `{"system":"S","messages":[{"role":"user","content":"hello"}]}`
	asBlocks := `{"system":[{"type":"text","text":"S"}],"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`
	if ConversationKey([]byte(asString)) != ConversationKey([]byte(asBlocks)) {
		t.Error("content 표현 방식만 다른데 키가 달라졌다")
	}
}

func TestConversationKeyEmptyOnUnusable(t *testing.T) {
	for _, in := range []string{`{`, `{}`, `{"messages":[]}`} {
		if got := ConversationKey([]byte(in)); got != "" {
			t.Errorf("ConversationKey(%q) = %q, want \"\"", in, got)
		}
	}
}
