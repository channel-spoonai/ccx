package anthropic

import (
	"bytes"
	"encoding/json"
	"strings"
)

// count_tokens를 구현하지 않은 업스트림(mlx-serve 등)에서 Claude Code는 글자 수로 토큰을
// 추정한다 — 일반 텍스트는 len/4, JSON(툴 결과 등)은 len/2. 이 모델 토크나이저 기준으로
// 툴 결과가 많은 대화는 2배 가까이 부풀고(실측: 서버 74,343토큰 → /context 142.3k),
// 한국어 텍스트는 반대로 절반 가까이 과소 추정된다. 프록시가 업스트림 토크나이저로 직접
// 세서 답하기 위해, count_tokens 요청을 토크나이저에 넣을 평문 하나로 펼친다.

// 채팅 템플릿이 메시지·툴 하나마다 덧붙이는 구분 토큰의 근사치(Qwen 계열
// "<|im_start|>role\n ... <|im_end|>\n" ≈ 5토큰). 정확한 값은 템플릿마다 다르지만
// 본문 토큰에 비하면 오차가 작다.
const (
	perMessageOverhead = 5
	perToolOverhead    = 5
)

// CountTokensText는 count_tokens 요청 본문을 토크나이저에 넣을 평문과, 템플릿 구분 토큰
// 근사치(overhead)로 바꾼다. 이미지·문서처럼 텍스트가 아닌 블록은 세지 않는다.
func CountTokensText(body []byte) (string, int, error) {
	var req struct {
		System json.RawMessage `json:"system"`
		Tools  []struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			InputSchema json.RawMessage `json:"input_schema"`
		} `json:"tools"`
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return "", 0, err
	}

	var sb strings.Builder
	overhead := 0
	if text := promptText(req.System); text != "" {
		sb.WriteString(text)
		sb.WriteByte('\n')
		overhead += perMessageOverhead
	}
	for _, t := range req.Tools {
		sb.WriteString(t.Name)
		sb.WriteByte('\n')
		sb.WriteString(t.Description)
		sb.WriteByte('\n')
		sb.WriteString(compactJSON(t.InputSchema))
		sb.WriteByte('\n')
		overhead += perToolOverhead
	}
	for _, m := range req.Messages {
		sb.WriteString(m.Role)
		sb.WriteByte('\n')
		sb.WriteString(promptText(m.Content))
		sb.WriteByte('\n')
		overhead += perMessageOverhead
	}
	return sb.String(), overhead, nil
}

// promptText는 문자열 또는 콘텐츠 블록 배열에서 모델이 실제로 읽는 텍스트를 모은다.
func promptText(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []json.RawMessage
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	var sb strings.Builder
	for _, b := range blocks {
		if text := blockText(b); text != "" {
			sb.WriteString(text)
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

func blockText(raw json.RawMessage) string {
	var b struct {
		Type     string          `json:"type"`
		Text     string          `json:"text"`
		Thinking string          `json:"thinking"`
		Name     string          `json:"name"`
		Input    json.RawMessage `json:"input"`
		Content  json.RawMessage `json:"content"`
	}
	if json.Unmarshal(raw, &b) != nil {
		return ""
	}
	switch b.Type {
	case "text":
		return b.Text
	case "thinking":
		return b.Thinking
	case "tool_use", "server_tool_use":
		return b.Name + "\n" + compactJSON(b.Input)
	case "tool_result", "web_search_tool_result":
		return promptText(b.Content)
	case "image", "document", "redacted_thinking":
		return ""
	}
	return compactJSON(raw)
}

// compactJSON은 JSON을 공백 없이, 비ASCII와 <>&를 이스케이프하지 않고 다시 쓴다 — 모델이
// 보는 형태에 가깝게 해야 토큰 수가 맞는다(\uXXXX로 바꾸면 한국어가 몇 배로 늘어난다).
func compactJSON(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return ""
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return string(raw)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if enc.Encode(v) != nil {
		return string(raw)
	}
	return strings.TrimSuffix(buf.String(), "\n")
}
