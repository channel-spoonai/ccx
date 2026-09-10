package anthropic

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// EffortOff는 매핑 값으로 쓰면 그 effort에서 thinking을 아예 끈다.
const EffortOff = "off"

// EffortKeep은 매핑 값으로 쓰면 그 effort를 손대지 않고 통과시킨다.
const EffortKeep = "keep"

// DefaultEffortMap은 Claude Code의 effort를 업스트림 동작으로 옮기는 기본 표.
//
// Claude Code 2.1.267은 anthropic-beta `effort-2025-11-24`로 effort를 요청 본문의
// `output_config.effort`에 싣는다(값은 low/medium/high/xhigh/max — `auto`와 `ultracode`도
// 구체값으로 해석된 뒤 실린다). MTPLX가 읽는 자리는 최상위 flat `reasoning_effort`뿐이라
// 그대로는 닿지 않는다.
//
// Claude Code는 5단이고 Qwen3.8 Flash Next가 선언한 티어는 low/medium/xhigh 3종
// (`reasoning_policy.effort_levels`)이라 1:1로 겹치지 않는다. 그래서 **한 칸 내려 얹는다** —
// medium→low, high→medium, xhigh 이상→xhigh. Claude Code 쪽 등급은 Anthropic 모델을 기준으로
// 매겨진 것이라 같은 이름이 같은 깊이를 뜻하지 않는다.
//
// 맨 아래 칸인 low는 "짧게 생각하기"가 아니라 **thinking 자체를 끈다**. Qwen3.8 계열은
// thinking을 끄면 chat template이 `<think>\n\n</think>`를 미리 채워 사고 단계를 건너뛰므로,
// 출력 토큰과 지연이 함께 줄어든다(실측: 툴 콜 포함 요청에서 53토큰 9.5초 → 27토큰 3.9초,
// tool_use는 정상). 그 자리를 low 티어에 주면 "사고 없이 빠르게"라는 칸이 사라진다.
func DefaultEffortMap() map[string]string {
	return map[string]string{
		"low":       EffortOff,
		"medium":    "low",
		"high":      "medium",
		"xhigh":     "xhigh",
		"max":       "xhigh",
		"ultracode": "xhigh",
	}
}

// ParseEffortMap은 "low=off,medium=medium,high=xhigh" 형태를 표로 바꾼다.
// 빈 문자열이면 기본 표를 그대로 준다.
func ParseEffortMap(s string) (map[string]string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return DefaultEffortMap(), nil
	}
	out := map[string]string{}
	for _, pair := range strings.Split(s, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		k, v, ok := strings.Cut(pair, "=")
		k = strings.ToLower(strings.TrimSpace(k))
		v = strings.ToLower(strings.TrimSpace(v))
		if !ok || k == "" || v == "" {
			return nil, fmt.Errorf("malformed effort mapping %q — expected level=action", pair)
		}
		out[k] = v
	}
	if len(out) == 0 {
		return DefaultEffortMap(), nil
	}
	return out, nil
}

// FormatEffortMap은 배너 출력용으로 표를 정렬된 한 줄로 만든다.
func FormatEffortMap(m map[string]string) string {
	order := map[string]int{"low": 0, "medium": 1, "high": 2, "xhigh": 3, "max": 4, "ultracode": 5}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		oi, oki := order[keys[i]]
		oj, okj := order[keys[j]]
		if oki != okj {
			return oki
		}
		if oki && okj && oi != oj {
			return oi < oj
		}
		return keys[i] < keys[j]
	})
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"→"+m[k])
	}
	return strings.Join(parts, " ")
}

// EffortResult는 ApplyEffort가 무엇을 했는지 알려준다.
type EffortResult struct {
	// Requested는 요청이 들고 온 output_config.effort ("" = 필드 없음).
	Requested string
	// ThinkingDisabled는 thinking을 껐는지.
	ThinkingDisabled bool
	// ReasoningEffort는 업스트림에 실은 reasoning_effort ("" = 안 실음).
	ReasoningEffort string
	// Changed는 본문을 실제로 고쳤는지.
	Changed bool
}

// ApplyEffort는 `output_config.effort`를 업스트림이 읽는 자리로 옮긴다.
//
//   - 매핑이 EffortOff면 `thinking`을 {"type":"disabled"}로 바꾸고 reasoning_effort는 싣지 않는다.
//     Claude Code는 thinking을 끄더라도 `thinking` 필드를 **빼기만** 하는데, 업스트림은 필드
//     부재를 "클라이언트 의견 없음"으로 읽고 서버 기본값(켬)으로 폴백한다. 그래서 끄려면
//     비우는 게 아니라 disabled를 명시해야 한다.
//   - 그 밖의 값이면 최상위 `reasoning_effort`에 그 값을 싣는다. `thinking`은 원본 그대로 둔다 —
//     Claude Code가 보내는 {"type":"adaptive"}는 업스트림이 인식하지 못해 서버 기본(켬)이 되고,
//     effort만 클라이언트 값으로 적용된다.
//
// output_config가 없거나 매핑에 없는 effort면 원본 바이트를 그대로 돌려준다. 최상위 키는
// 손대는 것만 교체하고 나머지는 원본 RawMessage를 보존한다 — 프록시는 번역기가 아니라
// 패스스루라서 건드리지 않은 필드가 바이트 그대로 업스트림에 닿아야 한다.
func ApplyEffort(body []byte, m map[string]string) ([]byte, EffortResult, error) {
	var res EffortResult
	if len(m) == 0 {
		return body, res, nil
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, res, err
	}
	raw, ok := envelope["output_config"]
	if !ok {
		return body, res, nil
	}
	var oc struct {
		Effort string `json:"effort"`
	}
	if err := json.Unmarshal(raw, &oc); err != nil {
		return nil, res, err
	}
	res.Requested = strings.TrimSpace(oc.Effort)
	if res.Requested == "" {
		return body, res, nil
	}

	action, ok := m[strings.ToLower(res.Requested)]
	if !ok || action == "" || action == EffortKeep {
		return body, res, nil
	}

	if action == EffortOff {
		envelope["thinking"] = json.RawMessage(`{"type":"disabled"}`)
		delete(envelope, "reasoning_effort")
		res.ThinkingDisabled = true
	} else {
		effort, err := json.Marshal(action)
		if err != nil {
			return nil, res, err
		}
		envelope["reasoning_effort"] = effort
		res.ReasoningEffort = action
	}

	out, err := json.Marshal(envelope)
	if err != nil {
		return nil, res, err
	}
	res.Changed = true
	return out, res, nil
}

// EffortMapFromEnv는 데몬 env 값 하나를 표로 바꾼다. "false"/"0"/"off"/"none"이면 nil —
// 기능 자체를 끈다(그때는 output_config가 그대로 업스트림에 흘러가고 무시된다).
// 빈 문자열은 기본 표.
func EffortMapFromEnv(v string) (map[string]string, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "false", "0", "off", "none":
		return nil, nil
	}
	return ParseEffortMap(v)
}
