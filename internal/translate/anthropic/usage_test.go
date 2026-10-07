package anthropic

import (
	"encoding/json"
	"testing"
)

func usageOf(t *testing.T, body []byte, path ...string) map[string]int64 {
	t.Helper()
	var cur any
	if err := json.Unmarshal(body, &cur); err != nil {
		t.Fatalf("invalid json %s: %v", body, err)
	}
	for _, k := range path {
		cur = cur.(map[string]any)[k]
	}
	out := map[string]int64{}
	for k, v := range cur.(map[string]any) {
		out[k] = int64(v.(float64))
	}
	return out
}

func TestFixInclusiveUsageSubtractsCache(t *testing.T) {
	// mlx-serve 실측 형태: input_tokens에 전체 프롬프트, cache_read가 그 위에 또.
	in := []byte(`{"id":"msg_1","type":"message","content":[{"type":"text","text":"OK"}],"usage":{"input_tokens":2792,"output_tokens":3,"cache_read_input_tokens":2761}}`)
	out, changed, err := FixInclusiveUsage(in)
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	u := usageOf(t, out, "usage")
	if u["input_tokens"] != 31 || u["cache_read_input_tokens"] != 2761 || u["output_tokens"] != 3 {
		t.Fatalf("usage = %v", u)
	}
}

func TestFixInclusiveUsageCountsCacheCreation(t *testing.T) {
	in := []byte(`{"usage":{"input_tokens":1000,"cache_read_input_tokens":600,"cache_creation_input_tokens":300}}`)
	out, _, _ := FixInclusiveUsage(in)
	if u := usageOf(t, out, "usage"); u["input_tokens"] != 100 {
		t.Fatalf("usage = %v", u)
	}
}

func TestFixInclusiveUsageLeavesUncachedAlone(t *testing.T) {
	in := []byte(`{"usage":{"input_tokens":6315,"output_tokens":1,"cache_read_input_tokens":0}}`)
	out, changed, err := FixInclusiveUsage(in)
	if err != nil || changed || string(out) != string(in) {
		t.Fatalf("changed=%v err=%v out=%s", changed, err, out)
	}
}

func TestUsageStreamFixerInjectsInputIntoDelta(t *testing.T) {
	// 스트리밍 실측 형태: input은 message_start, cache_read는 마지막 message_delta.
	var f UsageStreamFixer
	start := []byte(`{"type":"message_start","message":{"id":"msg_1","usage":{"input_tokens":6315,"output_tokens":1}}}`)
	out, changed, err := f.Event(start)
	if err != nil || changed || string(out) != string(start) {
		t.Fatalf("start changed=%v err=%v", changed, err)
	}
	ping := []byte(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"message_delta"}}`)
	if out, changed, _ := f.Event(ping); changed || string(out) != string(ping) {
		t.Fatalf("content delta must pass through: %s", out)
	}
	delta := []byte(`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1,"cache_read_input_tokens":6284}}`)
	out, changed, err = f.Event(delta)
	if err != nil || !changed {
		t.Fatalf("delta changed=%v err=%v", changed, err)
	}
	u := usageOf(t, out, "usage")
	if u["input_tokens"] != 31 || u["cache_read_input_tokens"] != 6284 || u["output_tokens"] != 1 {
		t.Fatalf("usage = %v", u)
	}
}

func TestUsageStreamFixerFixesStartWhenCacheArrivesEarly(t *testing.T) {
	var f UsageStreamFixer
	start := []byte(`{"type":"message_start","message":{"id":"msg_1","usage":{"input_tokens":500,"cache_read_input_tokens":400}}}`)
	out, changed, err := f.Event(start)
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if u := usageOf(t, out, "message", "usage"); u["input_tokens"] != 100 {
		t.Fatalf("usage = %v", u)
	}
	// delta가 캐시를 다시 실어도 원본 input 기준으로 계산해 이중 차감하지 않는다.
	out, _, _ = f.Event([]byte(`{"type":"message_delta","usage":{"output_tokens":5,"cache_read_input_tokens":400}}`))
	if u := usageOf(t, out, "usage"); u["input_tokens"] != 100 {
		t.Fatalf("usage = %v", u)
	}
}

func TestUsageStreamFixerKeepsFullyCachedAboveZero(t *testing.T) {
	// Claude Code는 delta input_tokens가 0이면 start 값(보정 전)을 유지하므로 1로 막는다.
	var f UsageStreamFixer
	_, _, _ = f.Event([]byte(`{"type":"message_start","message":{"usage":{"input_tokens":100}}}`))
	out, _, _ := f.Event([]byte(`{"type":"message_delta","usage":{"output_tokens":1,"cache_read_input_tokens":100}}`))
	if u := usageOf(t, out, "usage"); u["input_tokens"] != 1 {
		t.Fatalf("usage = %v", u)
	}
}

func TestUsageStreamFixerCorrectsDeltaInput(t *testing.T) {
	var f UsageStreamFixer
	_, _, _ = f.Event([]byte(`{"type":"message_start","message":{"usage":{"input_tokens":900}}}`))
	out, _, _ := f.Event([]byte(`{"type":"message_delta","usage":{"input_tokens":1000,"cache_read_input_tokens":700}}`))
	if u := usageOf(t, out, "usage"); u["input_tokens"] != 300 {
		t.Fatalf("usage = %v", u)
	}
}
