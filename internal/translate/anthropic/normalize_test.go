package anthropic

import (
	"encoding/json"
	"testing"
)

func roles(t *testing.T, body []byte) []string {
	t.Helper()
	var env struct {
		Messages []struct {
			Role string `json:"role"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("결과 JSON 파싱 실패: %v", err)
	}
	out := make([]string, len(env.Messages))
	for i, m := range env.Messages {
		out[i] = m.Role
	}
	return out
}

func TestNormalizeConvertsSystemMessages(t *testing.T) {
	in := []byte(`{"model":"m","messages":[
		{"role":"user","content":"a"},
		{"role":"system","content":"env"},
		{"role":"assistant","content":"b"},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"x"}]},
		{"role":"system","content":"env2"}]}`)
	out, n, err := NormalizeSystemMessages(in)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("변환 개수 = %d, want 2", n)
	}
	got := roles(t, out)
	want := []string{"user", "user", "assistant", "user", "user"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("roles = %v, want %v", got, want)
		}
	}
}

func TestNormalizePreservesUntouchedFields(t *testing.T) {
	in := []byte(`{"model":"m","max_tokens":7,"system":[{"type":"text","text":"s"}],` +
		`"tools":[{"name":"T","input_schema":{"type":"object"}}],` +
		`"messages":[{"role":"user","content":"a"},{"role":"system","content":"env"}]}`)
	out, _, err := NormalizeSystemMessages(in)
	if err != nil {
		t.Fatal(err)
	}
	var a, b map[string]any
	_ = json.Unmarshal(in, &a)
	_ = json.Unmarshal(out, &b)
	for _, k := range []string{"model", "max_tokens", "system", "tools"} {
		x, _ := json.Marshal(a[k])
		y, _ := json.Marshal(b[k])
		if string(x) != string(y) {
			t.Errorf("%s 필드가 변형됐다: %s → %s", k, x, y)
		}
	}
}

func TestNormalizeNoopReturnsOriginalBytes(t *testing.T) {
	in := []byte(`{"model":"m","messages":[{"role":"user","content":"a"}]}`)
	out, n, err := NormalizeSystemMessages(in)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("변환 개수 = %d, want 0", n)
	}
	// system이 없으면 재직렬화조차 하지 않아야 한다 — 패스스루 충실도.
	if string(out) != string(in) {
		t.Errorf("원본 바이트가 보존되지 않았다:\n%s\n%s", in, out)
	}
}

func TestNormalizeRejectsInvalidJSON(t *testing.T) {
	if _, _, err := NormalizeSystemMessages([]byte(`{`)); err == nil {
		t.Error("깨진 JSON에 에러를 내야 한다")
	}
}
