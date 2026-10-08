package anthropic

import (
	"strings"
	"testing"
)

func TestCountTokensText(t *testing.T) {
	body := `{
		"model": "m",
		"system": [{"type":"text","text":"SYS","cache_control":{"type":"ephemeral"}}],
		"tools": [{"name":"Read","description":"읽기 <도구>","input_schema":{"type":"object","properties":{"path":{"type":"string"}}}}],
		"messages": [
			{"role":"user","content":"안녕"},
			{"role":"assistant","content":[
				{"type":"thinking","thinking":"THINK","signature":"sig"},
				{"type":"tool_use","id":"t1","name":"Read","input":{"path":"a & b.go"}}
			]},
			{"role":"user","content":[
				{"type":"tool_result","tool_use_id":"t1","content":[{"type":"text","text":"RESULT"}]},
				{"type":"image","source":{"type":"base64","data":"QUFBQQ=="}}
			]}
		]
	}`
	text, overhead, err := CountTokensText([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"SYS", "Read", "읽기 <도구>", `{"properties":{"path":{"type":"string"}},"type":"object"}`,
		"안녕", "THINK", `{"path":"a & b.go"}`, "RESULT"} {
		if !strings.Contains(text, want) {
			t.Errorf("text에 %q 없음:\n%s", want, text)
		}
	}
	// 모델이 읽지 않는 메타(시그니처·id·cache_control)와 이미지 데이터는 세지 않는다.
	for _, bad := range []string{"sig", "t1", "ephemeral", "QUFBQQ", `\u`} {
		if strings.Contains(text, bad) {
			t.Errorf("text에 %q가 들어감:\n%s", bad, text)
		}
	}
	// system 1 + tool 1 + messages 3
	if want := 5 * perMessageOverhead; overhead != want {
		t.Errorf("overhead = %d, want %d", overhead, want)
	}
}

func TestCountTokensTextStringSystemAndEmpty(t *testing.T) {
	text, overhead, err := CountTokensText([]byte(`{"system":"S","messages":[{"role":"user","content":"hi"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "S\n") || !strings.Contains(text, "hi") || overhead != 2*perMessageOverhead {
		t.Errorf("text=%q overhead=%d", text, overhead)
	}

	text, overhead, err = CountTokensText([]byte(`{"messages":[]}`))
	if err != nil || text != "" || overhead != 0 {
		t.Errorf("빈 요청: text=%q overhead=%d err=%v", text, overhead, err)
	}

	if _, _, err := CountTokensText([]byte(`not json`)); err == nil {
		t.Error("잘못된 JSON에 에러가 없음")
	}
}
