package anthropic

import (
	"encoding/json"
	"testing"
)

// low는 thinking을 끈다 — Claude Code가 보내는 {"type":"adaptive"}를 disabled로 덮는다.
// 필드를 지우기만 하면 업스트림이 "의견 없음"으로 읽고 서버 기본(켬)으로 돌아가므로 명시가 필요하다.
func TestApplyEffortLowDisablesThinking(t *testing.T) {
	body := []byte(`{"model":"m","output_config":{"effort":"low"},"thinking":{"type":"adaptive","display":"omitted"},"messages":[]}`)
	out, res, err := ApplyEffort(body, DefaultEffortMap())
	if err != nil {
		t.Fatalf("ApplyEffort 실패: %v", err)
	}
	if !res.Changed || !res.ThinkingDisabled || res.ReasoningEffort != "" {
		t.Fatalf("결과가 thinking off여야 하는데 %+v", res)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("결과 파싱 실패: %v", err)
	}
	if string(got["thinking"]) != `{"type":"disabled"}` {
		t.Fatalf("thinking이 disabled여야 하는데 %s", got["thinking"])
	}
	if _, ok := got["reasoning_effort"]; ok {
		t.Fatal("thinking을 끈 요청에 reasoning_effort를 실으면 안 된다")
	}
}

// Claude Code 5단을 Qwen3.8의 3단(low/medium/xhigh) 위에 한 칸 내려 얹는다.
func TestApplyEffortMapsOneTierDown(t *testing.T) {
	for effort, want := range map[string]string{
		"medium":    "low",
		"high":      "medium",
		"xhigh":     "xhigh",
		"max":       "xhigh",
		"ultracode": "xhigh",
	} {
		body := []byte(`{"output_config":{"effort":"` + effort + `"},"thinking":{"type":"adaptive"}}`)
		out, res, err := ApplyEffort(body, DefaultEffortMap())
		if err != nil {
			t.Fatalf("ApplyEffort(%s) 실패: %v", effort, err)
		}
		if res.ReasoningEffort != want || res.ThinkingDisabled {
			t.Fatalf("%s는 %s 주입이어야 하는데 %+v", effort, want, res)
		}
		var got map[string]json.RawMessage
		_ = json.Unmarshal(out, &got)
		if string(got["reasoning_effort"]) != `"`+want+`"` {
			t.Fatalf("%s의 reasoning_effort가 %s여야 하는데 %s", effort, want, got["reasoning_effort"])
		}
		// thinking은 손대지 않는다 — 업스트림이 adaptive를 모르므로 서버 기본(켬)이 된다.
		if string(got["thinking"]) != `{"type":"adaptive"}` {
			t.Fatalf("%s에서 thinking 원본이 보존돼야 하는데 %s", effort, got["thinking"])
		}
	}
}

// 프록시는 번역기가 아니라 패스스루다 — 손대지 않은 최상위 필드는 바이트 그대로 남아야 한다.
func TestApplyEffortPreservesOtherFields(t *testing.T) {
	body := []byte(`{"output_config":{"effort":"low"},"system":[{"type":"text","text":"s"}],"tools":[{"name":"t"}],"metadata":{"user_id":"u"}}`)
	out, _, err := ApplyEffort(body, DefaultEffortMap())
	if err != nil {
		t.Fatalf("ApplyEffort 실패: %v", err)
	}
	var got map[string]json.RawMessage
	_ = json.Unmarshal(out, &got)
	for k, want := range map[string]string{
		"system":        `[{"type":"text","text":"s"}]`,
		"tools":         `[{"name":"t"}]`,
		"metadata":      `{"user_id":"u"}`,
		"output_config": `{"effort":"low"}`,
	} {
		if string(got[k]) != want {
			t.Fatalf("%s가 보존돼야 하는데 %s", k, got[k])
		}
	}
}

// output_config가 없거나 표에 없는 값이면 원본 바이트를 그대로 돌려준다(재직렬화도 하지 않는다).
func TestApplyEffortPassthrough(t *testing.T) {
	for _, body := range []string{
		`{"model":"m","messages":[]}`,
		`{"output_config":{},"messages":[]}`,
		`{"output_config":{"effort":"banana"},"messages":[]}`,
	} {
		out, res, err := ApplyEffort([]byte(body), DefaultEffortMap())
		if err != nil {
			t.Fatalf("ApplyEffort(%s) 실패: %v", body, err)
		}
		if res.Changed || string(out) != body {
			t.Fatalf("%s는 그대로 통과해야 하는데 %s (%+v)", body, out, res)
		}
	}
}

// keep은 그 티어만 손대지 않고 통과시킨다.
func TestApplyEffortKeep(t *testing.T) {
	m := map[string]string{"medium": EffortKeep}
	body := []byte(`{"output_config":{"effort":"medium"}}`)
	out, res, err := ApplyEffort(body, m)
	if err != nil {
		t.Fatalf("ApplyEffort 실패: %v", err)
	}
	if res.Changed || string(out) != string(body) {
		t.Fatalf("keep은 통과여야 하는데 %s", out)
	}
	if res.Requested != "medium" {
		t.Fatalf("요청 effort는 기록돼야 하는데 %q", res.Requested)
	}
}

// thinking을 끄는 티어에서는 이전 요청이 남긴 reasoning_effort도 걷어낸다.
func TestApplyEffortOffDropsStaleReasoningEffort(t *testing.T) {
	body := []byte(`{"output_config":{"effort":"low"},"reasoning_effort":"xhigh"}`)
	out, _, err := ApplyEffort(body, DefaultEffortMap())
	if err != nil {
		t.Fatalf("ApplyEffort 실패: %v", err)
	}
	var got map[string]json.RawMessage
	_ = json.Unmarshal(out, &got)
	if _, ok := got["reasoning_effort"]; ok {
		t.Fatalf("reasoning_effort가 지워져야 하는데 %s", out)
	}
}

func TestParseEffortMap(t *testing.T) {
	m, err := ParseEffortMap("low=off, high=xhigh")
	if err != nil {
		t.Fatalf("ParseEffortMap 실패: %v", err)
	}
	if m["low"] != EffortOff || m["high"] != "xhigh" || len(m) != 2 {
		t.Fatalf("표가 어긋난다: %+v", m)
	}
	if _, err := ParseEffortMap("low"); err == nil {
		t.Fatal("level=action 형식이 아니면 에러여야 한다")
	}
	def, err := ParseEffortMap("")
	if err != nil || def["low"] != EffortOff {
		t.Fatalf("빈 문자열은 기본 표여야 하는데 %+v (%v)", def, err)
	}
}

func TestEffortMapFromEnv(t *testing.T) {
	for _, v := range []string{"off", "false", "0", "none", " OFF "} {
		m, err := EffortMapFromEnv(v)
		if err != nil || m != nil {
			t.Fatalf("%q는 기능을 꺼야 하는데 %+v (%v)", v, m, err)
		}
	}
	m, err := EffortMapFromEnv("")
	if err != nil || m["max"] != "xhigh" {
		t.Fatalf("빈 값은 기본 표여야 하는데 %+v (%v)", m, err)
	}
}

// 표가 비어 있으면(기능 끔) 본문에 손대지 않는다.
func TestApplyEffortNilMap(t *testing.T) {
	body := []byte(`{"output_config":{"effort":"low"}}`)
	out, res, err := ApplyEffort(body, nil)
	if err != nil || res.Changed || string(out) != string(body) {
		t.Fatalf("nil 표는 통과여야 하는데 %s (%+v, %v)", out, res, err)
	}
}
