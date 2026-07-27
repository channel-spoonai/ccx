package providers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/channel-spoonai/ccx/internal/ctxwin"
)

func TestContextSuffix(t *testing.T) {
	cases := []struct {
		in   int
		want string
	}{
		{0, ""},
		{512, ""}, // 1k 미만은 표기 불가
		{131_072, "[131k]"},
		{200_000, "[200k]"},
		{262_144, "[262k]"},
		{1_000_000, "[1m]"},
		{1_050_000, "[1m]"},
	}
	for _, c := range cases {
		if got := ContextSuffix(c.in); got != c.want {
			t.Errorf("ContextSuffix(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestEffectiveContext(t *testing.T) {
	m := OpenRouterModel{ID: "x", ContextLength: 202_752}
	if got := EffectiveContext(m); got != 202_752 {
		t.Errorf("model-level only: got %d", got)
	}
	m.TopProvider.ContextLength = 200_000
	if got := EffectiveContext(m); got != 200_000 {
		t.Errorf("top_provider smaller must win: got %d", got)
	}
	m.TopProvider.ContextLength = 300_000
	if got := EffectiveContext(m); got != 202_752 {
		t.Errorf("model-level smaller must win: got %d", got)
	}
}

func TestFetchLMStudioContextsV1(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/models" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"models":[
			{"key":"ibm/granite-4-micro","max_context_length":1048576,
			 "loaded_instances":[{"config":{"context_length":131072}}]},
			{"key":"unloaded-model","max_context_length":32768,"loaded_instances":[]}
		]}`))
	}))
	defer srv.Close()

	got := FetchLMStudioContexts(srv.URL, "")
	if got["ibm/granite-4-micro"] != 131_072 {
		t.Errorf("loaded context = %d, want 131072 (max_context_length 무시)", got["ibm/granite-4-micro"])
	}
	if _, ok := got["unloaded-model"]; ok {
		t.Errorf("unloaded model must not be recorded: %v", got)
	}
}

// 같은 모델을 다른 컨텍스트로 여러 번 로드하면 보수적으로 최솟값을 쓰고,
// 인스턴스 식별자로도 매핑한다.
func TestFetchLMStudioContextsMultiInstance(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/models" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"models":[
			{"key":"granite","loaded_instances":[
				{"id":"granite","config":{"context_length":131072}},
				{"id":"granite:2","config":{"context_length":32768}}
			]}
		]}`))
	}))
	defer srv.Close()

	got := FetchLMStudioContexts(srv.URL, "")
	if got["granite"] != 32_768 {
		t.Errorf("granite = %d, want 32768 (min across instances)", got["granite"])
	}
	if got["granite:2"] != 32_768 {
		t.Errorf("granite:2 = %d, want 32768 (instance id mapped)", got["granite:2"])
	}
}

// v1이 응답하지만 로드된 인스턴스가 없으면 v0 폴백으로 이어져야 한다.
func TestFetchLMStudioContextsV1EmptyThenV0(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/models":
			w.Write([]byte(`{"models":[{"key":"granite","loaded_instances":[]}]}`))
		case "/api/v0/models":
			w.Write([]byte(`{"data":[{"id":"granite","loaded_context_length":65536}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	got := FetchLMStudioContexts(srv.URL, "")
	if got["granite"] != 65_536 {
		t.Errorf("granite = %v, want 65536 via v0 fallback", got)
	}
}

func TestFetchLMStudioContextsV0Fallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v0/models":
			w.Write([]byte(`{"data":[{"id":"granite","max_context_length":1048576,"loaded_context_length":65536}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	got := FetchLMStudioContexts(srv.URL, "")
	if got["granite"] != 65_536 {
		t.Errorf("v0 fallback = %v, want granite:65536", got)
	}
}

func TestFetchLMStudioContextsUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	if got := FetchLMStudioContexts(srv.URL, ""); len(got) != 0 {
		t.Errorf("want empty map on missing native API, got %v", got)
	}
}

// 박제(ContextSuffix)와 해석(ctxwin.ParseSuffix)의 왕복 가드 — flows가 붙인
// suffix는 launch에서 반드시 해석 가능해야 하며, floor로 인한 하향만 허용된다.
func TestContextSuffixRoundTrip(t *testing.T) {
	for _, w := range []int{1_000, 65_536, 131_072, 200_000, 202_752, 262_144, 999_999, 1_000_000, 1_050_000} {
		suffix := ContextSuffix(w)
		if suffix == "" {
			t.Errorf("ContextSuffix(%d) empty", w)
			continue
		}
		_, parsed, ok := ctxwin.ParseSuffix("model" + suffix)
		if !ok {
			t.Errorf("stamped suffix %q not parseable", suffix)
			continue
		}
		if parsed > w {
			t.Errorf("roundtrip %d → %q → %d overstates the window", w, suffix, parsed)
		}
	}
}
