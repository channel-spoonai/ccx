package anthropic

import (
	"bytes"
	"net/http"
	"strings"
)

// upstreamClient는 데몬 수명 동안 연결 풀을 재사용한다.
// 스트리밍 응답이 길게 이어지므로 전체 timeout은 두지 않고 요청 컨텍스트로 제어한다.
var upstreamClient = &http.Client{}

// forward는 클라이언트 요청을 업스트림으로 그대로 중계한다.
// 헤더는 hop-by-hop과 로컬 프록시용 자격증명만 걷어내고 나머지를 보존한다 —
// anthropic-version / anthropic-beta / x-session-id 같은 것들이 업스트림 동작을 좌우한다.
func (s *Server) forward(r *http.Request, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(r.Context(), r.Method, s.upstreamURL(r), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	for k, vs := range r.Header {
		if skipRequestHeader(k) {
			continue
		}
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	if len(body) > 0 && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	// 업스트림 자격증명은 프로파일 값으로 다시 채운다. 클라이언트가 들고 온 것은
	// 프록시의 shared secret이라 업스트림에서는 의미가 없다.
	if s.upstreamAuth != "" {
		req.Header.Set("Authorization", "Bearer "+s.upstreamAuth)
	}
	if s.upstreamAPIKey != "" {
		req.Header.Set("x-api-key", s.upstreamAPIKey)
	}
	return upstreamClient.Do(req)
}

// upstreamURL은 baseUrl 뒤에 클라이언트가 요청한 경로를 그대로 붙인다.
// Claude Code가 ANTHROPIC_BASE_URL에 /v1/messages를 append하므로, 프록시가 받은 경로를
// 그대로 이어 붙이면 업스트림에서 원래 URL이 복원된다.
func (s *Server) upstreamURL(r *http.Request) string {
	url := s.upstreamBaseURL + r.URL.Path
	if r.URL.RawQuery != "" {
		url += "?" + r.URL.RawQuery
	}
	return url
}

func skipRequestHeader(name string) bool {
	switch strings.ToLower(name) {
	case "host", "content-length", "connection", "keep-alive",
		"authorization", "x-api-key", "accept-encoding":
		return true
	}
	return false
}
