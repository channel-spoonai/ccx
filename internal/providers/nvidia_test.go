package providers

import (
	"testing"

	"github.com/channel-spoonai/ccx/internal/config"
)

func TestIsRecommended(t *testing.T) {
	cases := []struct {
		id   string
		want bool
	}{
		{"z-ai/glm-5.2", true},
		{"minimaxai/minimax-m3", true},
		{"nvidia/nemotron-3-ultra-550b-a55b", true},
		{"nvidia/nemotron-3-super-120b-a12b", true},
		{"deepseek-ai/deepseek-v4-flash", true},
		{"deepseek-ai/deepseek-v4-pro", true},
		{"DeepSeek-AI/DeepSeek-V4-Pro", true}, // 대소문자 무시

		// allowlist 밖 — 대화형이어도 숨긴다
		{"openai/gpt-oss-120b", false},
		{"meta/llama-3.3-70b-instruct", false},
		{"moonshotai/kimi-k2.6", false},
		{"nvidia/nemotron-3-nano-30b-a3b", false},
		// 애초에 대화형이 아닌 것들
		{"nvidia/nv-embedqa-e5-v5", false},
		{"meta/llama-guard-4-12b", false},
		{"", false},
	}
	for _, c := range cases {
		if got := IsRecommended(c.id); got != c.want {
			t.Errorf("IsRecommended(%q) = %v, want %v", c.id, got, c.want)
		}
	}
}

// 2026-07 실측값 — 한도 초과 요청에 NIM이 돌려주는
// "This model's maximum context length is N tokens" 메시지에서 읽었다.
// 모델 공식 스펙이 아니라 NIM의 실제 배포 설정이라는 점이 중요하다.
func TestNVIDIAContextWindow(t *testing.T) {
	cases := map[string]int{
		"z-ai/glm-5.2":                      202_752, // z.ai 직결은 1M — 프로바이더마다 다르다
		"minimaxai/minimax-m3":              524_288,
		"nvidia/nemotron-3-ultra-550b-a55b": 1_000_000,
		"nvidia/nemotron-3-super-120b-a12b": 1_000_000,
		"deepseek-ai/deepseek-v4-flash":     1_000_000,
		"deepseek-ai/deepseek-v4-pro":       262_144,
		"Z-AI/GLM-5.2":                      202_752, // 대소문자 무시
		"meta/llama-3.3-70b-instruct":       0,       // allowlist 밖 — 미상
		"":                                  0,
	}
	for id, want := range cases {
		if got := NVIDIAContextWindow(id); got != want {
			t.Errorf("NVIDIAContextWindow(%q) = %d, want %d", id, got, want)
		}
	}

	// allowlist의 모든 모델은 컨텍스트를 알아야 한다 — 모르면 Claude Code가
	// 200K로 가정해 202K/262K 모델에서 오버플로가 난다.
	for _, id := range RecommendedNVIDIAModels {
		if NVIDIAContextWindow(id) == 0 {
			t.Errorf("권장 모델 %q 의 컨텍스트가 테이블에 없다", id)
		}
	}
}

// 박제되는 suffix가 실측값을 넘지 않아야 한다 (ContextSuffix는 k 단위 내림).
func TestNVIDIASuffixNeverOverstates(t *testing.T) {
	for _, id := range RecommendedNVIDIAModels {
		w := NVIDIAContextWindow(id)
		switch s := ContextSuffix(w); s {
		case "[1m]":
			if w < 1_000_000 {
				t.Errorf("%s: window %d 인데 [1m] 박제", id, w)
			}
		case "":
			t.Errorf("%s: suffix가 비었다 (window=%d)", id, w)
		}
	}
}

func TestFilterRecommended(t *testing.T) {
	// 서버가 주는 순서와 무관하게 allowlist 순서로 나와야 한다.
	in := []NVIDIAModel{
		{ID: "meta/llama-3.3-70b-instruct"},
		{ID: "deepseek-ai/deepseek-v4-pro"},
		{ID: "nvidia/nv-embedqa-e5-v5"},
		{ID: "z-ai/glm-5.2"},
		{ID: "nvidia/nemotron-3-super-120b-a12b"},
	}
	got := FilterRecommended(in)
	want := []string{"z-ai/glm-5.2", "nvidia/nemotron-3-super-120b-a12b", "deepseek-ai/deepseek-v4-pro"}
	if len(got) != len(want) {
		t.Fatalf("FilterRecommended = %d models, want %d (%v)", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].ID != w {
			t.Errorf("FilterRecommended[%d] = %q, want %q", i, got[i].ID, w)
		}
	}

	// 권장 모델이 하나도 없으면 빈 슬라이스 — flows가 수동 입력으로 폴백한다.
	none := []NVIDIAModel{{ID: "meta/llama-3.3-70b-instruct"}, {ID: "baai/bge-m3"}}
	if got := FilterRecommended(none); len(got) != 0 {
		t.Errorf("FilterRecommended(no-match) = %v, want empty", got)
	}
}

func TestIsNVIDIA(t *testing.T) {
	cases := []struct {
		name    string
		baseURL string
		want    bool
	}{
		{"NVIDIA NIM", "", true},
		{"nvidia nim (copy)", "", true},
		// 이름이 달라도 baseUrl 호스트로 판별
		{"my gpu gateway", "https://integrate.api.nvidia.com/v1", true},
		{"OpenRouter", "https://openrouter.ai/api", false},
		{"LM Studio (local)", "http://localhost:1234", false},
		{"glm", "https://api.z.ai/api/anthropic", false},
	}
	for _, c := range cases {
		p := &config.Profile{Name: c.name, BaseURL: c.baseURL}
		if got := IsNVIDIA(p); got != c.want {
			t.Errorf("IsNVIDIA({name:%q, baseUrl:%q}) = %v, want %v", c.name, c.baseURL, got, c.want)
		}
	}
	if IsNVIDIA(nil) {
		t.Error("IsNVIDIA(nil) = true, want false")
	}
}
