package anthropic

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	tr "github.com/channel-spoonai/ccx/internal/translate/anthropic"
)

// count_tokens 폴백.
//
// Claude Code는 /context를 그릴 때 카테고리(시스템·툴·메모리·스킬·메시지)마다
// /v1/messages/count_tokens를 부른다(실측 16건). mlx-serve처럼 이 엔드포인트가 없어 404를
// 내면 글자 수 추정(len/4, JSON은 len/2)으로 떨어지는데, 툴 결과가 많은 대화에서 2배
// 가까이 부푼다(실측: 서버 74,343토큰 → /context 142.3k). 업스트림이 404를 내면 같은 서버의
// /tokenize(llama.cpp 계열 — mlx-serve 지원)로 직접 세서 Anthropic 형식으로 답한다.
//
// 기본 ON이고 env 키를 따로 두지 않는다 — /tokenize가 없거나 실패하면 원래 404를 그대로
// 돌려주므로 지금보다 나빠지는 경우가 없고, 키가 없으니 자동 업데이트 직후의 "구버전 부모 +
// 신버전 데몬" 조합에서도 바로 동작한다.

// isCountTokensPath는 count_tokens 요청인지 본다 (쿼리 ?beta=true는 URL.Path에 없다).
func isCountTokensPath(path string) bool {
	return strings.TrimSuffix(path, "/") == "/v1/messages/count_tokens"
}

// countTokensLocally는 업스트림 count_tokens가 404일 때 /tokenize로 세어 응답한다.
// 처리했으면 true. false면 호출자가 업스트림 응답을 그대로 중계한다.
func (s *Server) countTokensLocally(w http.ResponseWriter, r *http.Request, body []byte) bool {
	if s.tokenizeUnsupported.Load() {
		return false
	}
	text, overhead, err := tr.CountTokensText(body)
	if err != nil {
		return false
	}
	n, err := s.tokenize(r, text)
	if err != nil {
		if errors.Is(err, errTokenizeMissing) {
			s.tokenizeUnsupported.Store(true)
		}
		return false
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = fmt.Fprintf(w, `{"input_tokens":%d}`, n+overhead)
	return true
}

var errTokenizeMissing = errors.New("upstream has no /tokenize")

// tokenize는 업스트림 /tokenize에 텍스트를 보내 토큰 수를 받는다.
// 응답은 {"tokens":[...]}(llama.cpp·mlx-serve) 또는 {"count":N}(vLLM) 둘 다 받는다.
func (s *Server) tokenize(r *http.Request, text string) (int, error) {
	payload, err := json.Marshal(map[string]string{"content": text})
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, s.tokenizeURL(), bytes.NewReader(payload))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if s.upstreamAuth != "" {
		req.Header.Set("Authorization", "Bearer "+s.upstreamAuth)
	}
	if s.upstreamAPIKey != "" {
		req.Header.Set("x-api-key", s.upstreamAPIKey)
	}
	resp, err := upstreamClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
		return 0, errTokenizeMissing
	}
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("tokenize: status %d", resp.StatusCode)
	}
	var out struct {
		Tokens []json.RawMessage `json:"tokens"`
		Count  *int              `json:"count"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<20)).Decode(&out); err != nil {
		return 0, err
	}
	if out.Count != nil {
		return *out.Count, nil
	}
	if out.Tokens == nil {
		return 0, errTokenizeMissing
	}
	return len(out.Tokens), nil
}

// tokenizeURL은 서버 루트의 /tokenize다. baseUrl이 /v1로 끝나도 루트로 올린다.
func (s *Server) tokenizeURL() string {
	return strings.TrimSuffix(s.upstreamBaseURL, "/v1") + "/tokenize"
}
