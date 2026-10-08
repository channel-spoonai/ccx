package anthropic

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// fakeTokenizer는 count_tokens 없이 /tokenize만 있는 업스트림(mlx-serve)을 흉내 낸다.
// 토큰 수는 공백으로 나눈 단어 수.
func fakeTokenizer(t *testing.T, countCalls, tokenizeCalls *atomic.Int32, haveTokenize bool) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/messages/count_tokens":
			countCalls.Add(1)
			http.Error(w, `{"error":{"message":"Unknown endpoint"}}`, http.StatusNotFound)
		case "/tokenize":
			tokenizeCalls.Add(1)
			if !haveTokenize {
				http.NotFound(w, r)
				return
			}
			if r.Header.Get("Authorization") != "Bearer upstream-token" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			var in struct {
				Content string `json:"content"`
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			ids := make([]int, len(strings.Fields(in.Content)))
			_ = json.NewEncoder(w).Encode(map[string]any{"tokens": ids})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func inputTokens(t *testing.T, resp *http.Response) int {
	t.Helper()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d: %s", resp.StatusCode, b)
	}
	var out struct {
		InputTokens int `json:"input_tokens"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out.InputTokens
}

func TestProxyCountsTokensViaTokenizeOn404(t *testing.T) {
	var countCalls, tokenizeCalls atomic.Int32
	up := fakeTokenizer(t, &countCalls, &tokenizeCalls, true)
	s := startProxy(t, up.URL, false)

	body := `{"model":"m","messages":[{"role":"user","content":"one two three"}]}`
	// "user\none two three\n" → 4단어 + 메시지 1개 overhead
	want := 4 + 5
	if got := inputTokens(t, post(t, s, "/v1/messages/count_tokens?beta=true", body, nil)); got != want {
		t.Errorf("input_tokens = %d, want %d", got, want)
	}
	// 두 번째부터는 404를 기억해 count_tokens 왕복을 건너뛴다.
	if got := inputTokens(t, post(t, s, "/v1/messages/count_tokens", body, nil)); got != want {
		t.Errorf("input_tokens = %d, want %d", got, want)
	}
	if countCalls.Load() != 1 || tokenizeCalls.Load() != 2 {
		t.Errorf("count_tokens %d회 / tokenize %d회, want 1 / 2", countCalls.Load(), tokenizeCalls.Load())
	}
}

// /tokenize도 없는 서버는 원래 404를 그대로 돌려준다 — Claude Code의 글자 수 추정으로 떨어질 뿐
// 지금보다 나빠지지 않는다. 한 번 실패하면 더 시도하지 않는다.
func TestProxyPassesThrough404WithoutTokenize(t *testing.T) {
	var countCalls, tokenizeCalls atomic.Int32
	up := fakeTokenizer(t, &countCalls, &tokenizeCalls, false)
	s := startProxy(t, up.URL, false)

	for range 2 {
		resp := post(t, s, "/v1/messages/count_tokens", `{"messages":[{"role":"user","content":"x"}]}`, nil)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", resp.StatusCode)
		}
	}
	if tokenizeCalls.Load() != 1 {
		t.Errorf("tokenize %d회, want 1 (없는 걸 확인한 뒤엔 시도하지 않음)", tokenizeCalls.Load())
	}
}

// count_tokens를 구현한 업스트림은 그 답을 그대로 쓴다.
func TestProxyUsesUpstreamCountTokens(t *testing.T) {
	var got capture
	up := newUpstream(t, &got, func(w http.ResponseWriter) {
		_, _ = w.Write([]byte(`{"input_tokens":777}`))
	})
	s := startProxy(t, up.URL, false)
	if n := inputTokens(t, post(t, s, "/v1/messages/count_tokens", `{"messages":[]}`, nil)); n != 777 {
		t.Errorf("input_tokens = %d, want 777", n)
	}
	if got.path != "/v1/messages/count_tokens" {
		t.Errorf("upstream path = %q", got.path)
	}
}

func TestTokenizeURLStripsV1(t *testing.T) {
	for base, want := range map[string]string{
		"http://h:1":    "http://h:1/tokenize",
		"http://h:1/v1": "http://h:1/tokenize",
	} {
		if got := (&Server{upstreamBaseURL: base}).tokenizeURL(); got != want {
			t.Errorf("tokenizeURL(%q) = %q, want %q", base, got, want)
		}
	}
}
