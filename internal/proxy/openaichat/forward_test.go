package openaichat

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	tr "github.com/channel-spoonai/ccx/internal/translate/openaichat"
)

// enableThinkingSeen은 서버가 받은 요청들의 enable_thinking 포함 여부를 기록한다.
func newRejectingUpstream(t *testing.T, seen *[]bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
		}
		_, has := payload["enable_thinking"]
		*seen = append(*seen, has)
		if has {
			// NVIDIA NIM이 실제로 내는 형태
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte("{\"error\":{\"message\":\"Validation: Unsupported parameter(s): `enable_thinking`\",\"type\":\"Bad Request\",\"code\":400}}"))
			return
		}
		_, _ = w.Write([]byte(`{"id":"ok","choices":[]}`))
	}))
}

func TestForwardRetriesWithoutEnableThinking(t *testing.T) {
	var seen []bool
	srv := newRejectingUpstream(t, &seen)
	defer srv.Close()

	enabled := false
	body := &tr.ChatRequest{
		Model:          "nvidia/nemotron-3-super-120b-a12b",
		Messages:       []tr.ChatMessage{{Role: "user"}},
		EnableThinking: &enabled,
	}
	rc, err := Forward(context.Background(), body, ForwardOptions{UpstreamBaseURL: srv.URL})
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	defer rc.Close()
	if _, err := io.ReadAll(rc); err != nil {
		t.Fatalf("read body: %v", err)
	}

	if len(seen) != 2 || !seen[0] || seen[1] {
		t.Fatalf("요청 순서가 [enable_thinking 포함, 미포함]이어야 하는데 %v", seen)
	}
	// 호출자의 원본은 건드리지 않아야 한다
	if body.EnableThinking == nil {
		t.Error("Forward가 호출자의 ChatRequest를 변경했다")
	}

	// 같은 모델의 다음 요청은 처음부터 필드 없이 나가야 한다
	seen = nil
	rc2, err := Forward(context.Background(), body, ForwardOptions{UpstreamBaseURL: srv.URL})
	if err != nil {
		t.Fatalf("두 번째 Forward: %v", err)
	}
	defer rc2.Close()
	if len(seen) != 1 || seen[0] {
		t.Fatalf("거부된 모델은 재시도 없이 필드를 빼야 하는데 %v", seen)
	}
}

// enable_thinking과 무관한 400은 재시도하지 않는다.
func TestForwardDoesNotRetryUnrelated400(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"context length exceeded"}}`))
	}))
	defer srv.Close()

	enabled := false
	body := &tr.ChatRequest{
		Model:          "some/other-model",
		Messages:       []tr.ChatMessage{{Role: "user"}},
		EnableThinking: &enabled,
	}
	if _, err := Forward(context.Background(), body, ForwardOptions{UpstreamBaseURL: srv.URL}); err == nil {
		t.Fatal("400이면 에러를 돌려줘야 한다")
	}
	if calls != 1 {
		t.Errorf("무관한 400은 1회만 호출해야 하는데 %d회", calls)
	}
}
